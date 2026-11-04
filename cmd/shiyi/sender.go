package main

import (
	"net/http"
	"os"
	"time"
)

// cliTokenEnv is the environment variable the CLI reads its bearer token
// from. It matches the default of auth_token_env in the documented
// configuration.
const cliTokenEnv = "SHIYI_AUTH_TOKEN"

// requestTimeout bounds a single request to the service.
const requestTimeout = 60 * time.Second

// newSender builds the client that talks to the Shiyi service.
//
// The client keeps no state between calls: there is no local cache, no
// history and no retry.
func newSender(opts Options) (sender, error) {
	return &Client{
		BaseURL:    opts.Server,
		HTTPClient: &http.Client{Timeout: requestTimeout},
		Token:      os.Getenv(cliTokenEnv),
	}, nil
}
