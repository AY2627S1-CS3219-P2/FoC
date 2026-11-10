// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-20
// Scope: Added focused startup configuration tests for JWT token lifetimes.
// Author review: Validated tests reflects intended behvaiour

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"foc/user-service/internal/config"
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
	server := newHTTPServer(":8081", http.NotFoundHandler(), slog.New(slog.DiscardHandler))

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

func TestNewHTTPServerWritesNetHTTPErrorsAsJSON(t *testing.T) {
	var logs bytes.Buffer
	server := newHTTPServer(":8081", http.NotFoundHandler(), slog.New(slog.NewJSONHandler(&logs, nil)))

	server.ErrorLog.Printf("http: Accept error: %s; retrying in 5ms", "too many open files")

	var line map[string]any
	if err := json.Unmarshal(logs.Bytes(), &line); err != nil {
		t.Fatalf("ErrorLog wrote %q, want one JSON line: %v", logs.String(), err)
	}
	if line["level"] != "ERROR" || line["msg"] != "http: Accept error: too many open files; retrying in 5ms" {
		t.Fatalf("ErrorLog line = %v, want net/http's message at ERROR", line)
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

// A malformed USER_DB_URL or REDIS_URL stops run with an error that names the
// variable, and the fatal line main logs from it holds no piece of the
// password, whatever characters the password holds.
func TestRunKeepsConnectionPasswordsOutOfTheFatalLine(t *testing.T) {
	// run applies ./migrations, so from the service root, as in the container,
	// an unchecked USER_DB_URL would reach golang-migrate's parse of it.
	t.Chdir(filepath.Join("..", ".."))
	const (
		userDBURL = "postgres://svc:pw@127.0.0.1:1/users"
		redisURL  = "redis://127.0.0.1:1/0"
	)
	tests := map[string]struct {
		cfg      config.Config
		variable string   // the malformed one, which the error must name
		pieces   []string // of its password, none of which may be logged
	}{
		"database password with a slash":                {config.Config{UserDBURL: "postgres://svc:Pq8/Ws3@db:5432/users", RedisURL: redisURL}, "USER_DB_URL", []string{"Pq8", "Ws3"}},
		"database password with a colon and a slash":    {config.Config{UserDBURL: "postgres://svc:Sc4:Et9/Qz4@db:5432/users", RedisURL: redisURL}, "USER_DB_URL", []string{"Sc4", "Et9", "Qz4"}},
		"database password with a space":                {config.Config{UserDBURL: "postgres://svc:Xq7 Zr9@db:5432/users", RedisURL: redisURL}, "USER_DB_URL", []string{"Xq7", "Zr9"}},
		"database password with a double quote":         {config.Config{UserDBURL: `postgres://svc:Xq7"Zr9@db:5432/users`, RedisURL: redisURL}, "USER_DB_URL", []string{"Xq7", "Zr9"}},
		"database password with a hash":                 {config.Config{UserDBURL: "postgres://svc:Xq7#Zr9@db:5432/users", RedisURL: redisURL}, "USER_DB_URL", []string{"Xq7", "Zr9"}},
		"database password with a stray percent sign":   {config.Config{UserDBURL: "postgres://svc:Xq7%zzZr9@db:5432/users", RedisURL: redisURL}, "USER_DB_URL", []string{"Xq7", "%zz", "Zr9"}},
		"database settings in keyword=value form":       {config.Config{UserDBURL: "user=svc password=Xq7:Zr9 host=db dbname=users", RedisURL: redisURL}, "USER_DB_URL", []string{"Xq7", "Zr9"}},
		"database URL with no slashes after its scheme": {config.Config{UserDBURL: "postgres:svc:Xq7:Zr9@db:5432/users", RedisURL: redisURL}, "USER_DB_URL", []string{"Xq7", "Zr9"}},
		"Redis password with a slash":                   {config.Config{UserDBURL: userDBURL, RedisURL: "redis://default:Pq8/Ws3@redis:6379/0"}, "REDIS_URL", []string{"Pq8", "Ws3"}},
		"Redis URL with one slash after its scheme":     {config.Config{UserDBURL: userDBURL, RedisURL: "redis:/default:Xq7Zr9@redis:6379"}, "REDIS_URL", []string{"Xq7", "Zr9"}},
		// AI-generated (edited by PENDING): USER_RANKING_DB_URL gets the same
		// check, per James Teo's PR #70 comment of 2026-11-05.
		"ranking database password with a slash":          {config.Config{UserDBURL: userDBURL, RankingDBURL: "postgres://svc:Pq8/Ws3@db:5432/ranking", RedisURL: redisURL}, "USER_RANKING_DB_URL", []string{"Pq8", "Ws3"}},
		"ranking database settings in keyword=value form": {config.Config{UserDBURL: userDBURL, RankingDBURL: "user=svc password=Xq7:Zr9 host=db dbname=ranking", RedisURL: redisURL}, "USER_RANKING_DB_URL", []string{"Xq7", "Zr9"}},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))

			err := run(context.Background(), tt.cfg, logger)
			if err == nil {
				t.Fatal("run() error = nil, want the malformed URL rejected")
			}
			logger.Error("user service stopped", "error", err) // main's fatal line

			if !strings.Contains(err.Error(), tt.variable) {
				t.Errorf("run() error = %q, want it to name %s", err, tt.variable)
			}
			for _, piece := range tt.pieces {
				if strings.Contains(logs.String(), piece) {
					t.Errorf("fatal line %s holds %q from the password", logs.String(), piece)
				}
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
