// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Builds order-service's HTTP router: the middleware chain, JSON 404,
//   405 and parameter errors around the generated routes (D-053, D-056).
// Author review: James Teo — reviewed and verified.

// Package httpapi is order-service's HTTP transport. It implements the
// generated gen.ServerInterface (api/openapi.yaml, D-053) on top of
// order.Service: handlers decode, call the service and map its errors to
// status codes, and every business rule stays in internal/order (root
// AGENTS.md §6).
package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"foc/order-service/internal/gen"
	"foc/order-service/internal/order"
)

// Pinger reports whether the database answers. A *pgxpool.Pool satisfies it.
type Pinger interface {
	Ping(ctx context.Context) error
}

// NewRouter returns order-service's HTTP handler: the routes of
// api/openapi.yaml served by svc, and Find errands' records (D-092) by
// activity, with db checked by GET /health, access tokens checked by tokens
// and revocations, and every request logged to log (a nil log falls back to
// slog.Default()).
//
// AI-generated (edited by PENDING): the activity parameter.
//
// Each request passes, in order: the request id (D-047), the access log
// (NFR7.1), panic recovery, the body limit and write deadline for POST and
// PATCH, and token authentication (D-056), which every route except GET
// /health requires.
func NewRouter(svc *order.Service, activity *order.Activity, db Pinger, tokens TokenVerifier, revocations RevocationChecker, log *slog.Logger) http.Handler {
	if log == nil {
		log = slog.Default()
	}
	r := chi.NewRouter()
	// The request id is outermost so every later log line carries it; the
	// access log wraps recovery so it records the 500 a panic becomes;
	// authentication is innermost so a request it turns away is still logged.
	r.Use(
		requestID,
		accessLog(log),
		recoverPanics(log),
		limitBody,
		writeDeadline,
		authenticate(tokens, revocations, log),
	)
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "no such route")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		// chi's own 405 lists the allowed methods; a custom handler has to
		// work them out again to keep that.
		w.Header().Set("Allow", allowedMethods(r, req))
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	})
	return gen.HandlerWithOptions(&server{svc: svc, activity: activity, db: db, log: log}, gen.ChiServerOptions{
		BaseRouter:       r,
		ErrorHandlerFunc: writeParamError,
	})
}

// allowedMethods lists the methods routes serves at r's path, for a 405's
// Allow header.
func allowedMethods(routes chi.Routes, r *http.Request) string {
	path := routingPath(r)
	var allowed []string
	for _, m := range []string{
		http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions,
	} {
		if routes.Match(chi.NewRouteContext(), m, path) {
			allowed = append(allowed, m)
		}
	}
	return strings.Join(allowed, ", ")
}

// writeParamError answers a path, query or header parameter the generated
// wrapper could not bind (a malformed UUID, a missing scope) with a 400.
func writeParamError(w http.ResponseWriter, _ *http.Request, err error) {
	writeError(w, http.StatusBadRequest, paramMessage(err))
}

// paramMessage words a binding error for the client without the Go type
// names the generated errors carry.
func paramMessage(err error) string {
	var (
		required *gen.RequiredParamError
		format   *gen.InvalidParamFormatError
		repeated *gen.TooManyValuesForParamError
	)
	switch {
	case errors.As(err, &required):
		return required.ParamName + " is required"
	case errors.As(err, &format):
		switch format.ParamName {
		case "errandId", "supplierId":
			return format.ParamName + " must be a UUID"
		}
		return format.ParamName + " is not valid"
	case errors.As(err, &repeated):
		return "send " + repeated.ParamName + " only once"
	default:
		return "the request's parameters are not valid"
	}
}
