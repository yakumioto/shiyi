package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Environment variables that override the file and the built-in defaults.
const (
	EnvConfig  = "SHIYI_CONFIG"
	EnvListen  = "SHIYI_LISTEN"
	EnvDataDir = "SHIYI_DATA_DIR"
	EnvShard   = "SHIYI_SHARD"
)

// Defaults applied where neither the file nor the environment says otherwise.
const (
	DefaultListen       = "127.0.0.1:8787"
	DefaultDataDir      = "~/.local/share/shiyi"
	DefaultShard        = ShardMonthly
	DefaultMaxBodyBytes = 1 << 20 // 1 MiB

	DefaultClassifierTimeout = 20 * time.Second
	DefaultAnswerTimeout     = 30 * time.Second

	DefaultMaxSearchCalls = 3
	DefaultMaxRecords     = 20
	DefaultRequestTimeout = 60 * time.Second
)

// DefaultPath returns the configuration path used when SHIYI_CONFIG is unset.
func DefaultPath() string {
	return "~/.config/shiyi/config.toml"
}

// duration is a TOML-friendly time.Duration. It accepts a TOML string such as
// "20s" (the documented form) and also a bare integer, which is read as
// nanoseconds so that a file written against time.Duration still loads.
type duration time.Duration

// UnmarshalText implements encoding.TextUnmarshaler for TOML strings.
func (d *duration) UnmarshalText(b []byte) error {
	parsed, err := time.ParseDuration(string(b))
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", string(b), err)
	}
	*d = duration(parsed)
	return nil
}

// UnmarshalTOML accepts both the string form and the integer form.
func (d *duration) UnmarshalTOML(v any) error {
	switch value := v.(type) {
	case string:
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("invalid duration %q: %w", value, err)
		}
		*d = duration(parsed)
		return nil
	case int64:
		*d = duration(value)
		return nil
	default:
		return fmt.Errorf("invalid duration of type %T", v)
	}
}

func (d duration) Duration() time.Duration { return time.Duration(d) }

// fileModel mirrors one model section of the TOML file. Pointer fields let the
// loader tell "absent" from "present but zero", which is what the classifier's
// inheritance from the answer section depends on.
type fileModel struct {
	BaseURL     *string   `toml:"base_url"`
	Model       *string   `toml:"model"`
	APIKeyEnv   *string   `toml:"api_key_env"`
	Timeout     *duration `toml:"timeout"`
	Temperature *float64  `toml:"temperature"`
}

// fileBudget mirrors the budget section of the TOML file.
type fileBudget struct {
	MaxSearchCalls *int      `toml:"max_search_calls"`
	MaxRecords     *int      `toml:"max_records"`
	AnswerTimeout  *duration `toml:"answer_timeout"`
	RequestTimeout *duration `toml:"request_timeout"`
}

// fileConfig mirrors the whole TOML file.
type fileConfig struct {
	Auth         *AuthConfig `toml:"auth"`
	Listen       *string     `toml:"listen"`
	DataDir      *string     `toml:"data_dir"`
	Shard        *string     `toml:"shard"`
	AuthTokenEnv *string     `toml:"auth_token_env"`
	MaxBodyBytes *int64      `toml:"max_body_bytes"`
	Classifier   *fileModel  `toml:"classifier"`
	Answer       *fileModel  `toml:"answer"`
	Budget       *fileBudget `toml:"budget"`
}

// Load reads the configuration named by SHIYI_CONFIG, falling back to the
// default path. A missing file is not an error: the defaults and the
// environment are then used alone.
func Load() (Config, error) {
	path := os.Getenv(EnvConfig)
	if strings.TrimSpace(path) == "" {
		path = DefaultPath()
	}
	return LoadFile(path)
}

// LoadFile reads the configuration at path. A missing file is not an error.
func LoadFile(path string) (Config, error) {
	expanded, err := ExpandHome(path)
	if err != nil {
		return Config{}, err
	}

	var file fileConfig
	data, err := os.ReadFile(expanded)
	switch {
	case err == nil:
		if len(data) > 0 {
			if err := toml.Unmarshal(data, &file); err != nil {
				return Config{}, fmt.Errorf("parse %s: %w", expanded, err)
			}
		}
	case os.IsNotExist(err):
		// Not an error: the defaults and the environment are enough for the
		// loader, and Validate reports whatever is still missing.
	default:
		return Config{}, fmt.Errorf("read %s: %w", expanded, err)
	}

	cfg := build(file)

	if err := applyEnv(&cfg); err != nil {
		return Config{}, err
	}

	// The data directory is resolved here, once, so that every consumer
	// sees the same path. A "~" that is left in place would be read as a
	// directory named "~" relative to the working directory, and the
	// records would end up in a different place than the queries read.
	expandedDir, err := ExpandHome(cfg.DataDir)
	if err != nil {
		return Config{}, err
	}
	cfg.DataDir = expandedDir

	return cfg, nil
}

