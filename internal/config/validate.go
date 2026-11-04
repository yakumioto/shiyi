package config

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ValidationError reports every problem found in one validation pass, so a
// misconfigured deployment can be fixed in one edit rather than one restart
// per mistake.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return "配置无效：\n  - " + strings.Join(e.Problems, "\n  - ")
}

// Validate reports whether the configuration can start the service. It checks
// everything that can be checked locally: required fields, the shard
// enumeration, a writable data directory, a usable ripgrep, and the presence
// of each API key in the environment.
//
// The API key itself is never included in an error message.
func (c Config) Validate() error {
	var problems []string

	if strings.TrimSpace(c.Listen) == "" {
		problems = append(problems, "listen 不能为空（设置 listen 或 SHIYI_LISTEN）")
	}
	if !c.Shard.Valid() {
		problems = append(problems, fmt.Sprintf(
			"shard %q 不是 monthly、weekly、daily 之一（设置 shard 或 SHIYI_SHARD）", string(c.Shard)))
	}
	if c.MaxBodyBytes <= 0 {
		problems = append(problems, "max_body_bytes 必须是正数")
	}

	problems = append(problems, validateModel("answer", c.Answer)...)
	// Only check the classifier separately when it does not simply mirror the
	// answer section; otherwise a problem would be reported twice.
	if c.Classifier != c.Answer {
		problems = append(problems, validateModel("classifier", c.Classifier)...)
	}

	if err := c.validateDataDir(); err != nil {
		problems = append(problems, err.Error())
	}
	if err := c.validateRipgrep(); err != nil {
		problems = append(problems, err.Error())
	}

	if c.Budget.MaxSearchCalls <= 0 {
		problems = append(problems, "budget.max_search_calls 必须是正数")
	}
	if c.Budget.MaxRecords <= 0 {
		problems = append(problems, "budget.max_records 必须是正数")
	}
	if c.Budget.AnswerTimeout <= 0 {
		problems = append(problems, "budget.answer_timeout 必须是正数")
	}
	if c.Budget.RequestTimeout <= 0 {
		problems = append(problems, "budget.request_timeout 必须是正数")
	}

	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

// validateModel checks one model section.
func validateModel(name string, m ModelConfig) []string {
	var problems []string
	prefix := name + "."

	if strings.TrimSpace(m.BaseURL) == "" {
		problems = append(problems, prefix+"base_url 不能为空")
	}
	if strings.TrimSpace(m.Model) == "" {
		problems = append(problems, prefix+"model 不能为空")
	}
	if strings.TrimSpace(m.APIKeyEnv) == "" {
		problems = append(problems, prefix+"api_key_env 必须给出存 API key 的环境变量名")
	} else if _, err := m.apiKey(); err != nil {
		problems = append(problems, prefix+"api_key_env："+err.Error())
	}
	if m.Timeout <= 0 {
		problems = append(problems, prefix+"timeout 必须是正数")
	}
	return problems
}

// validateDataDir ensures the data directory exists and accepts writes.
func (c Config) validateDataDir() error {
	expanded, err := ExpandHome(c.DataDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(expanded, 0o755); err != nil {
		return fmt.Errorf("data_dir %s 不可写：%w", expanded, err)
	}
	probe, err := os.CreateTemp(expanded, ".shiyi-write-probe-*")
	if err != nil {
		return fmt.Errorf("data_dir %s 不可写：%w", expanded, err)
	}
	name := probe.Name()
	probe.Close()
	os.Remove(name)
	return nil
}

// validateRipgrep ensures the search binary is on PATH.
func (c Config) validateRipgrep() error {
	if _, err := exec.LookPath("rg"); err != nil {
		return errors.New("PATH 里找不到 ripgrep（rg），请先安装再启动 Shiyi")
	}
	return nil
}

// APIKey returns the answer model's API key, read from the environment
// variable named by the answer section. It never reads the key from the
// configuration file, and the key is never written to a log.
func (c Config) APIKey() (string, error) {
	return c.Answer.apiKey()
}

// APIKeyFor returns the API key for one model section.
func APIKeyFor(m ModelConfig) (string, error) {
	return m.apiKey()
}

// apiKey reads one model section's key from its environment variable.
func (m ModelConfig) apiKey() (string, error) {
	name := strings.TrimSpace(m.APIKeyEnv)
	if name == "" {
		return "", errors.New("api_key_env 必须给出存 API key 的环境变量名")
	}
	value := os.Getenv(name)
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("环境变量 %s 为空，请把 API key 导出到那里", name)
	}
	return value, nil
}

// AuthToken returns the bearer token required from clients, read from the
// environment variable named by AuthTokenEnv. An unset or empty variable means
// authentication is off.
func (c Config) AuthToken() string {
	name := strings.TrimSpace(c.AuthTokenEnv)
	if name == "" {
		return ""
	}
	return os.Getenv(name)
}

// ExpandedDataDir returns the data directory with "~" resolved.
func (c Config) ExpandedDataDir() (string, error) {
	return ExpandHome(c.DataDir)
}
