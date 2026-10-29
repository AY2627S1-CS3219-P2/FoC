// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: Tests for the user-service ranking-model client (D-099).
// Author review: PENDING — <reviewer to complete>

package userclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/auth"
	"foc/order-service/internal/ranking"
)

type seen struct {
	method, path, auth, contentType string
	body                            []byte
}

func server(t *testing.T, status int, body string) (*httptest.Server, *seen) {
	t.Helper()
	got := &seen{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path = r.Method, r.URL.Path
		got.auth, got.contentType = r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		got.body, _ = io.ReadAll(r.Body)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func TestLoadForwardsTheCouriersToken(t *testing.T) {
	courier := uuid.New()
	srv, got := server(t, http.StatusOK, `{"user_id":"`+courier.String()+`","model":{"version":1},"trained_at":"2026-10-29T10:00:00Z","updated_at":"2026-10-29T10:00:01Z"}`)
	ctx := auth.ContextWithToken(context.Background(), "courier-token")

	saved, err := New(srv.URL+"/", srv.Client()).Load(ctx, courier)

	require.NoError(t, err)
	assert.JSONEq(t, `{"version":1}`, string(saved.Model))
	assert.Equal(t, time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC), saved.TrainedAt)
	assert.Equal(t, http.MethodGet, got.method)
	assert.Equal(t, "/api/v1/users/"+courier.String()+"/ranking-model", got.path)
	assert.Equal(t, "Bearer courier-token", got.auth)
}

func TestLoadOfACourierWithNoModel(t *testing.T) {
	srv, _ := server(t, http.StatusNotFound, `{"error":"ranking model not found"}`)
	_, err := New(srv.URL, srv.Client()).Load(context.Background(), uuid.New())
	require.ErrorIs(t, err, ranking.ErrNoModel)
}

func TestSavePutsTheModel(t *testing.T) {
	courier := uuid.New()
	srv, got := server(t, http.StatusOK, `{}`)
	ctx := auth.ContextWithToken(context.Background(), "courier-token")
	trained := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)

	err := New(srv.URL, srv.Client()).Save(ctx, courier, ranking.Saved{Model: json.RawMessage(`{"version":1}`), TrainedAt: trained})

	require.NoError(t, err)
	assert.Equal(t, http.MethodPut, got.method)
	assert.Equal(t, "Bearer courier-token", got.auth)
	assert.Equal(t, "application/json", got.contentType)
	assert.JSONEq(t, `{"model":{"version":1},"trained_at":"2026-10-29T10:00:00Z"}`, string(got.body))
}

func TestFailuresAreErrors(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusRequestEntityTooLarge, http.StatusInternalServerError} {
		srv, _ := server(t, status, `{"error":"no"}`)
		c := New(srv.URL, srv.Client())
		_, err := c.Load(context.Background(), uuid.New())
		assert.Error(t, err, "load %d", status)
		assert.NotErrorIs(t, err, ranking.ErrNoModel)
		assert.Error(t, c.Save(context.Background(), uuid.New(), ranking.Saved{Model: json.RawMessage(`{}`)}), "save %d", status)
	}

	t.Run("an unreadable model", func(t *testing.T) {
		srv, _ := server(t, http.StatusOK, `not json`)
		_, err := New(srv.URL, srv.Client()).Load(context.Background(), uuid.New())
		assert.Error(t, err)
	})
	t.Run("no answer", func(t *testing.T) {
		srv, _ := server(t, http.StatusOK, `{}`)
		srv.Close()
		_, err := New(srv.URL, srv.Client()).Load(context.Background(), uuid.New())
		assert.Error(t, err)
	})
}
