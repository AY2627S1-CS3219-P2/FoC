// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
// Scope: Unit tests of how a delivery is answered, and a check that the queue in
//   rabbitmq/definitions.json keeps D-143's settings and D-141's bindings.
// Author review: PENDING — <reviewer to complete>

package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/notification-service/internal/notification"
)

type recordingSettler struct {
	acked, requeued, dropped int
}

func (r *recordingSettler) Ack(bool) error { r.acked++; return nil }
func (r *recordingSettler) Nack(_, requeue bool) error {
	if requeue {
		r.requeued++
	} else {
		r.dropped++
	}
	return nil
}

type stubHandler struct {
	got []notification.Event
	err error
}

func (h *stubHandler) Handle(_ context.Context, e notification.Event) error {
	h.got = append(h.got, e)
	return h.err
}

func newConsumer(h EventHandler) *Consumer {
	c := New("amqp://unused", h, slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.retryDelay = 0
	return c
}

func delivery(key, body string) amqp.Delivery {
	return amqp.Delivery{RoutingKey: key, Body: []byte(body)}
}

func TestSettle(t *testing.T) {
	eventID, errandID, requesterID, courierID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	good := fmt.Sprintf(
		`{"eventId":%q,"eventType":"errand.accepted","errandId":%q,"requesterId":%q,"courierId":%q,"amount":7,"occurredAt":"2026-11-05T12:00:00Z","correlationId":"c1"}`,
		eventID, errandID, requesterID, courierID)

	t.Run("a handled event is acked, with the payload's fields passed on", func(t *testing.T) {
		h, s := &stubHandler{}, &recordingSettler{}
		require.NoError(t, newConsumer(h).settle(t.Context(), delivery("errand.accepted", good), s))
		assert.Equal(t, recordingSettler{acked: 1}, *s)
		require.Len(t, h.got, 1)
		assert.Equal(t, notification.Event{
			ID: eventID, Type: "errand.accepted", ErrandID: errandID, RequesterID: requesterID,
			CourierID: &courierID, Amount: 7, OccurredAt: time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC),
		}, h.got[0])
	})

	t.Run("malformed JSON is dead-lettered without reaching the handler", func(t *testing.T) {
		h, s := &stubHandler{}, &recordingSettler{}
		require.NoError(t, newConsumer(h).settle(t.Context(), delivery("errand.accepted", "not json"), s))
		assert.Equal(t, recordingSettler{dropped: 1}, *s)
		assert.Empty(t, h.got)
	})

	t.Run("an event the service refuses is dead-lettered", func(t *testing.T) {
		h := &stubHandler{err: fmt.Errorf("%w: no courier", notification.ErrInvalidEvent)}
		s := &recordingSettler{}
		require.NoError(t, newConsumer(h).settle(t.Context(), delivery("errand.accepted", good), s))
		assert.Equal(t, recordingSettler{dropped: 1}, *s)
	})

	t.Run("a storage failure goes back on the queue", func(t *testing.T) {
		h, s := &stubHandler{err: errors.New("database down")}, &recordingSettler{}
		require.NoError(t, newConsumer(h).settle(t.Context(), delivery("errand.accepted", good), s))
		assert.Equal(t, recordingSettler{requeued: 1}, *s)
	})
}

func TestRunStopsWhenItsContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	done := make(chan struct{})
	go func() {
		newConsumer(&stubHandler{}).Run(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after its context ended")
	}
}

type definitions struct {
	Queues []struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"queues"`
	Bindings []struct {
		Source      string `json:"source"`
		Destination string `json:"destination"`
		RoutingKey  string `json:"routing_key"`
	} `json:"bindings"`
}

// TestTheQueueCannotPushBackOnPublishers guards D-140 and D-143. With
// reject-publish a full queue nacks the publisher, and order-service's relay
// would keep retrying, so a stopped notification-service would reach it.
func TestTheQueueCannotPushBackOnPublishers(t *testing.T) {
	raw, err := os.ReadFile("../../../rabbitmq/definitions.json")
	require.NoError(t, err)
	var defs definitions
	require.NoError(t, json.Unmarshal(raw, &defs))

	var args map[string]any
	for _, q := range defs.Queues {
		if q.Name == Queue {
			args = q.Arguments
		}
	}
	require.NotNil(t, args, "queue %s must be declared", Queue)
	assert.Equal(t, "drop-head", args["x-overflow"])
	assert.Equal(t, "at-most-once", args["x-dead-letter-strategy"])
	assert.Equal(t, "foc.dlx", args["x-dead-letter-exchange"])

	var bound []string
	for _, b := range defs.Bindings {
		if b.Source == "foc.events" && b.Destination == Queue {
			bound = append(bound, b.RoutingKey)
		}
	}
	assert.ElementsMatch(t, []string{
		notification.EventAccepted, notification.EventPickedUp, notification.EventDelivered,
		notification.EventWithdrawn, notification.EventCompleted, notification.EventCancelled,
		notification.EventExpired,
	}, bound)
}