// build folds the parsed file and the built-in defaults into a Config.
// The answer section always exists; the classifier section inherits it when it
// was not written out.
func build(file fileConfig) Config {
	cfg := Config{
		Listen:       DefaultListen,
		DataDir:      DefaultDataDir,
		Shard:        DefaultShard,
		MaxBodyBytes: DefaultMaxBodyBytes,
		Budget: BudgetConfig{
			MaxSearchCalls: DefaultMaxSearchCalls,
			MaxRecords:     DefaultMaxRecords,
			AnswerTimeout:  DefaultAnswerTimeout,
			RequestTimeout: DefaultRequestTimeout,
		},
	}

	cfg.Auth.StateDir = DefaultAuthStateDir
	if file.Auth != nil {
		cfg.Auth = *file.Auth
		if cfg.Auth.StateDir == "" {
			cfg.Auth.StateDir = DefaultAuthStateDir
		}
	}

	if file.Listen != nil {
		cfg.Listen = *file.Listen
	}
	if file.DataDir != nil {
		cfg.DataDir = *file.DataDir
	}
	if file.Shard != nil {
		cfg.Shard = Shard(*file.Shard)
	}
	if file.AuthTokenEnv != nil {
		cfg.AuthTokenEnv = *file.AuthTokenEnv
	}
	if file.MaxBodyBytes != nil {
		cfg.MaxBodyBytes = *file.MaxBodyBytes
	}

	cfg.Answer = modelFromFile(file.Answer)
	if cfg.Answer.Timeout == 0 {
		cfg.Answer.Timeout = DefaultAnswerTimeout
	}
	// The classifier section inherits the answer section when it is absent.
	if file.Classifier == nil {
		cfg.Classifier = cfg.Answer
	} else {
		cfg.Classifier = inheritModel(file.Classifier, cfg.Answer)
	}
	if cfg.Classifier.Timeout == 0 {
		cfg.Classifier.Timeout = DefaultClassifierTimeout
	}

	if file.Budget != nil {
		if file.Budget.MaxSearchCalls != nil {
			cfg.Budget.MaxSearchCalls = *file.Budget.MaxSearchCalls
		}
		if file.Budget.MaxRecords != nil {
			cfg.Budget.MaxRecords = *file.Budget.MaxRecords
		}
		if file.Budget.AnswerTimeout != nil {
			cfg.Budget.AnswerTimeout = file.Budget.AnswerTimeout.Duration()
		}
		if file.Budget.RequestTimeout != nil {
			cfg.Budget.RequestTimeout = file.Budget.RequestTimeout.Duration()
		}
	}

	return cfg
}

// modelFromFile turns one file section into a ModelConfig. Absent fields stay
// at their zero value so that Validate can report them.
func modelFromFile(file *fileModel) ModelConfig {
	var m ModelConfig
	if file == nil {
		return m
	}
	if file.BaseURL != nil {
		m.BaseURL = *file.BaseURL
	}
	if file.Model != nil {
		m.Model = *file.Model
	}
	if file.APIKeyEnv != nil {
		m.APIKeyEnv = *file.APIKeyEnv
	}
	if file.Timeout != nil {
		m.Timeout = file.Timeout.Duration()
	}
	if file.Temperature != nil {
		m.Temperature = *file.Temperature
	}
	return m
}

// inheritModel fills the fields the classifier section left out from the
// answer section.
func inheritModel(file *fileModel, answer ModelConfig) ModelConfig {
	m := answer
	if file == nil {
		return m
	}
	if file.BaseURL != nil {
		m.BaseURL = *file.BaseURL
	}
	if file.Model != nil {
		m.Model = *file.Model
	}
	if file.APIKeyEnv != nil {
		m.APIKeyEnv = *file.APIKeyEnv
	}
	if file.Timeout != nil {
		m.Timeout = file.Timeout.Duration()
	}
	if file.Temperature != nil {
		m.Temperature = *file.Temperature
	}
	return m
}

// applyEnv lets the environment win over the file.
func applyEnv(cfg *Config) error {
	if v := os.Getenv(EnvListen); strings.TrimSpace(v) != "" {
		cfg.Listen = v
	}
	if v := os.Getenv(EnvDataDir); strings.TrimSpace(v) != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv(EnvShard); strings.TrimSpace(v) != "" {
		cfg.Shard = Shard(v)
	}
	if v := os.Getenv("SHIYI_MAX_BODY_BYTES"); strings.TrimSpace(v) != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return fmt.Errorf("%s must be an integer: %w", "SHIYI_MAX_BODY_BYTES", err)
		}
		cfg.MaxBodyBytes = n
	}
	return nil
}

// ExpandHome replaces a leading "~" with the user's home directory.
func ExpandHome(path string) (string, error) {
	if path == "" || path[0] != '~' {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	rest := strings.TrimPrefix(path[1:], string(os.PathSeparator))
	if rest == "" {
		return home, nil
	}
	return home + string(os.PathSeparator) + rest, nil
}
