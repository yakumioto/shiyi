//go:build !serve_ready

package main

import (
	"context"
	"fmt"
	"io"
)

// runServe starts the local Shiyi service.
//
// The real implementation lives in serve_service.go and is guarded by the
// `serve_ready` build tag. The tag is off while the internal packages the
// service is assembled from (config, llm, store, classify, search, answer
// and httpapi) are still being written by the other tickets; turning it on
// makes the CLI depend on all of them at once.
//
// This placeholder keeps the branch buildable and tells the user what to do.
func runServe(ctx context.Context, opts Options, stdout, stderr io.Writer) int {
	fmt.Fprintf(stderr, "shiyi: serve 尚未实现：服务需要 internal/httpapi 等内部包\n")
	return exitFailure
}
