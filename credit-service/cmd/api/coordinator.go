// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Implemented first-error coordination group for startup and graceful shutdown per sourceOfTruth.md Phase 12.
// Author review: PENDING - reviewer to complete

package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
)

// coordinator manages component lifecycles, retains the first fatal error,
// and coordinates intake cancellation without blocking callers.
type coordinator struct {
	cancelIntake context.CancelFunc
	logger       *slog.Logger
	mu           sync.Mutex
	firstErr     error
	firstOrigin  string
	wg           sync.WaitGroup
}

func newCoordinator(cancelIntake context.CancelFunc, logger *slog.Logger) *coordinator {
	if logger == nil {
		logger = slog.Default()
	}
	return &coordinator{
		cancelIntake: cancelIntake,
		logger:       logger,
	}
}

// report registers a component exit. If err is fatal, it records the first error
// and cancels the shared intake context. Secondary errors are logged.
func (c *coordinator) report(component string, err error) {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, http.ErrServerClosed) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.firstErr == nil {
		c.firstErr = err
		c.firstOrigin = component
		c.logger.Error("component fatal error, initiating shutdown", "component", component, "error", err)
		c.cancelIntake()
	} else {
		c.logger.Error("secondary error during shutdown", "component", component, "error", err)
	}
}

// firstError returns the first fatal error recorded by any component, or nil.
func (c *coordinator) firstError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.firstErr
}
