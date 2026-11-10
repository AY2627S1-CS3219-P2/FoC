// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: The admin dashboard's two read endpoints (D-110, D-121 to D-123): the
//   account search and list, and the admin audit log. Both are admin-only.
// Author review: PENDING — <reviewer to complete>

package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"foc/user-service/internal/user"
)

// AccountSearcher lists accounts for an admin.
type AccountSearcher interface {
	SearchUsers(context.Context, user.UserFilter) (*user.UserPage, error)
}

// AuditReader reads the admin audit log.
type AuditReader interface {
	AuditLog(context.Context, user.AuditFilter) (*user.AuditPage, error)
}

// AdminDependencies contains only the operations AdminHandler needs.
type AdminDependencies struct {
	// AI Assistance Disclosure: Codex (GPT-6), 2026-11-09 — approved role
	// updater dependency. Author review: ZI YANG - reviewed and verified.
	RoleUpdater RoleUpdater
	Searcher    AccountSearcher
	Audit       AuditReader
	Logger      *slog.Logger
}

// AdminHandler serves the admin-only account list and audit log.
type AdminHandler struct{ deps AdminDependencies }

// NewAdminHandler constructs an admin handler with its required operations.
func NewAdminHandler(deps AdminDependencies) AdminHandler { return AdminHandler{deps: deps} }

// requireAdmin writes the answer and reports false unless the verified caller is
// an ADMIN. The caller is the token's principal, never a header (D-041).
func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	p, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return false
	}
	if p.Role != user.AccountRoleAdmin {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin access required"})
		return false
	}
	return true
}

// ListUsers handles GET /users, admin-only: accounts whose username or email
// contains q (ignoring case), optionally one status, sorted by username, paged
// (D-121 to D-123). It answers 50 a page at most (D-104).
func (h AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	q := r.URL.Query()
	f := user.UserFilter{Query: q.Get("q"), Status: user.AccountStatus(q.Get("status"))}
	var err error
	if f.Limit, err = intQuery(q.Get("limit")); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit must be a whole number"})
		return
	}
	if f.Offset, err = intQuery(q.Get("offset")); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "offset must be a whole number"})
		return
	}
	if h.deps.Searcher == nil {
		writeError(r.Context(), w, h.deps.Logger, http.StatusInternalServerError, errors.New("account searcher is required"), "account list unavailable")
		return
	}
	page, err := h.deps.Searcher.SearchUsers(r.Context(), f)
	if err != nil {
		if errors.Is(err, user.ErrInvalidFilter) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeError(r.Context(), w, h.deps.Logger, http.StatusInternalServerError, err, "account list unavailable")
		return
	}
	out := UserPageResponse{Items: make([]UserListItem, 0, len(page.Items)), Total: page.Total, Limit: page.Limit, Offset: page.Offset}
	for _, u := range page.Items {
		out.Items = append(out.Items, UserListItem{
			UID: u.UID.String(), Username: u.Username, Email: u.Email, PhoneNum: u.PhoneNum,
			AccountRole: string(u.AccountRole), AccountStatus: string(u.AccountStatus),
			DateCreated: u.DateCreated, LastLoginDate: u.LastLoginDate,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// AuditLog handles GET /users/audit-log, admin-only (D-110): one page of the
// admin audit log, newest first. Filters: action, outcome, actor_id, target_id,
// from and to (RFC 3339 or YYYY-MM-DD, "to" exclusive); limit (at most 50) and
// offset page it.
func (h AdminHandler) AuditLog(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	q := r.URL.Query()
	f := user.AuditFilter{Action: q.Get("action"), Outcome: q.Get("outcome"), ActorID: q.Get("actor_id"), TargetID: q.Get("target_id")}
	var err error
	if f.Limit, err = intQuery(q.Get("limit")); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit must be a whole number"})
		return
	}
	if f.Offset, err = intQuery(q.Get("offset")); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "offset must be a whole number"})
		return
	}
	if f.From, err = timeQuery(q.Get("from")); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "from must be an RFC 3339 time or YYYY-MM-DD"})
		return
	}
	if f.To, err = timeQuery(q.Get("to")); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "to must be an RFC 3339 time or YYYY-MM-DD"})
		return
	}
	if h.deps.Audit == nil {
		writeError(r.Context(), w, h.deps.Logger, http.StatusInternalServerError, errors.New("audit reader is required"), "audit log unavailable")
		return
	}
	page, err := h.deps.Audit.AuditLog(r.Context(), f)
	if err != nil {
		if errors.Is(err, user.ErrInvalidFilter) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeError(r.Context(), w, h.deps.Logger, http.StatusInternalServerError, err, "audit log unavailable")
		return
	}
	out := AuditPageResponse{Items: make([]AuditEntryResponse, 0, len(page.Items)), Total: page.Total, Limit: page.Limit, Offset: page.Offset}
	for _, e := range page.Items {
		out.Items = append(out.Items, AuditEntryResponse{
			ID: e.ID, OccurredAt: e.OccurredAt, ActorID: e.ActorID, ActorRole: e.ActorRole, Action: e.Action,
			TargetType: e.TargetType, TargetID: e.TargetID, Outcome: e.Outcome, RefusalReason: e.RefusalReason,
			Reason: e.Reason, BeforeState: e.BeforeState, AfterState: e.AfterState,
			AdditionalComments: e.AdditionalComments, CorrelationID: e.CorrelationID,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// intQuery parses an optional whole-number query value; empty is 0, which the
// service reads as "the default".
func intQuery(v string) (int, error) {
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("not a whole number: %w", err)
	}
	return n, nil
}

// timeQuery parses an optional time: RFC 3339, or a bare date read as midnight
// UTC. Empty is nil.
func timeQuery(v string) (*time.Time, error) {
	if v == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, v); err == nil {
			t = t.UTC()
			return &t, nil
		}
	}
	return nil, fmt.Errorf("not a time: %q", v)
}
