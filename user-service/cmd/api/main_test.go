// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-20
// Scope: Added focused startup configuration tests for JWT token lifetimes.
// Author review: Validated tests reflects intended behvaiour

package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestParseTokenTTL(t *testing.T) {
	tests := map[string]struct {
		value        string
		defaultValue time.Duration
		want         time.Duration
		wantError    bool
	}{
		"uses recorded default when unset": {defaultValue: 15 * time.Minute, want: 15 * time.Minute},
		"parses configured duration":       {value: "168h", defaultValue: time.Minute, want: 168 * time.Hour},
		"rejects malformed duration":       {value: "seven days", defaultValue: time.Minute, wantError: true},
		"rejects non-positive duration":    {value: "0s", defaultValue: time.Minute, wantError: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := parseTokenTTL(tt.value, tt.defaultValue)
			if (err != nil) != tt.wantError {
				t.Fatalf("parseTokenTTL() error = %v, wantError %v", err, tt.wantError)
			}
			if err == nil && got != tt.want {
				t.Fatalf("parseTokenTTL() = %v, want %v", got, tt.want)
			}
		})
	}
}

// AI-generated (edited by ZI YANG): the server timeouts match the recorded defensive policy.
func TestNewHTTPServerUsesRecordedTimeouts(t *testing.T) {
	server := newHTTPServer(":8081", http.NotFoundHandler())

	if server.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("ReadHeaderTimeout = %s, want 5s", server.ReadHeaderTimeout)
	}
	if server.ReadTimeout != 10*time.Second {
		t.Fatalf("ReadTimeout = %s, want 10s", server.ReadTimeout)
	}
	if server.WriteTimeout != 10*time.Second {
		t.Fatalf("WriteTimeout = %s, want 10s", server.WriteTimeout)
	}
	if server.IdleTimeout != 120*time.Second {
		t.Fatalf("IdleTimeout = %s, want 120s", server.IdleTimeout)
	}
}

// AI-generated (edited by ZI YANG): health must fail when either required persistence dependency is unavailable.
func TestNewHealthCheckIncludesRedis(t *testing.T) {
	databaseFailure := errors.New("database unavailable")
	redisFailure := errors.New("redis unavailable")
	tests := map[string]struct {
		databaseErr error
		redisErr    error
		wantErr     error
	}{
		"healthy dependencies": {},
		"database unavailable": {databaseErr: databaseFailure, wantErr: databaseFailure},
		"redis unavailable":    {redisErr: redisFailure, wantErr: redisFailure},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			check := newHealthCheck(
				func(context.Context) error { return tt.databaseErr },
				func(context.Context) error { return tt.redisErr },
			)

			err := check(context.Background())
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("health check error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// AI-generated (edited by James Teo): RABBITMQ_URL defaults for local
// development and is rejected, without echoing its password, unless it is an
// AMQP URL.
func TestParseRabbitMQURL(t *testing.T) {
	const password = "s3cret-password"
	tests := map[string]struct {
		value     string
		want      string
		wantError bool
	}{
		"uses the local default when unset":  {want: defaultRabbitMQURL},
		"keeps an amqp URL":                  {value: "amqp://foc:" + password + "@rabbitmq:5672/foc", want: "amqp://foc:" + password + "@rabbitmq:5672/foc"},
		"keeps an amqps URL":                 {value: "amqps://foc:" + password + "@broker.example:5671/foc", want: "amqps://foc:" + password + "@broker.example:5671/foc"},
		"rejects another scheme":             {value: "http://foc:" + password + "@rabbitmq:5672/foc", wantError: true},
		"rejects a URL net/url cannot parse": {value: "amqp://foc:" + password + "@rabbitmq:56 72/foc", wantError: true},
		"rejects a value with no scheme":     {value: "rabbitmq:5672", wantError: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := parseRabbitMQURL(tt.value)
			if (err != nil) != tt.wantError {
				t.Fatalf("parseRabbitMQURL() error = %v, wantError %v", err, tt.wantError)
			}
			if err != nil {
				if strings.Contains(err.Error(), password) {
					t.Fatalf("parseRabbitMQURL() error %q leaks the broker password", err)
				}
				return
			}
			if got != tt.want {
				t.Fatalf("parseRabbitMQURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

// AI-generated (edited by James Teo): stopping the outbox worker waits for it to
// return, so it never outlives the database pool it uses.
func TestStartBackgroundStopsAndWaitsForWork(t *testing.T) {
	started := make(chan struct{})
	var returned bool
	stop := startBackground(context.Background(), func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		returned = true
	})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("background work did not start within 5s")
	}

	stopped := make(chan struct{})
	go func() {
		stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not return within 5s of cancelling the work")
	}
	if !returned {
		t.Fatal("stop returned before the background work did")
	}
}
