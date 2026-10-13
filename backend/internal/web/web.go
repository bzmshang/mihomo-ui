// Package web serves the built panel from inside the binary, so a release is
// one file and there is no static directory to point at or get wrong.
//
// The panel is zashboard (MIT), whose built output is fetched by
// scripts/fetch-zashboard.* into internal/web/dist. It is a client for a Clash
// API, so this package also injects a small bootstrap script that points it at
// this panel's own same-origin proxy and gives it a session token instead of
// the kernel secret.
package web

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// dist is populated by scripts/fetch-zashboard.*; the placeholder keeps
// `go build` working in a checkout where it has not been fetched yet.
//
//go:embed all:dist
var dist embed.FS

//go:embed panel-bootstrap.js
var panelBootstrap []byte

const bootstrapTag = `<script src="/panel-bootstrap.js"></script>`

// Handler serves the SPA, falling back to index.html so client-side routes
// survive a reload.
func Handler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic("embedded panel is missing: " + err.Error())
	}
	// The dist directory ships with a placeholder so `go build` works in a fresh
	// checkout. Without this check that placeholder builds a binary that serves a
	// directory listing instead of the panel, and nothing says why.
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		panic("no panel was embedded: run scripts/fetch-zashboard.sh (or .ps1) before go build")
	}
	index = injectBootstrap(index)
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/"):
			http.NotFound(w, r)
		case r.URL.Path == "/panel-bootstrap.js":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			_, _ = w.Write(panelBootstrap)
		case r.URL.Path == "/" || r.URL.Path == "/index.html":
			// Always the injected copy, never the embedded one, so the bootstrap
			// cannot be lost to a cache or a direct hit on /index.html.
			serveIndex(w, index)
		default:
			if _, err := sub.Open(strings.TrimPrefix(r.URL.Path, "/")); err != nil {
				serveIndex(w, index)
				return
			}
			files.ServeHTTP(w, r)
		}
	})
}

// injectBootstrap puts the bootstrap script ahead of the panel's own module.
// zashboard is loaded with a deferred <script type="module"> in <head>, which
// runs after the document is parsed, so a classic script appended here still
// executes first — and it must, because zashboard reads its backend list as
// soon as its module evaluates.
func injectBootstrap(index []byte) []byte {
	if bytes.Contains(index, []byte(bootstrapTag)) {
		return index
	}
	if head := bytes.Index(index, []byte("</head>")); head >= 0 {
		out := make([]byte, 0, len(index)+len(bootstrapTag))
		out = append(out, index[:head]...)
		out = append(out, bootstrapTag...)
		return append(out, index[head:]...)
	}
	return append(append([]byte{}, index...), bootstrapTag...)
}

func serveIndex(w http.ResponseWriter, index []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(index)
}
