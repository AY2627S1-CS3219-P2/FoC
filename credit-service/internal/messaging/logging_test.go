// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-04
// Scope: Tests that every consumer line about an event carries its request id
//   and identifiers on the ack, retry and dead-letter paths, and that the
//   supervisor's reconnect line carries no RabbitMQ credentials (D-135).
// Author review: James Teo — reviewed and verified.

package messaging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"foc/credit-service/internal/credit"
	"foc/credit-service/internal/messaging"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

// leakyErr quotes a connection string, as a database driver's error can.
var leakyErr = errors.New("dial postgres://credit:hunter2@credit-db:5432/credit: connection refused")

func TestLifecycleDeliveryLogFields(t *testing.T) {
	cases := []struct {
		name        string
		routingKey  string
		contentType string
		serviceErr  error
		attempt     int64
		wantOutcome string
		wantLevel   string
		wantMsg     string
		wantNoIDs   bool
	}{
		{name: "a settled event is acknowledged", routingKey: messaging.RoutingCancelled,
			wantOutcome: "ack", wantLevel: "INFO", wantMsg: "lifecycle event acknowledged"},
		{name: "an applied reward change is acknowledged", routingKey: messaging.RoutingRewardSet,
			wantOutcome: "ack", wantLevel: "INFO", wantMsg: "lifecycle event acknowledged"},
		{name: "an unreserved errand.failed is acknowledged", routingKey: messaging.RoutingFailed, serviceErr: credit.ErrReservationNotFound,
			wantOutcome: "ack", wantLevel: "INFO", wantMsg: "errand.failed for unreserved errand acknowledged as no-op"},
		{name: "a temporary error is retried", routingKey: messaging.RoutingCompleted, serviceErr: leakyErr, attempt: 1,
			wantOutcome: "requeue", wantLevel: "WARN", wantMsg: "retrying lifecycle event after temporary error"},
		{name: "a missing reservation is retried", routingKey: messaging.RoutingExpired, serviceErr: credit.ErrReservationNotFound, attempt: 2,
			wantOutcome: "requeue", wantLevel: "WARN", wantMsg: "reservation not found for lifecycle event, requeuing"},
		{name: "a temporary error is dead-lettered on the fifth attempt", routingKey: messaging.RoutingCancelled, serviceErr: leakyErr, attempt: 5,
			wantOutcome: "dead-letter", wantLevel: "ERROR", wantMsg: "temporary error retry attempts exhausted, dead-lettering to DLQ"},
		{name: "a missing reservation is dead-lettered on the fifth attempt", routingKey: messaging.RoutingCompleted, serviceErr: credit.ErrReservationNotFound, attempt: 5,
			wantOutcome: "dead-letter", wantLevel: "ERROR", wantMsg: "reservation not found retry attempts exhausted, dead-lettering to DLQ"},
		{name: "a contract violation is dead-lettered", routingKey: messaging.RoutingCancelled, serviceErr: credit.ErrRequesterMismatch,
			wantOutcome: "dead-letter", wantLevel: "WARN", wantMsg: "rejecting contract or lifecycle violation to DLQ"},
		// Transport validation precedes any decoding of the body, so this line
		// names the delivery but not the event's identifiers.
		{name: "an invalid transport is dead-lettered", routingKey: messaging.RoutingCancelled, contentType: "text/plain",
			wantOutcome: "dead-letter", wantLevel: "WARN", wantMsg: "rejecting message with invalid transport to DLQ", wantNoIDs: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			fail := func() error { return tc.serviceErr }
			svc := &mockLifecycleService{
				refundFn: func(context.Context, uuid.UUID, *uuid.UUID, uuid.UUID, credit.TransactionReason, time.Time) error {
					return fail()
				},
				payoutFn: func(context.Context, uuid.UUID, *uuid.UUID, uuid.UUID, uuid.UUID, *int64, time.Time) error {
					return fail()
				},
				rewardSetFn: func(context.Context, uuid.UUID, uuid.UUID, int64, int64, time.Time) error { return fail() },
			}
			noWait := messaging.WithLifecycleBackoff(func(context.Context, int) bool { return true })
			h := messaging.NewLifecycleHandler(svc, slog.New(slog.NewJSONHandler(&logs, nil)), noWait)
			eventID, errandID := uuid.New(), uuid.New()
			d, acker := delivery(tc.routingKey, tc.contentType, tc.attempt, fmt.Sprintf(`{
				"schemaVersion": 1, "eventId": %q, "errandId": %q, "courierId": %q,
				"amount": 30, "version": 2, "occurredAt": %q
			}`, eventID, errandID, uuid.New(), time.Now().UTC().Format(time.RFC3339Nano)))

			h.HandleDelivery(context.Background(), d)

			if acker.outcome != tc.wantOutcome {
				t.Fatalf("outcome = %q, want %q", acker.outcome, tc.wantOutcome)
			}
			lines := decodeLines(t, &logs)
			wantFields := map[string]any{"request_id": d.CorrelationId, "eventId": eventID.String(), "errandId": errandID.String()}
			if tc.wantNoIDs {
				wantFields = map[string]any{"request_id": d.CorrelationId, "routingKey": tc.routingKey}
				for _, line := range lines {
					if _, ok := line["eventId"]; ok {
						t.Errorf("line %q names eventId from a body that failed transport validation", line["msg"])
					}
				}
			}
			requireFieldsOnEveryLine(t, lines, wantFields)
			requireOneLine(t, lines, tc.wantLevel, tc.wantMsg)
			if strings.Contains(logs.String(), "hunter2") {
				t.Errorf("a credential was logged: %s", logs.String())
			}
		})
	}
}

