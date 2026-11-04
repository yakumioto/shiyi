// Package config loads and validates Shiyi's configuration.
//
// Configuration comes from a TOML file, is overridden by environment
// variables, and is validated at start-up so that deployment problems show up
// when the process starts rather than on the first request.
package config

import (
	"time"
)

// Shard is the granularity at which records are split across files.
type Shard string

// The supported shard granularities. All boundaries are UTC.
const (
	ShardMonthly Shard = "monthly"
	ShardWeekly  Shard = "weekly"
	ShardDaily   Shard = "daily"
)

// Valid reports whether s is a supported shard granularity.
func (s Shard) Valid() bool {
	switch s {
	case ShardMonthly, ShardWeekly, ShardDaily:
		return true
	}
	return false
}

// ModelConfig describes one OpenAI-compatible model endpoint. The API key is
// never stored in the configuration; it is read at run time from the
// environment variable named by APIKeyEnv.
type ModelConfig struct {
	BaseURL     string        `toml:"base_url"`
	Model       string        `toml:"model"`
	APIKeyEnv   string        `toml:"api_key_env"`
	Timeout     time.Duration `toml:"timeout"`
	Temperature float64       `toml:"temperature"`
}

// BudgetConfig caps how much work a single query may do.
type BudgetConfig struct {
	MaxSearchCalls int           `toml:"max_search_calls"`
	MaxRecords     int           `toml:"max_records"`
	AnswerTimeout  time.Duration `toml:"answer_timeout"`
	RequestTimeout time.Duration `toml:"request_timeout"`
}

// Config is the full configuration of a Shiyi process.
type Config struct {
	Listen       string       `toml:"listen"`
	DataDir      string       `toml:"data_dir"`
	Shard        Shard        `toml:"shard"`
	AuthTokenEnv string       `toml:"auth_token_env"`
	MaxBodyBytes int64        `toml:"max_body_bytes"`
	Classifier   ModelConfig  `toml:"classifier"`
	Answer       ModelConfig  `toml:"answer"`
	Budget       BudgetConfig `toml:"budget"`
}
