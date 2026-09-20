// Package ui serves the built interface from inside the engine binary.
//
// This is what makes ClusterTrail a single download: no Node, no npm, no installer
// and nothing to sign. You get one file, you run it, and your browser opens.
// The desktop shell, when it arrives, will point a native window at the same
// server rather than replacing it.
package ui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// dist is filled at build time by `make bundle`, which copies the Vite output
// here. The placeholder below keeps `go build` working in a fresh checkout.
//
//go:embed all:dist
var dist embed.FS

// Built reports whether a real interface was bundled into this binary.
func Built() bool {
	body, err := dist.ReadFile("dist/index.html")
	return err == nil && !strings.Contains(string(body), "placeholder-build")
}

// Handler serves the interface, falling back to index.html for any path the
// bundle does not contain, because the UI routes in the browser.
func Handler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return http.NotFoundHandler()
	}
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(r.URL.Path, "/")
		if clean == "" {
			clean = "index.html"
		}
		if _, err := fs.Stat(sub, clean); err != nil {
			// Not a file we shipped: hand the browser the app and let it route.
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		// Hashed assets never change; the entry document always may.
		if strings.HasPrefix(clean, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-store")
		}
		files.ServeHTTP(w, r)
	})
}
