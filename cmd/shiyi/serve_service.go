// This file holds the real wiring of the service: it loads and validates the
// configuration, assembles the memory store, the model clients, the search,
// the answering loop and the HTTP server, and hands the result to the server
// lifecycle in serve.go.

package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"shiyi/internal/answer"
	"shiyi/internal/classify"
	"shiyi/internal/config"
	"shiyi/internal/httpapi"
	"shiyi/internal/llm"
	"shiyi/internal/search"
	"shiyi/internal/store"
)

// defaultServeDeps wires the service to the real internal packages.
var defaultServeDeps = serveDeps{
	loadConfig: loadAndValidateConfig,
	build:      buildServer,
}

// loadAndValidateConfig reads the configuration and checks it.
//
// The configuration comes from SHIYI_CONFIG or from the default path; a
// problem with it is reported before the socket is opened, so that a
// deployment mistake shows up at start-up rather than on the first request.
func loadAndValidateConfig() (config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return config.Config{}, fmt.Errorf("加载配置失败: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return config.Config{}, fmt.Errorf("配置无效: %w", err)
	}
	return cfg, nil
}

// buildServer assembles the service from the internal packages.
//
// The API key is read from the environment variable the configuration names
// and is handed to the model clients; it is never logged and never written
// anywhere.
func buildServer(cfg config.Config, logger *slog.Logger) (http.Handler, error) {
	answerKey, err := cfg.APIKey()
	if err != nil {
		return nil, fmt.Errorf("读取 API key: %w", err)
	}

	// The classifier may name its own key variable; otherwise it inherits
	// the one of the answer model.
	classifierKey := answerKey
	if cfg.Classifier.APIKeyEnv != "" && cfg.Classifier.APIKeyEnv != cfg.Answer.APIKeyEnv {
		classifierKey = strings.TrimSpace(os.Getenv(cfg.Classifier.APIKeyEnv))
	}

	classifierClient := llm.New(llm.Options{
		BaseURL:     cfg.Classifier.BaseURL,
		Model:       cfg.Classifier.Model,
		APIKey:      classifierKey,
		Timeout:     cfg.Classifier.Timeout,
		Temperature: cfg.Classifier.Temperature,
		HTTPClient:  &http.Client{Timeout: cfg.Classifier.Timeout},
		Logger:      logger,
	})

	answerClient := llm.New(llm.Options{
		BaseURL:     cfg.Answer.BaseURL,
		Model:       cfg.Answer.Model,
		APIKey:      answerKey,
		Timeout:     cfg.Answer.Timeout,
		Temperature: cfg.Answer.Temperature,
		HTTPClient:  &http.Client{Timeout: cfg.Answer.Timeout},
		Logger:      logger,
	})

	memory, err := store.New(cfg.DataDir, string(cfg.Shard))
	if err != nil {
		return nil, fmt.Errorf("打开记忆存储: %w", err)
	}

	judge := classify.New(classifierClient, cfg.Classifier.Model, cfg.Classifier.Temperature)
	searcher := search.New(cfg.DataDir)
	answerer := answer.New(answerClient, searcher, cfg.Answer.Model, answer.Budget{
		MaxSearchCalls: cfg.Budget.MaxSearchCalls,
		MaxRecords:     cfg.Budget.MaxRecords,
		AnswerTimeout:  cfg.Budget.AnswerTimeout,
	})

	server, err := httpapi.New(httpapi.Deps{
		Config:   cfg,
		Judge:    judge,
		Store:    memory,
		Answerer: answerer,
		Logger:   logger,
	})
	if err != nil {
		return nil, fmt.Errorf("构造 HTTP 服务: %w", err)
	}

	// The web page is served by the same process, so that the deployment
	// stays a single binary.
	if page := webPage(); page != nil {
		return server.HandlerWithWeb(page), nil
	}

	return server.Handler(), nil
}
