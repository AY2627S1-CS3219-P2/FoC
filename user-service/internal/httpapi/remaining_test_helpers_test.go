// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-28
// Scope: Retained temporary parent-package router and token-verifier helpers while endpoint suites are relocated incrementally.
// Author review: PENDING

package httpapi

import (
	"context"
	"net/http"

	"foc/user-service/internal/httpapi/handlers"
	"foc/user-service/internal/httpapi/router"
	"foc/user-service/internal/httpapi/routes"
)

type fakeTokenVerifier struct {
	principal handlers.Principal
	err       error
	token     string
}

func (f *fakeTokenVerifier) Verify(_ context.Context, rawToken string) (handlers.Principal, error) {
	f.token = rawToken
	return f.principal, f.err
}

func newTestRouter(deps routes.Dependencies) http.Handler {
	return router.Setup(deps)
}
