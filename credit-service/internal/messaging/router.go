// Package messaging adapts broker events into credit-service use cases.
package messaging

import (
	"context"
	"fmt"
	"log/slog"
)

// EventHandler handles a specific event routing key and payload.
type EventHandler interface {
	Handle(ctx context.Context, routingKey string, body []byte) error
}

// EventHandlerFunc is an adapter to allow the use of ordinary functions as EventHandlers.
type EventHandlerFunc func(ctx context.Context, routingKey string, body []byte) error

// Handle calls f(ctx, routingKey, body).
func (f EventHandlerFunc) Handle(ctx context.Context, routingKey string, body []byte) error {
	return f(ctx, routingKey, body)
}

// EventRouter maps routing keys to dedicated EventHandlers.
type EventRouter struct {
	handlers map[string]EventHandler
	logger   *slog.Logger
}

// NewEventRouter constructs a new EventRouter.
func NewEventRouter(logger *slog.Logger) *EventRouter {
	if logger == nil {
		logger = slog.Default()
	}
	return &EventRouter{
		handlers: make(map[string]EventHandler),
		logger:   logger,
	}
}

// Register registers an EventHandler for a specific routing key.
func (r *EventRouter) Register(routingKey string, h EventHandler) {
	r.handlers[routingKey] = h
}

// SupportedRoutingKeys returns all routing keys registered with the router.
func (r *EventRouter) SupportedRoutingKeys() []string {
	keys := make([]string, 0, len(r.handlers))
	for k := range r.handlers {
		keys = append(keys, k)
	}
	return keys
}

// Dispatch routes an event by its routing key to the registered EventHandler.
func (r *EventRouter) Dispatch(ctx context.Context, routingKey string, body []byte) error {
	h, ok := r.handlers[routingKey]
	if !ok {
		return fmt.Errorf("%w: unsupported routing key %s", ErrMalformedEvent, routingKey)
	}
	return h.Handle(ctx, routingKey, body)
}
