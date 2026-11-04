package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shiyi/internal/config"
)

// TestLoadResolvesTheDataDirectory guards against a literal "~" directory.
//
// The default data directory is written as "~/.local/share/shiyi". A "~" that
// survives the loading would be read as a directory named "~" relative to the
// working directory: the records would be written into it and the queries
// would read from a different place.
func TestLoadResolvesTheDataDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHIYI_CONFIG", filepath.Join(home, "config.toml"))

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if strings.Contains(cfg.DataDir, "~") {
		t.Fatalf("数据目录不应再含 ~，得到 %q", cfg.DataDir)
	}
	want := filepath.Join(home, ".local", "share", "shiyi")
	if cfg.DataDir != want {
		t.Fatalf("data_dir = %q, want %q", cfg.DataDir, want)
	}
}

// TestLoadResolvesTheConfiguredDataDirectory checks the same for a directory
// the configuration file names.
func TestLoadResolvesTheConfiguredDataDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path := filepath.Join(home, "config.toml")
	body := "data_dir = \"~/records\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("SHIYI_CONFIG", path)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := filepath.Join(home, "records")
	if cfg.DataDir != want {
		t.Fatalf("data_dir = %q, want %q", cfg.DataDir, want)
	}
}
