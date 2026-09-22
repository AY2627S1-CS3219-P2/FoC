// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-22
// Scope: Serves the built frontend from the gateway, which is what makes the
//   browser same-origin with the API in Compose and in production (D-033).
// Author review: PENDING — <reviewer to complete>

package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// newSPAHandler serves the built frontend out of dir.
//
// D-033 puts the refresh token in a SameSite=Strict cookie, which means the
// page and the API must be one origin. In development Vite's server.proxy
// does that; here the gateway does it by serving the bundle itself, so there
// is no second origin and no CORS anywhere.
//
// Unknown paths fall back to index.html rather than 404ing, because the
// router is client-side: /suppliers is a React route, not a file. Only paths
// that do NOT belong to the API reach this handler at all, so a mistyped
// /api/... still gets the API's own 404 rather than a page.
func newSPAHandler(dir string) http.Handler {
	index := filepath.Join(dir, "index.html")
	files := http.FileServer(http.Dir(dir))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reject traversal before touching the filesystem. http.Dir already
		// refuses to escape its root, but failing here keeps the intent
		// visible to whoever reads this next.
		clean := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		if strings.HasPrefix(clean, "..") {
			http.NotFound(w, r)
			return
		}

		if info, err := os.Stat(filepath.Join(dir, clean)); err == nil && !info.IsDir() {
			files.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, index)
	})
}
