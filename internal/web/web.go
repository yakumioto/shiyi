// Package web serves Shiyi's minimal browser interface.
//
// The package owns one self-contained page: an input box, a send button and
// the list of inputs and answers displayed in this page's memory. Static assets
// are embedded into the binary: no CDN or frontend build step is needed.
//
// The package deliberately depends on no other internal package. The HTTP
// layer mounts it with httpapi.Server.HandlerWithWeb, and the page talks to
// the same-origin /auth/ endpoints and POST /messages directly.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strconv"
)

//go:embed static
var files embed.FS

// assets is the embedded static directory without its "static/" prefix.
var assets = mustSub(files, "static")

// Handler returns the single-page interface.
//
// The handler serves the page at "/" (and "/index.html") together with its
// two assets at "/app.js" and "/style.css". Every other path is a 404 and
// every method other than GET is a 405.
//
// The returned handler holds no state: it never reads or writes browser
// storage, never caches private inputs or answers and never calls the API itself.
// The page consumes the authentication API and sends independent POST /messages.
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", serveIndex)
	mux.HandleFunc("GET /index.html", serveIndex)
	mux.HandleFunc("GET /app.js", func(w http.ResponseWriter, r *http.Request) {
		serveAsset(w, "app.js", "text/javascript; charset=utf-8")
	})
	mux.HandleFunc("GET /style.css", func(w http.ResponseWriter, r *http.Request) {
		serveAsset(w, "style.css", "text/css; charset=utf-8")
	})
	return mux
}

// serveIndex writes the single page. The pattern "GET /" also matches every
// deeper path, so anything that is not the page itself is a 404.
func serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		http.NotFound(w, r)
		return
	}
	serveAsset(w, "index.html", "text/html; charset=utf-8")
}

// serveAsset writes one embedded file with an explicit content type. The type
// is never sniffed: the page must load exactly the resources it declares.
func serveAsset(w http.ResponseWriter, name, contentType string) {
	data, err := fs.ReadFile(assets, name)
	if err != nil {
		// The embedded set is fixed at build time, so this cannot happen
		// at runtime; report it as a missing resource rather than panicking
		// inside an HTTP handler.
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// mustSub strips a directory prefix from an embedded file system.
func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic("web: 嵌入静态资源失败: " + err.Error())
	}
	return sub
}
