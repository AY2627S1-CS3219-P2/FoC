// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-16
// Scope: httptest tests for the order-service client: request path and
//   forwarded token, non-200 and malformed answers, timeout and cancellation.
// Author review: James Teo — reviewed and verified.

package orderclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const supplierID = "3f2b6c1e-8a4d-4c39-9f0e-5b7d2a1c6e48"

// seenRequest is what the fake order-service saw. It travels back on a
// channel because the handler runs on the server's goroutine.
type seenRequest struct {
	method        string
	escapedPath   string
	authorization string
	hasAuth       bool
}

// fakeOrderService starts an order-service that records each request and
// answers status and body. The channel holds the last request seen.
func fakeOrderService(t *testing.T, status int, body string) (*httptest.Server, <-chan seenRequest) {
	t.Helper()
	seen := make(chan seenRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hasAuth := r.Header["Authorization"]
		select {
		case seen <- seenRequest{
			method:        r.Method,
			escapedPath:   r.URL.EscapedPath(),
			authorization: r.Header.Get("Authorization"),
			hasAuth:       hasAuth,
		}:
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, seen
}

func TestActiveErrands_AsksOrderServiceWithTheAdminsToken(t *testing.T) {
	srv, seen := fakeOrderService(t, http.StatusOK,
		`{"supplierId":"`+supplierID+`","activeErrands":3}`)
	// A trailing slash on the base URL must not double the path's slash.
	c := New(srv.URL + "/")

	ctx := WithToken(context.Background(), "Bearer admin-token")
	n, err := c.ActiveErrands(ctx, supplierID)

	require.NoError(t, err)
	assert.Equal(t, 3, n)
	got := <-seen
	assert.Equal(t, http.MethodGet, got.method)
	assert.Equal(t, "/suppliers/"+supplierID+"/active-errands", got.escapedPath)
	assert.Equal(t, "Bearer admin-token", got.authorization)
}

func TestActiveErrands_EscapesTheSupplierID(t *testing.T) {
	srv, seen := fakeOrderService(t, http.StatusOK, `{"activeErrands":0}`)
	c := New(srv.URL)

	_, err := c.ActiveErrands(context.Background(), "a/b?c")

	require.NoError(t, err)
	assert.Equal(t, "/suppliers/a%2Fb%3Fc/active-errands", (<-seen).escapedPath)
}

func TestActiveErrands_SendsNoAuthorizationWithoutAToken(t *testing.T) {
	srv, seen := fakeOrderService(t, http.StatusUnauthorized, `{"error":"missing token"}`)
	c := New(srv.URL)

	_, err := c.ActiveErrands(context.Background(), supplierID)

	require.Error(t, err, "order-service's 401 must refuse the delete")
	assert.False(t, (<-seen).hasAuth)
}

func TestActiveErrands_RejectsAnythingButAWellFormed200(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "401 bad token", status: http.StatusUnauthorized, body: `{"error":"invalid token"}`},
		{name: "403 not an admin", status: http.StatusForbidden, body: `{"error":"admin only"}`},
		{name: "400 bad supplier id", status: http.StatusBadRequest, body: `{"error":"bad id"}`},
		{name: "404 no such route", status: http.StatusNotFound, body: `{"error":"not found"}`},
		{name: "500 order-service fault", status: http.StatusInternalServerError, body: `{"error":"boom"}`},
		{name: "200 not JSON", status: http.StatusOK, body: `not json`},
		{name: "200 without activeErrands", status: http.StatusOK, body: `{"supplierId":"` + supplierID + `"}`},
		{name: "200 null activeErrands", status: http.StatusOK, body: `{"activeErrands":null}`},
		{name: "200 activeErrands not a number", status: http.StatusOK, body: `{"activeErrands":"2"}`},
		{name: "200 negative activeErrands", status: http.StatusOK, body: `{"activeErrands":-1}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := fakeOrderService(t, tc.status, tc.body)
			c := New(srv.URL)

			n, err := c.ActiveErrands(WithToken(context.Background(), "Bearer admin-token"), supplierID)

			require.Error(t, err)
			assert.Zero(t, n)
			if tc.status != http.StatusOK {
				assert.Contains(t, err.Error(), strconv.Itoa(tc.status), "the error names the status")
			}
		})
	}
}

func TestActiveErrands_GivesUpAfterItsTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(srv.Close)
	// Registered after srv.Close, so it runs first and unblocks the handler.
	t.Cleanup(func() { close(release) })
	c := New(srv.URL)
	c.timeout = 50 * time.Millisecond

	_, err := c.ActiveErrands(context.Background(), supplierID)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestActiveErrands_StopsWhenTheCallerCancels(t *testing.T) {
	srv, _ := fakeOrderService(t, http.StatusOK, `{"activeErrands":0}`)
	c := New(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.ActiveErrands(ctx, supplierID)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}