func TestRegistrationDeliveryLogFields(t *testing.T) {
	cases := []struct {
		name        string
		allocErr    error
		dropField   string
		attempt     int64
		wantOutcome string
		wantLevel   string
		wantMsg     string
	}{
		{name: "an allocation is acknowledged",
			wantOutcome: "ack", wantLevel: "INFO", wantMsg: "user.registered event acknowledged"},
		{name: "a failed allocation is retried", allocErr: leakyErr, attempt: 1,
			wantOutcome: "requeue", wantLevel: "WARN", wantMsg: "retrying user.registered event after handler failure"},
		{name: "a failed allocation is dead-lettered on the fifth attempt", allocErr: leakyErr, attempt: 5,
			wantOutcome: "dead-letter", wantLevel: "ERROR", wantMsg: "registration delivery attempts exhausted, dead-lettering to DLQ"},
		{name: "a malformed event is dead-lettered", dropField: "occurredAt",
			wantOutcome: "dead-letter", wantLevel: "WARN", wantMsg: "dropping malformed user.registered event to DLQ"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			allocator := allocatorFunc(func(context.Context, uuid.UUID, time.Time) error { return tc.allocErr })
			h := messaging.NewRegistrationHandler(allocator, messaging.WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))))
			eventID, userID := uuid.New(), uuid.New()
			event := map[string]any{"eventId": eventID, "userId": userID, "occurredAt": time.Now().UTC()}
			delete(event, tc.dropField)
			body, err := json.Marshal(event)
			if err != nil {
				t.Fatalf("marshal event: %v", err)
			}
			d, acker := delivery(messaging.RoutingUserRegistered, "", tc.attempt, string(body))

			h.HandleDelivery(context.Background(), d)

			if acker.outcome != tc.wantOutcome {
				t.Fatalf("outcome = %q, want %q", acker.outcome, tc.wantOutcome)
			}
			lines := decodeLines(t, &logs)
			requireFieldsOnEveryLine(t, lines, map[string]any{"request_id": d.CorrelationId, "eventId": eventID.String(), "userId": userID.String()})
			requireOneLine(t, lines, tc.wantLevel, tc.wantMsg)
			if strings.Contains(logs.String(), "hunter2") {
				t.Errorf("a credential was logged: %s", logs.String())
			}
		})
	}
}

