package config_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shiyi/internal/config"
)

// writeFile writes body to a fresh temp file and returns its path.
func writeFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

const answerOnly = `
listen = "127.0.0.1:9000"
data_dir = "%s"
shard = "daily"

[answer]
base_url = "https://example.test"
model = "answer-model"
api_key_env = "TEST_API_KEY"
timeout = "30s"
temperature = 0.2
`

// TestLoadReadsDocumentedConfigFile checks the shape design.md documents,
// including the string durations that plain time.Duration cannot decode.
func TestLoadReadsDocumentedConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, strings.Replace(answerOnly, "%s", dir, 1))

	t.Setenv("TEST_API_KEY", "secret-value")

	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	if got, want := cfg.Listen, "127.0.0.1:9000"; got != want {
		t.Errorf("listen = %q, want %q", got, want)
	}
	if got, want := cfg.DataDir, dir; got != want {
		t.Errorf("data_dir = %q, want %q", got, want)
	}
	if got, want := cfg.Shard, config.ShardDaily; got != want {
		t.Errorf("shard = %q, want %q", got, want)
	}
	if got, want := cfg.Answer.Timeout, 30*time.Second; got != want {
		t.Errorf("answer.timeout = %v, want %v (TOML string duration)", got, want)
	}
	if got, want := cfg.Answer.Temperature, 0.2; got != want {
		t.Errorf("answer.temperature = %v, want %v", got, want)
	}
}

// TestDurationFieldsAreStringsInTheFile is the regression test for the
// documented configuration contract: times are written as "20s", not as a
// bare integer.
func TestDurationFieldsAreStringsInTheFile(t *testing.T) {
	dir := t.TempDir()
	body := `
data_dir = "` + dir + `"
[answer]
base_url = "https://example.test"
model = "m"
api_key_env = "TEST_API_KEY"
timeout = "20s"
[budget]
answer_timeout = "45s"
request_timeout = "90s"
`
	t.Setenv("TEST_API_KEY", "secret-value")

	cfg, err := config.LoadFile(writeFile(t, body))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if got, want := cfg.Answer.Timeout, 20*time.Second; got != want {
		t.Errorf("answer.timeout = %v, want %v", got, want)
	}
	if got, want := cfg.Budget.AnswerTimeout, 45*time.Second; got != want {
		t.Errorf("budget.answer_timeout = %v, want %v", got, want)
	}
	if got, want := cfg.Budget.RequestTimeout, 90*time.Second; got != want {
		t.Errorf("budget.request_timeout = %v, want %v", got, want)
	}
}

// TestClassifierInheritsAnswerWhenOmitted covers the documented rule that an
// omitted classifier section reuses the answer section.
func TestClassifierInheritsAnswerWhenOmitted(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TEST_API_KEY", "secret-value")

	cfg, err := config.LoadFile(writeFile(t, strings.Replace(answerOnly, "%s", dir, 1)))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.Classifier != cfg.Answer {
		t.Fatalf("classifier did not inherit answer:\n classifier = %+v\n answer = %+v",
			cfg.Classifier, cfg.Answer)
	}
}

// TestClassifierOverridesOnlyNamedFields checks partial overrides: unnamed
// classifier fields still come from the answer section.
func TestClassifierOverridesOnlyNamedFields(t *testing.T) {
	dir := t.TempDir()
	body := `
data_dir = "` + dir + `"
[answer]
base_url = "https://example.test"
model = "answer-model"
api_key_env = "TEST_API_KEY"
timeout = "30s"
temperature = 0.5
[classifier]
model = "classifier-model"
timeout = "10s"
`
	t.Setenv("TEST_API_KEY", "secret-value")

	cfg, err := config.LoadFile(writeFile(t, body))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if got, want := cfg.Classifier.Model, "classifier-model"; got != want {
		t.Errorf("classifier.model = %q, want %q", got, want)
	}
	if got, want := cfg.Classifier.Timeout, 10*time.Second; got != want {
		t.Errorf("classifier.timeout = %v, want %v", got, want)
	}
	if got, want := cfg.Classifier.BaseURL, "https://example.test"; got != want {
		t.Errorf("classifier.base_url = %q, want inherited %q", got, want)
	}
	if got, want := cfg.Classifier.Temperature, 0.5; got != want {
		t.Errorf("classifier.temperature = %v, want inherited %v", got, want)
	}
}

