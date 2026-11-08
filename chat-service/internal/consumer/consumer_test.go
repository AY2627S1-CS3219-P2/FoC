// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
// Scope: Added the table-driven delivery-logging test and its fakes (request_id,
//   eventId and errandId on every line, one line on ack, requeue, dead-letter
//   and a refused ack), and the logger argument to the existing routing test
//   (D-135).
// Author review: James Teo — reviewed and verified.

package consumer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"foc/chat-service/internal/chat"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeChatRepo struct {
	chat.Repository
	acceptedCalled   bool
	withdrawnCalled  bool
	completedCalled  bool
	terminatedCalled bool
}

func (f *fakeChatRepo) UpsertConversationOnAccepted(ctx context.Context, errandID, requesterID, courierID uuid.UUID, occurredAt time.Time) (*chat.Conversation, error) {
	f.acceptedCalled = true
	return &chat.Conversation{
		ID:          uuid.New(),
		ErrandID:    errandID,
		RequesterID: requesterID,
		CourierID:   courierID,
		Status:      chat.StatusActive,
	}, nil
}

func (f *fakeChatRepo) GetConversationByErrandCourier(ctx context.Context, errandID, courierID uuid.UUID) (*chat.Conversation, error) {
	return &chat.Conversation{
		ID:          uuid.New(),
		ErrandID:    errandID,
		CourierID:   courierID,
		RequesterID: uuid.New(),
		Status:      chat.StatusActive,
	}, nil
}

func (f *fakeChatRepo) FreezeConversation(ctx context.Context, errandID, courierID uuid.UUID, occurredAt time.Time) error {
	f.withdrawnCalled = true
	return nil
}

func (f *fakeChatRepo) MarkErrandCompleted(ctx context.Context, errandID uuid.UUID, occurredAt time.Time) error {
	f.completedCalled = true
	return nil
}

func (f *fakeChatRepo) FreezeErrandCancelledOrExpired(ctx context.Context, errandID uuid.UUID, occurredAt time.Time) error {
	f.terminatedCalled = true
	return nil
}

func TestLifecycleHandler_Routing(t *testing.T) {
	errandID := uuid.New()
	requesterID := uuid.New()
	courierID := uuid.New()
	now := time.Now().UTC()

	tests := []struct {
		name       string
		routingKey string
		payload    ErrandLifecycleEvent
		check      func(t *testing.T, repo *fakeChatRepo)
	}{
		{
			name:       "errand.accepted",
			routingKey: "errand.accepted",
			payload: ErrandLifecycleEvent{
				EventID:     uuid.New(),
				EventType:   "errand.accepted",
				ErrandID:    errandID,
				RequesterID: requesterID,
				CourierID:   &courierID,
				OccurredAt:  now,
			},
			check: func(t *testing.T, repo *fakeChatRepo) {
				assert.True(t, repo.acceptedCalled)
			},
		},
		{
			name:       "errand.withdrawn",
			routingKey: "errand.withdrawn",
			payload: ErrandLifecycleEvent{
				EventID:    uuid.New(),
				EventType:  "errand.withdrawn",
				ErrandID:   errandID,
				CourierID:  &courierID,
				OccurredAt: now,
			},
			check: func(t *testing.T, repo *fakeChatRepo) {
				assert.True(t, repo.withdrawnCalled)
			},
		},
		{
			name:       "errand.completed",
			routingKey: "errand.completed",
			payload: ErrandLifecycleEvent{
				EventID:    uuid.New(),
				EventType:  "errand.completed",
				ErrandID:   errandID,
				OccurredAt: now,
			},
			check: func(t *testing.T, repo *fakeChatRepo) {
				assert.True(t, repo.completedCalled)
			},
		},
		{
			name:       "errand.cancelled",
			routingKey: "errand.cancelled",
			payload: ErrandLifecycleEvent{
				EventID:    uuid.New(),
				EventType:  "errand.cancelled",
				ErrandID:   errandID,
				OccurredAt: now,
			},
			check: func(t *testing.T, repo *fakeChatRepo) {
				assert.True(t, repo.terminatedCalled)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeChatRepo{}
			svc := chat.NewService(repo, nil, nil)
			handler := NewLifecycleHandler(svc, slog.New(slog.DiscardHandler))

			body, err := json.Marshal(tc.payload)
			require.NoError(t, err)

			err = handler.Handle(context.Background(), tc.routingKey, body)
			require.NoError(t, err)
			tc.check(t, repo)
		})
	}
}

// fakeAcker records how a delivery was answered.
type fakeAcker struct {
	outcome string
	ackErr  error
}

func (a *fakeAcker) Ack(uint64, bool) error {
	a.outcome = "ack"
	return a.ackErr
}

func (a *fakeAcker) Nack(_ uint64, _ bool, requeue bool) error {
	a.outcome = "dead-letter"
	if requeue {
		a.outcome = "requeue"
	}
	return nil
}

func (a *fakeAcker) Reject(_ uint64, requeue bool) error {
	return a.Nack(0, false, requeue)
}

// failingCompletionRepo fails every errand completion with err.
type failingCompletionRepo struct {
	fakeChatRepo
	err error
}

