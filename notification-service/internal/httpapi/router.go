// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
// Scope: Builds the HTTP router around the generated routes and implements them
//   (api/openapi.yaml, D-144). Handlers do transport only (root AGENTS.md §6).
// Author review: PENDING — <reviewer to complete>

// Package httpapi is notification-service's HTTP transport. It implements the
// generated gen.ServerInterface on top of notification.Service.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"foc/notification-service/internal/gen"
	"foc/notification-service/internal/notification"
)

// Messages the transport sends.
const (
	msgInternal        = "internal error"
	msgUnauthenticated = "missing, invalid or expired access token"
	msgAuthUnavailable = "sign-in cannot be checked right now; please try again"
	msgUnavailable     = "notifications are unavailable right now; please try again"
	msgNotFound        = "no such notification"
	msgBadParameter    = "a request parameter is not valid"
)

// healthTimeout bounds the database ping in GET /health.
const healthTimeout = 2 * time.Second

// Pinger reports whether the database answers. A *pgxpool.Pool satisfies it.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Service is what the handlers call. *notification.Service satisfies it.
type Service interface {
	List(ctx context.Context, caller openapi_types.UUID, limit, offset int) (notification.Page, error)
	MarkRead(ctx context.Context, caller, id openapi_types.UUID) error
}

type server struct {
	svc Service
	db  Pinger
	log *slog.Logger
}

// NewRouter returns notification-service's HTTP handler.
func NewRouter(svc Service, db Pinger, tokens TokenVerifier, revocations RevocationChecker, log *slog.Logger) http.Handler {
	r := chi.NewRouter()
	r.Use(accessLog(log), recoverPanics(log), authenticate(tokens, revocations, log))
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "no such route")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	})
	return gen.HandlerWithOptions(&server{svc: svc, db: db, log: log}, gen.ChiServerOptions{
		BaseRouter: r,
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, _ error) {
			writeError(w, http.StatusBadRequest, msgBadParameter)
		},
	})
}

// GetHealth answers 200 while the database does.
func (s *server) GetHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), healthTimeout)
	defer cancel()
	if err := s.db.Ping(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database not reachable")
		return
	}
	writeJSON(w, http.StatusOK, gen.Health{Status: "ok"})
}

// ListNotifications returns a page of the caller's notifications.
func (s *server) ListNotifications(w http.ResponseWriter, r *http.Request, params gen.ListNotificationsParams) {
	caller, ok := callerOf(w, r)
	if !ok {
		return
	}
	limit, offset := notification.DefaultLimit, 0
	if params.Limit != nil {
		limit = *params.Limit
	}
	if params.Offset != nil {
		offset = *params.Offset
	}
	page, err := s.svc.List(r.Context(), caller, limit, offset)
	switch {
	case errors.Is(err, notification.ErrInvalidPage):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		s.log.ErrorContext(r.Context(), "list notifications", "err", err)
		writeError(w, http.StatusServiceUnavailable, msgUnavailable)
		return
	}
	out := gen.NotificationPage{Notifications: make([]gen.Notification, 0, len(page.Notifications)), Total: page.Total}
	for _, n := range page.Notifications {
		out.Notifications = append(out.Notifications, gen.Notification{
			Id:          n.ID,
			ErrandId:    n.ErrandID,
			StateChange: n.StateChange,
			Message:     n.Message,
			Read:        n.Read,
			CreatedAt:   n.CreatedAt.UTC(),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// MarkNotificationRead marks one of the caller's notifications as read.
func (s *server) MarkNotificationRead(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	caller, ok := callerOf(w, r)
	if !ok {
		return
	}
	err := s.svc.MarkRead(r.Context(), caller, id)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, notification.ErrNotFound):
		writeError(w, http.StatusNotFound, msgNotFound)
	default:
		s.log.ErrorContext(r.Context(), "mark notification read", "err", err)
		writeError(w, http.StatusServiceUnavailable, msgUnavailable)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		status, body = http.StatusInternalServerError, []byte(`{"error":"`+msgInternal+`"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, gen.Error{Error: msg})
}
