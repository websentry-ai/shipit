package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var staticFS embed.FS

// Handler returns an http.Handler that serves the embedded web dashboard.
// It serves static files from the embedded dist directory and falls back
// to index.html for SPA routing.
func Handler() http.Handler {
	// Get the dist subdirectory
	distFS, err := fs.Sub(staticFS, "dist")
	if err != nil {
		panic(err)
	}

	fileServer := http.FileServer(http.FS(distFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if the request is for the API
		if strings.HasPrefix(r.URL.Path, "/api") {
			http.NotFound(w, r)
			return
		}

		// Try to serve the file
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		// Check if file exists
		f, err := distFS.Open(strings.TrimPrefix(path, "/"))
		if err != nil {
			// File doesn't exist, serve index.html for SPA routing
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
			return
		}
		f.Close()

		// Serve the file
		fileServer.ServeHTTP(w, r)
	})
}
