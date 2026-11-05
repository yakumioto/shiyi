package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shiyi/internal/config"
)

func TestAuthConfigurationFailsClosedAndRequiresExplicitOrigin(t *testing.T) {
	for _, tc := range []struct {
		name string
		auth config.AuthConfig
	}{
		{"missing", config.AuthConfig{}},
		{"whitespace hash", config.AuthConfig{PasswordHash: " \t", StateDir: "/tmp/auth", PublicOrigin: "https://memory.example"}},
		{"missing origin", config.AuthConfig{DevAnonymous: true}},
		{"HTTP in normal mode", config.AuthConfig{PasswordHash: "test-hash", StateDir: "/tmp/auth", PublicOrigin: "http://memory.example"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig(t)
			cfg.Auth = tc.auth
			if err := cfg.Validate(); err == nil {
				t.Fatal("unsafe authentication configuration accepted")
			}
		})
	}
	for _, origin := range []string{"https://memory.example/", "https://u:p@memory.example", "https://memory.example?q=s", "https://memory.example#fragment", "null", "https://memory.example:0", "https://memory.example:65536", "https://memory.example https://other.example"} {
		t.Run(origin, func(t *testing.T) {
			cfg := validConfig(t)
			cfg.Auth = config.AuthConfig{DevAnonymous: true, PublicOrigin: origin}
			if err := cfg.Validate(); err == nil {
				t.Fatal("invalid origin accepted")
			}
		})
	}
	cfg := validConfig(t)
	cfg.Auth = config.AuthConfig{DevAnonymous: true, PublicOrigin: "http://memory.example"}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAuthEnvironmentOverridesEvenWhenEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := writeFile(t, `[auth]
password_hash = "file-hash-marker"
state_dir = "~/state"
public_origin = "https://memory.example"
dev_anonymous = true
`)
	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.StateDir != filepath.Join(home, "state") || cfg.Auth.PasswordHash != "file-hash-marker" || !cfg.Auth.DevAnonymous {
		t.Fatalf("auth fields not loaded/expanded: %+v", cfg.Auth)
	}
	t.Setenv(config.EnvAuthPasswordHash, "")
	t.Setenv(config.EnvAuthPublicOrigin, "")
	t.Setenv(config.EnvAuthStateDir, "")
	t.Setenv(config.EnvDevAnonymous, "false")
	cfg, err = config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.PasswordHash != "" || cfg.Auth.PublicOrigin != "" || cfg.Auth.StateDir != "" || cfg.Auth.DevAnonymous {
		t.Fatal("explicit empty overrides fell back to file")
	}
	for _, value := range []string{"", "1", "TRUE", " false ", "no"} {
		t.Setenv(config.EnvDevAnonymous, value)
		if _, err := config.LoadFile(path); err == nil {
			t.Fatalf("invalid development boolean %q accepted", value)
		}
	}
}

func TestLegacyAuthSettingsAlwaysRequireMigration(t *testing.T) {
	for _, value := range []string{"", "legacy-credential-marker"} {
		t.Run("TOML-"+value, func(t *testing.T) {
			_, err := config.LoadFile(writeFile(t, `auth_token_env = "`+value+`"`))
			if err == nil || !strings.Contains(err.Error(), "迁移") {
				t.Fatalf("expected migration error, got %v", err)
			}
			if value != "" && strings.Contains(err.Error(), value) {
				t.Fatal("legacy value leaked")
			}
		})
		t.Run("env-"+value, func(t *testing.T) {
			t.Setenv(config.LegacyAuthTokenEnv, value)
			_, err := config.LoadFile(filepath.Join(t.TempDir(), "absent"))
			if err == nil || !strings.Contains(err.Error(), "迁移") {
				t.Fatalf("expected migration error, got %v", err)
			}
			if value != "" && strings.Contains(err.Error(), value) {
				t.Fatal("legacy value leaked")
			}
		})
	}
}

func TestAuthStateCannotLiveInsideMemory(t *testing.T) {
	cfg := validConfig(t)
	cfg.Auth = config.AuthConfig{PasswordHash: "test-hash", PublicOrigin: "https://memory.example"}
	for _, dir := range []string{cfg.DataDir, filepath.Join(cfg.DataDir, "auth")} {
		cfg.Auth.StateDir = dir
		if err := cfg.Validate(); err == nil {
			t.Fatal("auth state overlaps memory")
		}
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(cfg.DataDir, alias); err != nil {
		t.Fatal(err)
	}
	cfg.Auth.StateDir = filepath.Join(alias, "auth")
	if err := cfg.Validate(); err == nil {
		t.Fatal("symlink bypasses memory isolation")
	}
}

func TestInvalidConfigNeverEchoesCredentialInput(t *testing.T) {
	marker := "private-hash-marker"
	_, err := config.LoadFile(writeFile(t, "[auth]\npassword_hash = "+marker))
	if err == nil {
		t.Fatal("malformed TOML accepted")
	}
	if strings.Contains(err.Error(), marker) {
		t.Fatal("parser echoed secret input")
	}
}
