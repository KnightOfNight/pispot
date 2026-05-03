package web

import (
	"bytes"
	"io/fs"
	"net/http"
	"strings"
)

// TitleHandler wraps the embedded filesystem handler and substitutes
// {{DASHBOARD_TITLE}} in HTML responses with the configured title.
// Non-HTML files are served verbatim for efficiency.
func TitleHandler(h http.Handler, title string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only intercept .html files (and bare "/" which serves index.html).
		path := r.URL.Path
		if path == "/" || strings.HasSuffix(path, ".html") {
			// Read the file from the embedded FS, substitute, and serve.
			name := strings.TrimPrefix(path, "/")
			if name == "" {
				name = "index.html"
			}
			data, err := fs.ReadFile(FS(), name)
			if err != nil {
				// Let the underlying handler return a 404.
				h.ServeHTTP(w, r)
				return
			}
			out := bytes.ReplaceAll(data, []byte("{{DASHBOARD_TITLE}}"), []byte(title))
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache, must-revalidate")
			_, _ = w.Write(out)
			return
		}
		h.ServeHTTP(w, r)
	})
}
