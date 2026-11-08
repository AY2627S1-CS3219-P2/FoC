//go:build e2e && observability

// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-04
// Scope: The evidence for NFR7.1's 30-second clause (D-136): with
//   supplier-service stopped, order-service's ERROR line for an errand post's
//   503 must be in Loki within 30 seconds. Its own observability tag keeps it
//   out of the suite's run.
// Author review: James Teo — reviewed and verified.

package e2e

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	defaultLokiURL = "http://127.0.0.1:3100"
	// errorVisibleWithin is NFR7.1's bound on how soon a high-severity error
	// shows in the centralised logging view.
	errorVisibleWithin = 30 * time.Second
	// lokiReadyTimeout lets a Loki that has only just started settle: it
	// answers /ready some seconds after it starts.
	lokiReadyTimeout = 30 * time.Second
	// lokiWindow is how far back a query looks. Loki counts it back from its
	// own clock, so the host's clock cannot move the line out of range.
	lokiWindow = "5m"
	// composeTimeout bounds one docker compose command: the stop, or the start
	// and the wait for the healthcheck to pass.
	composeTimeout = 2 * time.Minute
)

// TestOrderServiceErrorReachesLokiWithin30Seconds stops supplier-service, so
// order-service answers an errand post with 503 and logs it at ERROR under the
// post's X-Request-Id, and expects that line in Loki within 30 seconds of the
// 503 (NFR7.1, D-136). README.md has the command that runs it.
func TestOrderServiceErrorReachesLokiWithin30Seconds(t *testing.T) {
	// Not parallel: while supplier-service is stopped no test can post an errand.
	s := newStack(t)
	loki := newLokiClient()
	loki.requireReady(t)

	u := s.newStudent(t)
	// Built while supplier-service is up, so only its outage can fail the post.
	in := s.errandInput(t, u, 1)
	stopSupplierService(t)

	requestID := randomUUID()
	header := http.Header{}
	header.Set("X-Request-Id", requestID)
	r, err := s.request(t.Context(), http.MethodPost, ordersPath, u.token, in, header)
	answered := time.Now()
	require.NoError(t, err)
	expectStatus(t, r, http.StatusServiceUnavailable)

	query := fmt.Sprintf(`{service="order-service", level="ERROR"} |= %q`, requestID)
	ctx, cancel := context.WithDeadline(t.Context(), answered.Add(errorVisibleWithin))
	defer cancel()
	var took time.Duration
	var line string
	last, ok := poll(ctx, func(ctx context.Context) (bool, string) {
		lines, err := loki.lines(ctx, query)
		if err != nil {
			return false, err.Error()
		}
		if len(lines) == 0 {
			return false, "no line matches yet"
		}
		took, line = time.Since(answered), lines[0]
		return true, ""
	})
	if !ok {
		t.Fatalf("Loki holds no order-service line at ERROR with request id %s %s after the 503 (last: %s); query: %s; the post: %s",
			requestID, errorVisibleWithin, last, query, r)
	}
	t.Logf("order-service's ERROR line for request %s was in Loki %s after the 503: %s",
		requestID, took.Round(time.Millisecond), line)
}

// stopSupplierService stops supplier-service's container until the test ends,
// then starts it again and waits until its healthcheck passes, so the stack is
// left as the test found it.
func stopSupplierService(t *testing.T) {
	t.Helper()
	// Registered first, so a stop that fails part way is undone too.
	t.Cleanup(func() {
		// t.Context() is already cancelled when a cleanup runs.
		ctx, cancel := context.WithTimeout(context.Background(), composeTimeout)
		defer cancel()
		// up --no-recreate starts the stopped container as start would, and
		// can also wait for its healthcheck (--wait).
		if err := dockerCompose(ctx, "up", "--no-deps", "--no-recreate", "--wait", "supplier-service"); err != nil {
			t.Errorf("supplier-service is not healthy again: %v; start it with `docker compose start supplier-service`", err)
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), composeTimeout)
	defer cancel()
	require.NoError(t, dockerCompose(ctx, "stop", "supplier-service"))
}

// dockerCompose runs docker compose in the repository root, as
// verificationCodes does, so it acts on the stack docker compose up started
// there.
func dockerCompose(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", append([]string{"compose"}, args...)...)
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker compose %s: %w: %s", strings.Join(args, " "), err, bytes.TrimSpace(out))
	}
	return nil
}

// lokiClient queries the Loki the observability profile publishes, which
// stack.request cannot reach: it calls only the gateway.
type lokiClient struct {
	baseURL string
	client  *http.Client
}

func newLokiClient() lokiClient {
	return lokiClient{
		baseURL: strings.TrimRight(envOr("E2E_LOKI_URL", defaultLokiURL), "/"),
		client:  newHTTPClient(),
	}
}

// requireReady waits for Loki to answer /ready, and fails the test, before
// anything is stopped, if it never does.
func (l lokiClient) requireReady(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), lokiReadyTimeout)
	defer cancel()
	last, ok := poll(ctx, func(ctx context.Context) (bool, string) {
		r, err := l.get(ctx, "/ready", nil)
		if err != nil {
			return false, err.Error()
		}
		return r.status == http.StatusOK, r.String()
	})
	if !ok {
		t.Fatalf("Loki is not ready at %s within %s (last: %s). Start the stack with its observability profile, "+
			"`docker compose --profile observability up --build --wait`, or set E2E_LOKI_URL to where Loki listens",
			l.baseURL, lokiReadyTimeout, last)
	}
}

// lines returns the lines query selects from the last lokiWindow, through
// Loki's query_range API.
func (l lokiClient) lines(ctx context.Context, query string) ([]string, error) {
	r, err := l.get(ctx, "/loki/api/v1/query_range", url.Values{"query": {query}, "since": {lokiWindow}})
	if err != nil {
		return nil, err
	}
	if r.status != http.StatusOK {
		return nil, errors.New(r.String())
	}
	var answer struct {
		Data struct {
			Result []struct {
				// Each value is a [timestamp, line] pair.
				Values [][2]string `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := r.decode(&answer); err != nil {
		return nil, err
	}
	var lines []string
	for _, stream := range answer.Data.Result {
		for _, v := range stream.Values {
			lines = append(lines, v[1])
		}
	}
	return lines, nil
}

func (l lokiClient) get(ctx context.Context, path string, query url.Values) (response, error) {
	target := l.baseURL + path
	if query != nil {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return response{}, fmt.Errorf("build GET %s: %w", path, err)
	}
	res, err := l.client.Do(req)
	if err != nil {
		return response{}, fmt.Errorf("GET %s: %w", path, err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBodyBytes))
	if err != nil {
		return response{}, fmt.Errorf("read the answer to GET %s: %w", path, err)
	}
	return response{method: http.MethodGet, path: path, status: res.StatusCode, header: res.Header, body: raw}, nil
}
