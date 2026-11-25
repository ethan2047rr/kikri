// Package gui embeds the popup's static frontend.
//
// index.html and style.css are hand-written; app.js is compiled from
// frontend/src by `make frontend` and is not committed.
package gui

import (
	"embed"
	"io/fs"
)

//go:embed all:assets
var assets embed.FS

// Assets returns the frontend files rooted at the assets directory.
func Assets() fs.FS {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err) // impossible: the directory is embedded at build time
	}
	return sub
}
