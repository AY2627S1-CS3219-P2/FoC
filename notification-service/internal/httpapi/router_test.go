// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
// Scope: Unit tests of the HTTP surface: authentication, status codes, paging
//   defaults and the response body (api/openapi.yaml, D-144).
// Author review: nigeltzy - Checked that the unit test implementation seems correct and as expected according to requirements.

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/notification-service/internal/auth"
	"foc/notification-service/internal/gen"
	"foc/notification-service/internal/notification"
)

var caller = uuid.New()

type stubTokens struct{ err error }

func (s stubTokens) Verify(context.Context, string) (auth.Claims, error) {
	return auth.Claims{UserID: caller, Role: "STUDENT"}, s.err
}

type stubRevocations struct {
	revoked bool
	err     error
}

func (s stubRevocations) Revoked(context.Context, auth.Claims) (bool, error) {
	return s.revoked, s.err
}

type stubService struct {
	page                notification.Page
	listErr, readErr    error
	gotCaller, gotID    uuid.UUID
	gotLimit, gotOffset int
}

func (s *stubService) List(_ context.Context, c uuid.UUID, limit, offset int) (notification.Page, error) {
	s.gotCaller, s.gotLimit, s.gotOffset = c, limit, offset
	return s.page, s.listErr
}

func (s *stubService) MarkRead(_ context.Context, c, id uuid.UUID) error {
	s.gotCaller, s.gotID = c, id
	return s.readErr
}

type stubDB struct{ err error }

func (s stubDB) Ping(context.Context) error { return s.err }

type harness struct {
	svc    *stubService
	tokens stubTokens
	rev    stubRevocations
	db     stubDB
}

func (h harness) do(t *testing.T, method, path string, bearer bool) *httptest.ResponseRecorder {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := NewRouter(h.svc, h.db, h.tokens, h.rev, log)
	req := httptest.NewRequest(method, path, nil)
	if bearer {
		req.Header.Set("Authorization", "Bearer a-token")
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestAuthentication(t *testing.T) {
	for _, tc := range []struct {
		name   string
		h      harness
		bearer bool
		want   int
	}{
		{"no token", harness{}, false, http.StatusUnauthorized},
		{"a bad token", harness{tokens: stubTokens{err: auth.ErrInvalidSignature}}, true, http.StatusUnauthorized},
		{"a revoked token", harness{rev: stubRevocations{revoked: true}}, true, http.StatusUnauthorized},
		{"keys unavailable", harness{tokens: stubTokens{err: auth.ErrKeysUnavailable}}, true, http.StatusServiceUnavailable},
		{"Redis answers with an error", harness{rev: stubRevocations{err: errors.New("NOAUTH")}}, true, http.StatusServiceUnavailable},
		{"Redis unreachable skips the check", harness{rev: stubRevocations{err: fmt.Errorf("x: %w", auth.ErrRedisUnreachable)}}, true, http.StatusOK},
		{"a good token", harness{}, true, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.h.svc = &stubService{}
			assert.Equal(t, tc.want, tc.h.do(t, http.MethodGet, "/notifications", tc.bearer).Code)
		})
	}
}

func TestIdentityHeadersAreIgnored(t *testing.T) {
	svc := &stubService{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := NewRouter(svc, stubDB{}, stubTokens{}, stubRevocations{}, log)
	req := httptest.NewRequest(http.MethodGet, "/notifications", nil)
	req.Header.Set("Authorization", "Bearer a-token")
	req.Header.Set("X-User-Id", uuid.NewString())
	router.ServeHTTP(httptest.NewRecorder(), req)
	assert.Equal(t, caller, svc.gotCaller, "the caller is the token's subject")
}

func TestHealthNeedsNoToken(t *testing.T) {
	h := harness{svc: &stubService{}}
	assert.Equal(t, http.StatusOK, h.do(t, http.MethodGet, "/health", false).Code)
	h.db.err = errors.New("down")
	assert.Equal(t, http.StatusServiceUnavailable, h.do(t, http.MethodGet, "/health", false).Code)
}

func TestListResponse(t *testing.T) {
	id, errand := uuid.New(), uuid.New()
	created := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	h := harness{svc: &stubService{page: notification.Page{Total: 9, Notifications: []notification.Notification{{
		ID: id, ErrandID: errand, StateChange: notification.EventAccepted,
		Message: "A courier accepted your errand.", CreatedAt: created,
	}}}}}
	rec := h.do(t, http.MethodGet, "/notifications", true)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, notification.DefaultLimit, h.svc.gotLimit)
	assert.Zero(t, h.svc.gotOffset)

	var page gen.NotificationPage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))
	assert.Equal(t, 9, page.Total)
	require.Len(t, page.Notifications, 1)
	got := page.Notifications[0]
	assert.Equal(t, id, got.Id)
	assert.Equal(t, errand, got.ErrandId)
	assert.Equal(t, notification.EventAccepted, got.StateChange)
	assert.Equal(t, "A courier accepted your errand.", got.Message)
	assert.False(t, got.Read)
	assert.True(t, created.Equal(got.CreatedAt))
}

func TestAnEmptyListIsAnArrayNotNull(t *testing.T) {
	h := harness{svc: &stubService{}}
	rec := h.do(t, http.MethodGet, "/notifications", true)
	assert.JSONEq(t, `{"notifications":[],"total":0}`, rec.Body.String())
}

func TestListPaging(t *testing.T) {
	h := harness{svc: &stubService{}}
	require.Equal(t, http.StatusOK, h.do(t, http.MethodGet, "/notifications?limit=10&offset=20", true).Code)
	assert.Equal(t, 10, h.svc.gotLimit)
	assert.Equal(t, 20, h.svc.gotOffset)

	assert.Equal(t, http.StatusBadRequest, h.do(t, http.MethodGet, "/notifications?limit=abc", true).Code)

	h.svc.listErr = fmt.Errorf("%w: limit", notification.ErrInvalidPage)
	assert.Equal(t, http.StatusBadRequest, h.do(t, http.MethodGet, "/notifications?limit=999", true).Code)

	h.svc.listErr = errors.New("database down")
	assert.Equal(t, http.StatusServiceUnavailable, h.do(t, http.MethodGet, "/notifications", true).Code)
}

func TestMarkRead(t *testing.T) {
	id := uuid.New()
	path := "/notifications/" + id.String() + "/read"

	h := harness{svc: &stubService{}}
	rec := h.do(t, http.MethodPost, path, true)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())
	assert.Equal(t, caller, h.svc.gotCaller)
	assert.Equal(t, id, h.svc.gotID)

	assert.Equal(t, http.StatusBadRequest, h.do(t, http.MethodPost, "/notifications/not-a-uuid/read", true).Code)

	h.svc.readErr = fmt.Errorf("x: %w", notification.ErrNotFound)
	assert.Equal(t, http.StatusNotFound, h.do(t, http.MethodPost, path, true).Code)

	h.svc.readErr = errors.New("database down")
	assert.Equal(t, http.StatusServiceUnavailable, h.do(t, http.MethodPost, path, true).Code)
}

func TestUnknownRoutesAnswerJSON(t *testing.T) {
	h := harness{svc: &stubService{}}
	assert.Equal(t, http.StatusNotFound, h.do(t, http.MethodGet, "/nope", true).Code)
	assert.Equal(t, http.StatusMethodNotAllowed, h.do(t, http.MethodDelete, "/notifications", true).Code)
}
