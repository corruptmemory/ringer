// Package hud serves the Ringside dashboard (templ + htmx) on
// 127.0.0.1:8700. The live panels poll /hud/* fragment routes rendered
// straight from the Go run-state; there is no JSON API and no client-side
// schema adaptation.
package hud

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var staticEmbed embed.FS

// staticFS is the embedded static tree rooted at "static/".
var staticFS = mustSub(staticEmbed, "static")

func mustSub(f embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(f, dir)
	if err != nil {
		panic(err) // embed path is a compile-time constant; failure is a build bug
	}
	return sub
}

// staticHandler serves the embedded static assets under /static/.
func staticHandler() http.Handler {
	return http.StripPrefix("/static/", http.FileServer(http.FS(staticFS)))
}
