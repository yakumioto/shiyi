//go:build !serve_ready

package main

import (
	"fmt"
	"log/slog"
	"net/http"

	"shiyi/internal/config"
)

// defaultServeDeps stands in for the real wiring.
//
// The service is assembled from the internal packages (config, llm, store,
// classify, search, answer and httpapi). Those packages are written by the
// other tickets, so the real wiring lives in serve_service.go behind the
// `serve_ready` build tag and is only compiled once they are in place.
//
// Until then the command fails with a message that says what is missing.
var defaultServeDeps = serveDeps{
	loadConfig: func() (config.Config, error) {
		return config.Config{}, fmt.Errorf("serve 尚未实现：服务需要 internal/httpapi 等内部包")
	},
	build: func(config.Config, *slog.Logger) (http.Handler, error) {
		return nil, fmt.Errorf("serve 尚未实现：服务需要 internal/httpapi 等内部包")
	},
}
