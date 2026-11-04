//go:build !config_ready

package main

import (
	"os"
	"strings"
)

// resolveToken reads the bearer token the service expects.
//
// The token comes from the environment variable the configuration names
// (auth_token_env, by default SHIYI_AUTH_TOKEN). An unset variable means the
// service does not authenticate and the client sends no Authorization header.
//
// This build reads the default variable only. The build that can read the
// configuration file lives in token_config.go.
//
// The token itself is never printed and never written anywhere.
func resolveToken() (string, error) {
	return strings.TrimSpace(os.Getenv(cliTokenEnv)), nil
}
