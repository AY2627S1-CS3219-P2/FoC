// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-21
// Scope: Added shared test helper that exercises the dedicated router package.
// Author review: Validated tests reflects intended behaviour

package handlers_test

import (
	"context"
	"net/http"

	"foc/user-service/internal/httpapi/handlers"
	"foc/user-service/internal/httpapi/router"
	"foc/user-service/internal/httpapi/routes"
)

type allowRevocationChecker struct{}

func (allowRevocationChecker) Check(context.Context, handlers.Principal) (bool, error) {
	return false, nil
}

func newTestRouter(deps routes.Dependencies) http.Handler {
	if deps.RevocationChecker == nil {
		deps.RevocationChecker = allowRevocationChecker{}
	}
	return router.Setup(deps)
}