func TestReconnectLineCarriesNoCredentials(t *testing.T) {
	cases := []struct {
		name      string
		password  string
		wantError string
	}{
		{name: "a password holding '/'", password: "s3cr/et99",
			wantError: `dial RabbitMQ: parse "amqp://REDACTED": invalid port "REDACTED" after host`},
		{name: "a password holding '#'", password: "s3cr#et99",
			wantError: `dial RabbitMQ: parse "amqp://REDACTED": invalid port "REDACTED" after host`},
		{name: "a password holding a bad escape", password: "s3cr%zzet99",
			wantError: `dial RabbitMQ: parse "amqp://REDACTED": invalid URL escape "REDACTED"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := &firstWrite{wrote: make(chan struct{})}
			s := messaging.NewSupervisor("amqp://foc:"+tc.password+"@rabbitmq:5672/foc", nil, nil, 1, slog.New(slog.NewJSONHandler(logs, nil)))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stopped := make(chan error, 1)
			go func() { stopped <- s.Run(ctx) }()

			select {
			case <-logs.wrote:
			case <-time.After(10 * time.Second):
				t.Fatal("the supervisor logged nothing within 10s")
			}
			cancel()
			if err := <-stopped; !errors.Is(err, context.Canceled) {
				t.Fatalf("Run returned %v, want context.Canceled", err)
			}

			for _, line := range decodeLines(t, &logs.buf) {
				if line["level"] != "WARN" || line["msg"] != "RabbitMQ connection lost or failed, reconnecting" {
					t.Errorf("logged %v, want only the reconnect WARN", line)
				}
				if line["error"] != tc.wantError {
					t.Errorf("error = %v, want %q", line["error"], tc.wantError)
				}
			}
			for _, piece := range []string{"s3cr", "et99", "%zz"} {
				if strings.Contains(logs.buf.String(), piece) {
					t.Errorf("%q from the password was logged: %s", piece, logs.buf.String())
				}
			}
		})
	}
}

// delivery is a delivery of body with its own request id as its CorrelationId,
// settled through the returned acker. A zero attempt sends no delivery count.
func delivery(routingKey, contentType string, attempt int64, body string) (amqp.Delivery, *fakeAcker) {
	if contentType == "" {
		contentType = "application/json"
	}
	acker := &fakeAcker{}
	d := amqp.Delivery{
		Acknowledger:  acker,
		RoutingKey:    routingKey,
		ContentType:   contentType,
		CorrelationId: "req-" + uuid.NewString(),
		Body:          []byte(body),
	}
	if attempt > 0 {
		d.Headers = amqp.Table{"x-delivery-count": attempt}
	}
	return d, acker
}

// fakeAcker records how a delivery was settled.
type fakeAcker struct {
	outcome string
}

func (a *fakeAcker) Ack(uint64, bool) error {
	a.outcome = "ack"
	return nil
}

func (a *fakeAcker) Nack(_ uint64, _ bool, requeue bool) error {
	a.settle(requeue)
	return nil
}

func (a *fakeAcker) Reject(_ uint64, requeue bool) error {
	a.settle(requeue)
	return nil
}

func (a *fakeAcker) settle(requeue bool) {
	a.outcome = "dead-letter"
	if requeue {
		a.outcome = "requeue"
	}
}

type allocatorFunc func(context.Context, uuid.UUID, time.Time) error

func (f allocatorFunc) AllocateInitialCredits(ctx context.Context, userID uuid.UUID, occurredAt time.Time) error {
	return f(ctx, userID, occurredAt)
}

// firstWrite keeps what a logger writes and closes wrote once the first line
// is in.
type firstWrite struct {
	buf   bytes.Buffer
	wrote chan struct{}
	once  sync.Once
}

func (w *firstWrite) Write(p []byte) (int, error) {
	n, err := w.buf.Write(p)
	w.once.Do(func() { close(w.wrote) })
	return n, err
}

func decodeLines(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, raw := range bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
		if len(raw) == 0 {
			continue
		}
		var line map[string]any
		if err := json.Unmarshal(raw, &line); err != nil {
			t.Fatalf("log line is not one JSON object: %v: %s", err, raw)
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		t.Fatal("nothing was logged")
	}
	return lines
}

func requireFieldsOnEveryLine(t *testing.T, lines []map[string]any, want map[string]any) {
	t.Helper()
	for _, line := range lines {
		for key, value := range want {
			if line[key] != value {
				t.Errorf("line %q: %s = %v, want %v", line["msg"], key, line[key], value)
			}
		}
	}
}

func requireOneLine(t *testing.T, lines []map[string]any, level, msg string) {
	t.Helper()
	count := 0
	for _, line := range lines {
		if line["msg"] == msg {
			count++
			if line["level"] != level {
				t.Errorf("%q logged at %v, want %s", msg, line["level"], level)
			}
		}
	}
	if count != 1 {
		t.Errorf("%q logged %d times, want once", msg, count)
	}
}
