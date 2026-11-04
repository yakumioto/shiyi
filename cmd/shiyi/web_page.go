//go:build serve_ready

package main

import (
	"net/http"

	"shiyi/internal/web"
)

// webPage returns the single page web interface.
//
// The page is served by the same process as the API, so that the deployment
// stays a single binary.
func webPage() http.Handler {
	return web.Handler()
}
