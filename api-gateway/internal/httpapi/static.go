// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-22
// Scope: Serves the built frontend from the gateway, which is what makes the
//   browser same-origin with the API in Compose and in production (D-033).
// Author review: Nigeltzy - Checked the simple generated boilerplate code and it seems reasonable. I also checked the output of the generated code and it seems to be a typical implementation of a SPA handler.

package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// newSPAHandler serves the file at the request path from dir, and
// dir/index.html for any path that is not a file, so client-side routes load
// the app.
func newSPAHandler(dir string) http.Handler {
	index := filepath.Join(dir, "index.html")
	files := http.FileServer(http.Dir(dir))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// AI-generated (edited by Nigeltzy).
		// Reject paths that clean to outside dir before os.Stat sees them;
		// http.Dir also refuses to leave its root. IsLocal rejects ".." as a
		// path element but not a name that merely starts with "..".
		clean := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		if !filepath.IsLocal(clean) {
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
