package messaging_test

import (
	"context"
	"errors"
	"testing"

	"foc/credit-service/internal/messaging"
)

func TestEventRouter_Dispatch(t *testing.T) {
	router := messaging.NewEventRouter(nil)

	handledKey := ""
	handledBody := ""

	router.Register("test.event", messaging.EventHandlerFunc(func(ctx context.Context, routingKey string, body []byte) error {
		handledKey = routingKey
		handledBody = string(body)
		return nil
	}))

	router.Register("error.event", messaging.EventHandlerFunc(func(ctx context.Context, routingKey string, body []byte) error {
		return errors.New("boom")
	}))

	t.Run("dispatches to registered handler", func(t *testing.T) {
		err := router.Dispatch(context.Background(), "test.event", []byte("hello"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if handledKey != "test.event" || handledBody != "hello" {
			t.Fatalf("handler received key=%q body=%q", handledKey, handledBody)
		}
	})

	t.Run("propagates handler error", func(t *testing.T) {
		err := router.Dispatch(context.Background(), "error.event", []byte("bad"))
		if err == nil || err.Error() != "boom" {
			t.Fatalf("expected 'boom' error, got: %v", err)
		}
	})

	t.Run("returns malformed event on unknown routing key", func(t *testing.T) {
		err := router.Dispatch(context.Background(), "unknown.key", []byte("data"))
		if !errors.Is(err, messaging.ErrMalformedEvent) {
			t.Fatalf("expected ErrMalformedEvent, got: %v", err)
		}
	})

	t.Run("returns supported routing keys", func(t *testing.T) {
		keys := router.SupportedRoutingKeys()
		if len(keys) != 2 {
			t.Fatalf("expected 2 keys, got %d", len(keys))
		}
	})
}
