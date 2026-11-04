//go:build serve_ready && !web_ready

package main

import "net/http"

// webPage returns no web interface.
//
// The web page lives in internal/web, which is written by its own ticket.
// Until it is in place the service offers the API alone.
func webPage() http.Handler {
	return nil
}
