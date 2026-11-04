// Package frozen runs the versioned, immutable fixture set.
//
// The fixtures pin the behaviour of everything above the model: intent
// classification, the query path, citation checking, the budget, and the
// failure paths. Each fixture supplies the model's turns, so no test here ever
// reaches the network or needs an API key.
//
// A frozen version is never edited in place. When a prompt or a behaviour has
// to change, a new version directory is added and every version keeps running.
package frozen

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ManifestName is the file that pins the bytes of every fixture.
const ManifestName = "manifest.json"

// Manifest records the SHA-256 of each fixture file of one frozen version.
type Manifest struct {
	// Version is the fixture-set version, matching the directory name.
	Version string `json:"version"`
	// Files maps a fixture file name to its SHA-256.
	Files map[string]string `json:"files"`
}

// LoadManifest reads the manifest of one version directory.
func LoadManifest(dir string) (Manifest, error) {
	path := filepath.Join(dir, ManifestName)
	content, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read %s: %w", path, err)
	}
	var manifest Manifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if manifest.Version == "" {
		return Manifest{}, fmt.Errorf("%s does not name a version", path)
	}
	if len(manifest.Files) == 0 {
		return Manifest{}, fmt.Errorf("%s lists no files", path)
	}
	return manifest, nil
}

// VerifyManifest checks every file the manifest names against the bytes on
// disk, and reports a fixture that was added without being registered.
//
// A mismatch is always a failure: a frozen fixture that no longer matches its
// hash means the behaviour under test was changed without a new version.
func VerifyManifest(dir string, manifest Manifest) error {
	var problems []string

	names := make([]string, 0, len(manifest.Files))
	for name := range manifest.Files {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		want := manifest.Files[name]
		path := filepath.Join(dir, name)
		content, err := os.ReadFile(path)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		got := sum(content)
		if got != want {
			problems = append(problems, fmt.Sprintf(
				"%s: sha256 is %s but the manifest says %s (frozen fixtures must not be edited in place)",
				name, got, want))
		}
	}

	// A fixture that exists but is not registered would otherwise drift freely.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read %s: %w", dir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == ManifestName {
			continue
		}
		if _, registered := manifest.Files[name]; !registered {
			problems = append(problems, fmt.Sprintf(
				"%s exists but is not recorded in %s", name, ManifestName))
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("frozen fixtures in %s failed verification:\n  - %s",
			filepath.Base(dir), strings.Join(problems, "\n  - "))
	}
	return nil
}

// sum returns the hex SHA-256 of content.
func sum(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

// Versions lists the frozen version directories under root, oldest first.
func Versions(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", root, err)
	}
	var versions []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, entry.Name(), ManifestName)); err != nil {
			continue
		}
		versions = append(versions, entry.Name())
	}
	sort.Strings(versions)
	return versions, nil
}
