// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Implemented RabbitMQ ConnectionSupervisor with passive topology verification, reconnection loop with jittered backoff, and consumer pool management per sourceOfTruth.md Phase 11.
// Author review: PENDING - reviewer to complete

package messaging

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Supervisor coordinates RabbitMQ connection, consumer workers, and readiness status.
type Supervisor struct {
	amqpURL             string
	registrationHandler *RegistrationHandler
	lifecycleHandler    *LifecycleHandler
	workerConcurrency   int
	logger              *slog.Logger
	ready               atomic.Bool
}

// NewSupervisor constructs a new RabbitMQ connection supervisor.
func NewSupervisor(amqpURL string, regHandler *RegistrationHandler, lifeHandler *LifecycleHandler, workerConcurrency int, logger *slog.Logger) *Supervisor {
	if workerConcurrency <= 0 {
		workerConcurrency = 2
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Supervisor{
		amqpURL:             amqpURL,
		registrationHandler: regHandler,
		lifecycleHandler:    lifeHandler,
		workerConcurrency:   workerConcurrency,
		logger:              logger,
	}
}

// IsReady returns true when connection is healthy and consumers are active.
func (s *Supervisor) IsReady() bool {
	return s.ready.Load()
}

// Run executes the connection and reconnection supervisor loop until ctx is cancelled.
func (s *Supervisor) Run(ctx context.Context) error {
	backoffSteps := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		30 * time.Second,
	}
	stepIdx := 0

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		err := s.connectAndConsume(ctx)
		s.ready.Store(false)

		if ctx.Err() != nil {
			return ctx.Err()
		}

		baseInterval := backoffSteps[stepIdx]
		if stepIdx < len(backoffSteps)-1 {
			stepIdx++
		}

		// Apply +/-20% jitter
		jitterFactor := 0.8 + rand.Float64()*0.4
		waitDuration := time.Duration(float64(baseInterval) * jitterFactor)

		s.logger.Warn("RabbitMQ connection lost or failed, reconnecting",
			"error", err,
			"wait", waitDuration.String(),
		)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitDuration):
		}
	}
}

func (s *Supervisor) connectAndConsume(ctx context.Context) error {
	conn, err := amqp.Dial(s.amqpURL)
	if err != nil {
		return fmt.Errorf("dial RabbitMQ: %w", err)
	}
	defer func() { _ = conn.Close() }()

	closeNotify := make(chan *amqp.Error, 1)
	conn.NotifyClose(closeNotify)

	// Passively verify required exchanges and queues
	checkCh, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("open verification channel: %w", err)
	}
	if err := s.verifyTopology(checkCh); err != nil {
		_ = checkCh.Close()
		return fmt.Errorf("topology verification failed: %w", err)
	}
	_ = checkCh.Close()

	consumerCtx, cancelConsumers := context.WithCancel(ctx)
	defer cancelConsumers()

	var wg sync.WaitGroup

	// Start Registration Consumers
	for i := 0; i < s.workerConcurrency; i++ {
		ch, err := conn.Channel()
		if err != nil {
			return fmt.Errorf("open registration channel: %w", err)
		}
		defer func() { _ = ch.Close() }()

		if err := ch.Qos(1, 0, false); err != nil {
			return fmt.Errorf("set qos on registration channel: %w", err)
		}

		consumerTag := fmt.Sprintf("%s-%d", RegistrationConsumer, i)
		deliveries, err := ch.ConsumeWithContext(consumerCtx, RegistrationQueue, consumerTag, false, false, false, false, nil)
		if err != nil {
			return fmt.Errorf("consume registration: %w", err)
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			for d := range deliveries {
				s.registrationHandler.HandleDelivery(consumerCtx, d)
			}
		}()
	}

	// Start Lifecycle Consumers
	for i := 0; i < s.workerConcurrency; i++ {
		ch, err := conn.Channel()
		if err != nil {
			return fmt.Errorf("open lifecycle channel: %w", err)
		}
		defer func() { _ = ch.Close() }()

		if err := ch.Qos(1, 0, false); err != nil {
			return fmt.Errorf("set qos on lifecycle channel: %w", err)
		}

		consumerTag := fmt.Sprintf("%s-%d", LifecycleConsumer, i)
		deliveries, err := ch.ConsumeWithContext(consumerCtx, LifecycleQueue, consumerTag, false, false, false, false, nil)
		if err != nil {
			return fmt.Errorf("consume lifecycle: %w", err)
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			for d := range deliveries {
				s.lifecycleHandler.HandleDelivery(consumerCtx, d)
			}
		}()
	}

	// Mark supervisor ready once all consumers are running
	s.ready.Store(true)
	s.logger.Info("RabbitMQ supervisor active with verified topology and active consumers")

	select {
	case <-ctx.Done():
		cancelConsumers()
		wg.Wait()
		return ctx.Err()
	case amqpErr := <-closeNotify:
		cancelConsumers()
		wg.Wait()
		if amqpErr != nil {
			return amqpErr
		}
		return errors.New("connection closed cleanly")
	}
}

func (s *Supervisor) verifyTopology(ch *amqp.Channel) error {
	// Passively declare exchanges
	if err := ch.ExchangeDeclarePassive("foc.events", "topic", true, false, false, false, nil); err != nil {
		return fmt.Errorf("passive exchange foc.events: %w", err)
	}
	if err := ch.ExchangeDeclarePassive("foc.dlx", "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("passive exchange foc.dlx: %w", err)
	}

	// Passively declare queues
	queues := []string{
		RegistrationQueue,
		"credit.registration.dlq",
		LifecycleQueue,
		"credit.errand.lifecycle.dlq",
	}
	for _, q := range queues {
		if _, err := ch.QueueDeclarePassive(q, true, false, false, false, nil); err != nil {
			return fmt.Errorf("passive queue %s: %w", q, err)
		}
	}

	return nil
}
