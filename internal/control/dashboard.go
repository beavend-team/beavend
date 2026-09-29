package control

import (
	"errors"
	"io/fs"
	"net/http"
	"strings"
)

// DashboardHandler serves the embedded SPA dashboard files with client-side SPA routing fallback.
func DashboardHandler(staticFS fs.FS) http.Handler {
	if staticFS == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<!DOCTYPE html><html><body style="font-family:sans-serif;padding:2rem;background:#020617;color:#f8fafc"><h1>Beavend Dashboard</h1><p>Static dashboard build not embedded in this development build.</p></body></html>`))
		})
	}

	fileServer := http.StripPrefix("/dashboard", http.FileServer(http.FS(staticFS)))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/dashboard")
		path = strings.TrimPrefix(path, "/")
		if path == "" {
			path = "index.html"
		}

		// Check if the requested file exists in the embedded filesystem
		f, err := staticFS.Open(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				// Fall back to index.html for client-side routing (e.g. /dashboard/apps, /dashboard/prompts)
				http.ServeFileFS(w, r, staticFS, "index.html")
				return
			}
			http.NotFound(w, r)
			return
		}
		_ = f.Close()

		fileServer.ServeHTTP(w, r)
	})
}
