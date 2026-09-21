// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-21
// Scope: Added shared test helper that exercises the dedicated router package.
// Author review: PENDING — reviewer to complete

package httpapi

import (
	"net/http"

	"foc/user-service/internal/httpapi/router"
	"foc/user-service/internal/httpapi/routes"
)

func newTestRouter(deps routes.Dependencies) http.Handler {
	return router.Setup(deps)
}
