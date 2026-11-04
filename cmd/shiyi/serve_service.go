//go:build serve_ready

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"shiyi/internal/answer"
	"shiyi/internal/classify"
	"shiyi/internal/config"
	"shiyi/internal/httpapi"
	"shiyi/internal/llm"
	"shiyi/internal/search"
	"shiyi/internal/store"
)

// shutdownTimeout bounds how long the service waits for in-flight requests.
const shutdownTimeout = 10 * time.Second

// runServe starts the local Shiyi service.
//
// The configuration is loaded and validated first, so that a deployment
// problem is reported before the socket is opened rather than on the first
// request. The start-up log names the listen address, the data directory and
// the shard granularity; it never names the API key.
func runServe(ctx context.Context, opts Options, stdout, stderr io.Writer) int {
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "shiyi: 加载配置失败: %v\n", err)
		return exitFailure
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(stderr, "shiyi: 配置无效: %v\n", err)
		return exitFailure
	}

	handler, err := buildServer(cfg, logger)
	if err != nil {
		fmt.Fprintf(stderr, "shiyi: 启动服务失败: %v\n", err)
		return exitFailure
	}

	server := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// The signal context is cancelled on SIGINT and SIGTERM; the service
	// then stops accepting and waits for the in-flight requests.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("shiyi 服务启动",
			"listen", cfg.Listen,
			"data_dir", cfg.DataDir,
			"shard", string(cfg.Shard),
		)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		if err != nil {
			fmt.Fprintf(stderr, "shiyi: 服务退出: %v\n", err)
			return exitFailure
		}
		return exitOK
	case <-ctx.Done():
		logger.Info("shiyi 服务收到停止信号，正在关闭")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(stderr, "shiyi: 关闭服务失败: %v\n", err)
		return exitFailure
	}
	logger.Info("shiyi 服务已停止")
	return exitOK
}

// buildServer assembles the service from the internal packages.
//
// The API key is read from the environment variable the configuration names
// and is handed to the model clients; it is never logged and never written
// anywhere.
func buildServer(cfg config.Config, logger *slog.Logger) (http.Handler, error) {
	classifierKey, err := cfg.APIKey()
	if err != nil {
		return nil, fmt.Errorf("读取分类器 API key: %w", err)
	}
	answerKey, err := cfg.APIKey()
	if err != nil {
		return nil, fmt.Errorf("读取回答 API key: %w", err)
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
	answerer := answer.New(answerClient, searcher, cfg.Answer.Model, cfg.Answer.Temperature, answer.Budget{
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

	return server.Handler(), nil
}
