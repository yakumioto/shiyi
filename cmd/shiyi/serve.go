package main

import (
	"context"
	"fmt"
	"io"
)

// runServe starts the local Shiyi service.
//
// The service itself is assembled from the internal packages (config, llm,
// store, classify, search, answer and httpapi). Those packages are written by
// other tickets, so the wiring lives behind its own build tag and is not
// compiled into the binary until those packages are in place. See
// serve_service.go for the real implementation.
func runServe(ctx context.Context, opts Options, stdout, stderr io.Writer) int {
	fmt.Fprintf(stderr, "shiyi: serve 尚未实现\n")
	return exitFailure
}
