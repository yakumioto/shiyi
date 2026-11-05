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
	"shiyi/internal/auth"
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
		return config.Config{}, err
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

	// The configured directory may start with "~", which only means the user's
	// home directory once it has been expanded. Both the store and the search
	// must see the same resolved path, otherwise a query would read a
	// different directory from the one records are written to.
	dataDir, err := cfg.ExpandedDataDir()
	if err != nil {
		return nil, fmt.Errorf("解析数据目录: %w", err)
	}

	memory, err := store.New(dataDir, string(cfg.Shard))
	if err != nil {
		return nil, fmt.Errorf("打开记忆存储: %w", err)
	}

	judge := classify.New(classifierClient, cfg.Classifier.Model, cfg.Classifier.Temperature)
	searcher := search.New(dataDir)
	answerer := answer.New(answerClient, searcher, cfg.Answer.Model, cfg.Answer.Temperature, answer.Budget{
		MaxSearchCalls: cfg.Budget.MaxSearchCalls,
		MaxRecords:     cfg.Budget.MaxRecords,
		AnswerTimeout:  cfg.Budget.AnswerTimeout,
	})

	module, err := auth.New(cfg.Auth, auth.Options{})
	if err != nil {
		return nil, err
	}
	server, err := httpapi.New(httpapi.Deps{
		Auth:     module,
		Config:   cfg,
		Judge:    judge,
		Store:    memory,
		Answerer: answerer,
		Logger:   logger,
	})
	if err != nil {
		_ = module.Close()
		return nil, fmt.Errorf("构造 HTTP 服务: %w", err)
	}

	// The web page is served by the same process, so that the deployment
	// stays a single binary.
	if page := webPage(); page != nil {
		return &serviceHandler{Handler: server.HandlerWithWeb(page), server: server}, nil
	}

	return &serviceHandler{Handler: server.Handler(), server: server}, nil
}

type serviceHandler struct {
	http.Handler
	server *httpapi.Server
}

func (s *serviceHandler) Close() error { return s.server.Close() }