func (f *failingCompletionRepo) MarkErrandCompleted(context.Context, uuid.UUID, time.Time) error {
	return f.err
}

func TestHandleDeliveryLogsEveryEventWithItsIDs(t *testing.T) {
	const correlationID = "req-event-7b1c"
	dbDown := errors.New("mark errand completed: dial tcp 10.0.0.5:5432: connect: connection refused")

	cases := []struct {
		name          string
		routingKey    string
		body          string // "{eventId}" and "{errandId}" stand for the event's ids
		deliveryCount int64
		repoErr       error
		ackErr        error
		wantOutcome   string
		wantLevel     string
		wantMsg       string
		wantErr       string // part of the line's err; "" when it has none
		noEventID     bool   // the body holds no valid eventId to name
		noErrandID    bool   // the body holds no valid errandId to name
	}{
		{
			name: "an accepted errand is acknowledged", routingKey: "errand.accepted",
			body:        `{"eventId":"{eventId}","errandId":"{errandId}","requesterId":"` + uuid.NewString() + `","courierId":"` + uuid.NewString() + `"}`,
			wantOutcome: "ack", wantLevel: "INFO", wantMsg: "lifecycle event acknowledged",
		},
		{
			name: "an event this consumer ignores is acknowledged", routingKey: "errand.created",
			body:        `{"eventId":"{eventId}","errandId":"{errandId}"}`,
			wantOutcome: "ack", wantLevel: "INFO", wantMsg: "lifecycle event acknowledged",
		},
		{
			name: "a failing event is requeued", routingKey: "errand.completed", repoErr: dbDown, deliveryCount: 2,
			body:        `{"eventId":"{eventId}","errandId":"{errandId}"}`,
			wantOutcome: "requeue", wantLevel: "WARN", wantMsg: "lifecycle event failed; requeuing", wantErr: "connection refused",
		},
		{
			name: "a failing event is dead-lettered on its fifth delivery", routingKey: "errand.completed", repoErr: dbDown, deliveryCount: 5,
			body:        `{"eventId":"{eventId}","errandId":"{errandId}"}`,
			wantOutcome: "dead-letter", wantLevel: "ERROR", wantMsg: "lifecycle event failed; delivery count exceeded, dead-lettering", wantErr: "connection refused",
		},
		{
			name: "an event without an errandId still names its eventId", routingKey: "errand.completed",
			body:        `{"eventId":"{eventId}"}`,
			wantOutcome: "requeue", wantLevel: "WARN", wantMsg: "lifecycle event failed; requeuing", wantErr: "errandId is required",
			noErrandID: true,
		},
		{
			name: "a malformed eventId hides no other id", routingKey: "errand.completed",
			body:        `{"eventId":"not-a-uuid","errandId":"{errandId}"}`,
			wantOutcome: "requeue", wantLevel: "WARN", wantMsg: "lifecycle event failed; requeuing", wantErr: "malformed json",
			noEventID: true,
		},
		{
			name: "an ack the broker refuses", routingKey: "errand.completed", ackErr: amqp.ErrClosed,
			body:        `{"eventId":"{eventId}","errandId":"{errandId}"}`,
			wantOutcome: "ack", wantLevel: "WARN", wantMsg: "could not acknowledge lifecycle event; the broker will redeliver it", wantErr: "channel/connection is not open",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			svc := chat.NewService(&failingCompletionRepo{err: tc.repoErr}, nil, nil)
			handler := NewLifecycleHandler(svc, slog.New(slog.NewJSONHandler(&logs, nil)))
			eventID, errandID := uuid.NewString(), uuid.NewString()
			body := strings.NewReplacer("{eventId}", eventID, "{errandId}", errandID).Replace(tc.body)
			acker := &fakeAcker{ackErr: tc.ackErr}
			d := amqp.Delivery{Acknowledger: acker, RoutingKey: tc.routingKey, CorrelationId: correlationID, Body: []byte(body)}
			if tc.deliveryCount > 0 {
				d.Headers = amqp.Table{"x-delivery-count": tc.deliveryCount}
			}

			handler.HandleDelivery(context.Background(), d)

			assert.Equal(t, tc.wantOutcome, acker.outcome)
			var lines []map[string]any
			sc := bufio.NewScanner(&logs)
			for sc.Scan() {
				var line map[string]any
				require.NoError(t, json.Unmarshal(sc.Bytes(), &line), sc.Text())
				lines = append(lines, line)
			}
			require.Len(t, lines, 1, "one line per delivery")
			line := lines[0]
			assert.Equal(t, tc.wantLevel, line["level"])
			assert.Equal(t, tc.wantMsg, line["msg"])
			assert.Equal(t, correlationID, line["request_id"])
			assert.Equal(t, tc.routingKey, line["routingKey"])
			if tc.noEventID {
				assert.NotContains(t, line, "eventId")
			} else {
				assert.Equal(t, eventID, line["eventId"])
			}
			if tc.noErrandID {
				assert.NotContains(t, line, "errandId")
			} else {
				assert.Equal(t, errandID, line["errandId"])
			}
			if tc.wantErr == "" {
				assert.NotContains(t, line, "err")
			} else {
				assert.Contains(t, line["err"], tc.wantErr)
			}
		})
	}
}