// TestDefaultsApplyWhenFileIsMissing checks that a missing file is not an
// error and that the documented defaults are in place.
func TestDefaultsApplyWhenFileIsMissing(t *testing.T) {
	cfg, err := config.LoadFile(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatalf("LoadFile on a missing file should not fail: %v", err)
	}
	if got, want := cfg.Listen, config.DefaultListen; got != want {
		t.Errorf("listen = %q, want default %q", got, want)
	}
	if got, want := cfg.Shard, config.ShardMonthly; got != want {
		t.Errorf("shard = %q, want default %q", got, want)
	}
	if got, want := cfg.MaxBodyBytes, int64(config.DefaultMaxBodyBytes); got != want {
		t.Errorf("max_body_bytes = %d, want default %d", got, want)
	}
	if got, want := cfg.Budget.MaxSearchCalls, config.DefaultMaxSearchCalls; got != want {
		t.Errorf("max_search_calls = %d, want default %d", got, want)
	}
	if got, want := cfg.Budget.MaxRecords, config.DefaultMaxRecords; got != want {
		t.Errorf("max_records = %d, want default %d", got, want)
	}
	if got, want := cfg.Budget.AnswerTimeout, config.DefaultAnswerTimeout; got != want {
		t.Errorf("answer_timeout = %v, want default %v", got, want)
	}
	if got, want := cfg.Budget.RequestTimeout, config.DefaultRequestTimeout; got != want {
		t.Errorf("request_timeout = %v, want default %v", got, want)
	}
}

// TestEnvironmentOverridesFile checks that the documented environment
// variables win over the file.
func TestEnvironmentOverridesFile(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, strings.Replace(answerOnly, "%s", dir, 1))

	t.Setenv("SHIYI_LISTEN", "0.0.0.0:1234")
	t.Setenv("SHIYI_DATA_DIR", "/tmp/from-env")
	t.Setenv("SHIYI_SHARD", "weekly")
	t.Setenv("TEST_API_KEY", "secret-value")

	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if got, want := cfg.Listen, "0.0.0.0:1234"; got != want {
		t.Errorf("listen = %q, want env %q", got, want)
	}
	if got, want := cfg.DataDir, "/tmp/from-env"; got != want {
		t.Errorf("data_dir = %q, want env %q", got, want)
	}
	if got, want := cfg.Shard, config.ShardWeekly; got != want {
		t.Errorf("shard = %q, want env %q", got, want)
	}
}

// TestLoadUsesConfigEnvironmentVariable checks the SHIYI_CONFIG indirection.
func TestLoadUsesConfigEnvironmentVariable(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, strings.Replace(answerOnly, "%s", dir, 1))

	t.Setenv("SHIYI_CONFIG", path)
	t.Setenv("TEST_API_KEY", "secret-value")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.Listen, "127.0.0.1:9000"; got != want {
		t.Errorf("listen = %q, want %q from SHIYI_CONFIG file", got, want)
	}
}

// TestAPIKeyComesFromEnvironmentOnly checks that the key is read from the
// named environment variable and never from the file.
func TestAPIKeyComesFromEnvironmentOnly(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, strings.Replace(answerOnly, "%s", dir, 1))

	t.Setenv("TEST_API_KEY", "key-from-environment")

	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	got, err := cfg.APIKey()
	if err != nil {
		t.Fatalf("APIKey: %v", err)
	}
	if want := "key-from-environment"; got != want {
		t.Fatalf("APIKey() = %q, want %q", got, want)
	}
}

