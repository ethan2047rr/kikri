package gui

import (
	"io/fs"
	"testing"
)

func TestAssetsEmbedded(t *testing.T) {
	for _, name := range []string{"index.html", "style.css"} {
		if _, err := fs.Stat(Assets(), name); err != nil {
			t.Errorf("%s not embedded: %v", name, err)
		}
	}
	// app.js is a build product; warn rather than fail so `go test` works
	// before `make frontend` has run.
	if _, err := fs.Stat(Assets(), "app.js"); err != nil {
		t.Log("app.js missing: run `make frontend`")
	}
}
