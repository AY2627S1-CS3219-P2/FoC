//go:build integration

// AI Assistance Disclosure:
// Tool: Codex (model: GPT-5), date: 2026-10-14
// Scope: Added RabbitMQ-to-Postgres integration test for initial credit allocation.
// Author review: PENDING - reviewer to complete

package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"testing"
	"time"

	"foc/credit-service/internal/credit"
	"foc/credit-service/internal/repository"

	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestRegistrationConsumerAllocatesInitialCredits(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	postgresURL := startPostgres(ctx, t)
	amqpURL := startRabbitMQ(ctx, t)

	if err := repository.Apply(postgresURL, "file://../../migrations"); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	pool, err := pgxpool.New(ctx, postgresURL)
	if err != nil {
		t.Fatalf("create pg pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := declareRegistrationTopology(amqpURL); err != nil {
		t.Fatalf("declare RabbitMQ topology: %v", err)
	}

	consumerCtx, stopConsumer := context.WithCancel(ctx)
	t.Cleanup(stopConsumer)
	handler := NewRegistrationHandler(credit.NewService(repository.NewPostgresRepository(pool)))
	consumerErr := make(chan error, 1)
	go func() {
		consumerErr <- RunRegistrationConsumer(consumerCtx, amqpURL, handler, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	}()

	body := []byte(`{
		"eventId": "550e8400-e29b-41d4-a716-446655440000",
		"userId": "6ba7b810-9dad-41d1-80b4-00c04fd430c8",
		"occurredAt": "2026-10-14T14:00:00Z"
	}`)
	if err := publishUserRegistered(amqpURL, body); err != nil {
		t.Fatalf("publish user.registered: %v", err)
	}

	pollUntil(t, ctx, func(ctx context.Context) error {
		var availableCredits int
		var reservedCredits int
		err := pool.QueryRow(ctx, `
			SELECT available_credits, reserved_credits
			FROM credit_accounts
			WHERE user_id = '6ba7b810-9dad-41d1-80b4-00c04fd430c8'`,
		).Scan(&availableCredits, &reservedCredits)
		if err != nil {
			return err
		}
		if availableCredits != 100 || reservedCredits != 0 {
			return fmt.Errorf("credits = (%d, %d), want (100, 0)", availableCredits, reservedCredits)
		}
		return nil
	})

	if err := publishUserRegistered(amqpURL, body); err != nil {
		t.Fatalf("publish duplicate user.registered: %v", err)
	}

	time.Sleep(500 * time.Millisecond)
	var transactionCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM credit_transactions
		WHERE user_id = '6ba7b810-9dad-41d1-80b4-00c04fd430c8'`,
	).Scan(&transactionCount); err != nil {
		t.Fatalf("count transactions: %v", err)
	}
	if transactionCount != 1 {
		t.Fatalf("transaction count = %d, want 1", transactionCount)
	}

	stopConsumer()
	select {
	case err := <-consumerErr:
		if err != nil && err != context.Canceled {
			t.Fatalf("consumer error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("consumer did not stop")
	}
}

func startPostgres(ctx context.Context, t *testing.T) string {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:16-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_DB":       "credit",
				"POSTGRES_USER":     "credit",
				"POSTGRES_PASSWORD": "credit",
			},
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(45 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start Postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Fatalf("terminate Postgres container: %v", err)
		}
	})

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("Postgres host: %v", err)
	}
	port, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatalf("Postgres port: %v", err)
	}
	return fmt.Sprintf("postgres://credit:credit@%s:%s/credit?sslmode=disable", host, port.Port())
}

func startRabbitMQ(ctx context.Context, t *testing.T) string {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "rabbitmq:3.13-management",
			ExposedPorts: []string{"5672/tcp"},
			Env: map[string]string{
				"RABBITMQ_DEFAULT_USER":  "foc",
				"RABBITMQ_DEFAULT_PASS":  "foc-dev-password",
				"RABBITMQ_DEFAULT_VHOST": "foc",
			},
			WaitingFor: wait.ForListeningPort("5672/tcp").WithStartupTimeout(90 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start RabbitMQ container: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Fatalf("terminate RabbitMQ container: %v", err)
		}
	})

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("RabbitMQ host: %v", err)
	}
	port, err := container.MappedPort(ctx, "5672/tcp")
	if err != nil {
		t.Fatalf("RabbitMQ port: %v", err)
	}
	return fmt.Sprintf("amqp://foc:foc-dev-password@%s:%s/%s", host, port.Port(), url.PathEscape("foc"))
}

func declareRegistrationTopology(amqpURL string) error {
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer func() { _ = ch.Close() }()

	if err := ch.ExchangeDeclare("foc.events", "topic", true, false, false, false, nil); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare(registrationQueue, true, false, false, false, nil); err != nil {
		return err
	}
	return ch.QueueBind(registrationQueue, "user.registered", "foc.events", false, nil)
}

func publishUserRegistered(amqpURL string, body []byte) error {
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer func() { _ = ch.Close() }()

	return ch.Publish("foc.events", "user.registered", false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	})
}

func pollUntil(t *testing.T, ctx context.Context, check func(context.Context) error) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := check(ctx); err == nil {
			return
		} else {
			lastErr = err
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("condition not met: %v", lastErr)
}
