package httpapi

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"shiyi/internal/config"
)

// validateConfig runs the start-up checks of the server.
//
// The checks are the ones that would otherwise turn into a confusing failure
// on the first request: ripgrep has to be executable, the data directory has
// to be writable and the model endpoints have to be configured.
//
// The API key is deliberately not checked here. The configuration does not
// hold it, the model client reads it from the environment, and the CLI
// reports a missing key when it builds the clients.
func validateConfig(cfg config.Config) error {
	if err := checkRipgrep(); err != nil {
		return err
	}
	if err := checkDataDir(cfg.DataDir); err != nil {
		return err
	}
	return checkModelConfig(cfg)
}

// checkRipgrep makes sure ripgrep can be run.
//
// The search pre-filters the shard files with ripgrep; without it a query
// cannot find anything, and the failure would only show up when a user asks
// a question.
func checkRipgrep() error {
	path, err := exec.LookPath("rg")
	if err != nil {
		return errors.New("httpapi: ripgrep (rg) is not installed or not in PATH")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("httpapi: ripgrep at %s cannot be read: %w", path, err)
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return fmt.Errorf("httpapi: ripgrep at %s is not executable", path)
	}
	return nil
}

// checkDataDir makes sure the data directory can be written to.
//
// The directory is created when it does not exist yet, because a fresh
// installation has none. Writing there is what a record does, so a directory
// that cannot be written to would turn every record into a failure.
func checkDataDir(dir string) error {
	if dir == "" {
		return errors.New("httpapi: no data directory is configured")
	}

	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}

	if err := os.MkdirAll(abs, 0o755); err != nil {
		return fmt.Errorf("httpapi: the data directory %s cannot be created: %w", abs, err)
	}

	probe, err := os.CreateTemp(abs, ".shiyi-write-probe-*")
	if err != nil {
		return fmt.Errorf("httpapi: the data directory %s is not writable: %w", abs, err)
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)

	return nil
}

// checkModelConfig makes sure the model endpoints are configured.
//
// A classifier that is not configured on its own inherits the answer
// endpoint, which is the rule of the configuration file. The API key is not
// looked at: its environment variable is read by the model client.
func checkModelConfig(cfg config.Config) error {
	answerBase := cfg.Answer.BaseURL
	answerModel := cfg.Answer.Model
	if answerBase == "" {
		return errors.New("httpapi: no model endpoint is configured (answer.base_url is empty)")
	}
	if answerModel == "" {
		return errors.New("httpapi: no model is configured (answer.model is empty)")
	}

	classifierBase := cfg.Classifier.BaseURL
	if classifierBase == "" {
		classifierBase = answerBase
	}
	classifierModel := cfg.Classifier.Model
	if classifierModel == "" {
		classifierModel = answerModel
	}
	if classifierBase == "" {
		return errors.New("httpapi: no classifier endpoint is configured")
	}
	if classifierModel == "" {
		return errors.New("httpapi: no classifier model is configured")
	}
	return nil
}
