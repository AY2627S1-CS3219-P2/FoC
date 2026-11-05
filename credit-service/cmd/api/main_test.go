// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-04
// Scope: Tests that net/http's own errors reach the JSON logger and that main's
//   fatal line carries no connection-string credentials (D-135).
// Author review: James Teo — reviewed and verified.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// runMainEnv makes the test binary run main in place of the test that sets it,
// so that a test can watch main exit.
const runMainEnv = "CREDIT_SERVICE_TEST_RUN_MAIN"

func TestHTTPServerErrorsAreLoggedAsJSON(t *testing.T) {
	var logs bytes.Buffer
	server := newHTTPServer(":0", http.NotFoundHandler(), slog.New(slog.NewJSONHandler(&logs, nil)))

	server.ErrorLog.Print("http: superfluous response.WriteHeader call")

	var line map[string]any
	if err := json.Unmarshal(logs.Bytes(), &line); err != nil {
		t.Fatalf("net/http's error is not one JSON line: %v: %s", err, logs.String())
	}
	want := map[string]any{
		"level":     "ERROR",
		"msg":       "http: superfluous response.WriteHeader call",
		"component": "http_server",
	}
	for key, value := range want {
		if line[key] != value {
			t.Errorf("%s = %v, want %v", key, line[key], value)
		}
	}
}

func TestFatalLineCarriesNoCredentials(t *testing.T) {
	if os.Getenv(runMainEnv) == "1" {
		main()
		return
	}
	cases := []struct {
		name  string
		dbURL string
	}{
		// pgx's own redaction keeps what precedes the password's last ':'.
		{name: "a password holding ':' and '/'", dbURL: "postgres://credit:s3cr:et99/x@credit-db:5432/credit"},
		// url.Parse quotes the password's first piece back as a port.
		{name: "a password holding '#'", dbURL: "postgres://credit:s3cr#et99@credit-db:5432/credit"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFatalLineCarriesNoCredentials$")
			// The database URL does not parse, so main exits before dialling anything.
			cmd.Env = append(os.Environ(), runMainEnv+"=1", "CREDIT_DB_URL="+tc.dbURL,
				"JWKS_URL=http://127.0.0.1:1/jwks", "RABBITMQ_URL=amqp://127.0.0.1:1/")

			out, err := cmd.Output()

			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 {
				t.Fatalf("main ended with %v, want exit status 1; stdout: %s", err, out)
			}
			var fatal []map[string]any
			for _, raw := range bytes.Split(bytes.TrimSpace(out), []byte("\n")) {
				var line map[string]any
				if err := json.Unmarshal(raw, &line); err != nil {
					t.Fatalf("stdout line is not one JSON object: %v: %s", err, raw)
				}
				if line["msg"] == "credit service stopped with fatal error" {
					fatal = append(fatal, line)
				}
			}
			if len(fatal) != 1 {
				t.Fatalf("found %d fatal lines, want 1; stdout: %s", len(fatal), out)
			}
			if got, _ := fatal[0]["error"].(string); fatal[0]["level"] != "ERROR" || !strings.Contains(got, "postgres://REDACTED@credit-db:5432/credit") {
				t.Errorf("fatal line = %v, want an ERROR naming the database URL redacted", fatal[0])
			}
			for _, piece := range []string{"s3cr", "et99"} {
				if bytes.Contains(out, []byte(piece)) {
					t.Errorf("%q from the password was logged: %s", piece, out)
				}
			}
		})
	}
}
