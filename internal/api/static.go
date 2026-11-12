package api

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

const noUI = `<!doctype html><meta charset="utf-8"><title>Yubeos</title>
<body style="font:16px system-ui;max-width:40rem;margin:4rem auto;padding:0 1rem">
<h1>Yubeos web UI not built</h1>
<p>The API is running, but this binary has no embedded UI. Run <code>make web</code> and rebuild, or use <code>make demo</code>.</p>`

// spa serves the embedded single-page app: real files as-is, everything else falls back to index.html
// (client-side routing). Hashed build assets are immutable; the shell is never cached.
func spa(ui fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if ui == nil {
			serveNoUI(w)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if st, err := fs.Stat(ui, name); err == nil && !st.IsDir() {
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			http.ServeFileFS(w, r, ui, name)
			return
		}
		if _, err := fs.Stat(ui, "index.html"); err != nil {
			serveNoUI(w)
			return
		}
		// A path with an extension that does not exist is a real 404 (missing asset), not an app route.
		if path.Ext(name) != "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, ui, "index.html")
	})
}

func serveNoUI(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(noUI))
}
