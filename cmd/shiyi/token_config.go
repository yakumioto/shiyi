//go:build config_ready

package main

import (
	"os"
	"strings"

	"shiyi/internal/config"
)

// resolveToken reads the bearer token the service expects.
//
// The token comes from the environment variable the configuration names
// (auth_token_env, by default SHIYI_AUTH_TOKEN). When the configuration
// cannot be read, the default variable is used. An unset variable means the
// service does not authenticate and the client sends no Authorization header.
//
// The token itself is never printed and never written anywhere.
func resolveToken() (string, error) {
	cfg, err := config.Load()
	if err == nil {
		if token := cfg.AuthToken(); token != "" {
			return strings.TrimSpace(token), nil
		}
		// The configuration names a token variable that holds nothing:
		// the service does not authenticate.
		return "", nil
	}

	return strings.TrimSpace(os.Getenv(cliTokenEnv)), nil
}