// TestAPIKeyMissingReportsVariableName checks the error names the variable to
// set, without inventing a value.
func TestAPIKeyMissingReportsVariableName(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, strings.Replace(answerOnly, "%s", dir, 1))

	t.Setenv("TEST_API_KEY", "")

	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	_, err = cfg.APIKey()
	if err == nil {
		t.Fatal("APIKey() should fail when the environment variable is empty")
	}
	if !strings.Contains(err.Error(), "TEST_API_KEY") {
		t.Fatalf("error should name the variable: %v", err)
	}
}

// TestExpandHome resolves a leading tilde.
func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}

	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain path is untouched", in: "/var/lib/shiyi", want: "/var/lib/shiyi"},
		{name: "empty stays empty", in: "", want: ""},
		{name: "tilde alone is the home directory", in: "~", want: home},
		{name: "tilde prefix expands", in: "~/data", want: filepath.Join(home, "data")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := config.ExpandHome(tc.in)
			if err != nil {
				t.Fatalf("ExpandHome(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("ExpandHome(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// validConfig returns a Config that passes Validate in this environment.
func validConfig(t *testing.T) config.Config {
	t.Helper()
	t.Setenv("TEST_API_KEY", "secret-value")
	cfg, err := config.LoadFile(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	cfg.DataDir = t.TempDir()
	cfg.Answer = config.ModelConfig{
		BaseURL:   "https://example.test",
		Model:     "m",
		APIKeyEnv: "TEST_API_KEY",
		Timeout:   time.Second,
	}
	cfg.Classifier = cfg.Answer
	cfg.Auth = config.AuthConfig{DevAnonymous: true, PublicOrigin: "http://memory.test"}
	return cfg
}

// TestValidateAcceptsUsableConfiguration is the happy path.
func TestValidateAcceptsUsableConfiguration(t *testing.T) {
	cfg := validConfig(t)
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("ripgrep is not installed; skipping validation happy path")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate on a usable configuration: %v", err)
	}
}

// TestValidateReportsEveryProblemAtOnce checks that a broken deployment can be
// fixed in one pass instead of one restart per mistake.
func TestValidateReportsEveryProblemAtOnce(t *testing.T) {
	cfg := validConfig(t)
	cfg.Shard = "fortnightly"
	cfg.Answer.BaseURL = ""
	cfg.Answer.Model = ""
	cfg.Budget.MaxRecords = 0

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate should reject this configuration")
	}
	message := err.Error()
	for _, want := range []string{"shard", "base_url", "model", "max_records"} {
		if !strings.Contains(message, want) {
			t.Errorf("error message should mention %q, got:\n%s", want, message)
		}
	}
}

// TestValidateRejectsInvalidShard checks the documented shard enumeration.
func TestValidateRejectsInvalidShard(t *testing.T) {
	cfg := validConfig(t)
	cfg.Shard = "yearly"
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "shard") {
		t.Fatalf("Validate should reject shard %q, got %v", cfg.Shard, err)
	}
}

// TestValidateRejectsMissingAPIKey checks that a missing key is a start-up
// failure, and that the message names the variable rather than the value.
func TestValidateRejectsMissingAPIKey(t *testing.T) {
	cfg := validConfig(t)
	t.Setenv("TEST_API_KEY", "")

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate should reject a configuration whose API key is missing")
	}
	if !strings.Contains(err.Error(), "TEST_API_KEY") {
		t.Errorf("error should name the environment variable: %v", err)
	}
	if strings.Contains(err.Error(), "secret-value") {
		t.Errorf("error must never contain the key value: %v", err)
	}
}

// TestValidateErrorNeverContainsKeyValue guards the logging rule.
func TestValidateErrorNeverContainsKeyValue(t *testing.T) {
	cfg := validConfig(t)
	t.Setenv("TEST_API_KEY", "super-secret-key-material")
	cfg.Shard = "nope"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected a validation error")
	}
	if strings.Contains(err.Error(), "super-secret-key-material") {
		t.Fatalf("validation error leaked the key: %v", err)
	}
}
