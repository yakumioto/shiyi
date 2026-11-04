package main

import (
	"os"
	"strings"

	"shiyi/internal/config"
)

// resolveToken reads the bearer token the CLI sends to the service.
//
// The variable it reads comes from the configuration's auth_token_env. When
// the configuration cannot be read, or does not name a variable, the default
// SHIYI_AUTH_TOKEN is used. An unset or empty variable means the service does
// not authenticate and the client sends no Authorization header.
//
// The token itself is never printed and never written anywhere.
func resolveToken() (string, error) {
	name := cliTokenEnv

	if cfg, err := config.Load(); err == nil {
		if configured := strings.TrimSpace(cfg.AuthTokenEnv); configured != "" {
			name = configured
		}
	}

	return strings.TrimSpace(os.Getenv(name)), nil
}
