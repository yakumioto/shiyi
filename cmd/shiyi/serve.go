package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"shiyi/internal/config"
)

// shutdownTimeout bounds how long the service waits for in-flight requests.
const shutdownTimeout = 10 * time.Second

// serveDeps are the pieces the service needs from the rest of the system.
//
// They are injected so that the life cycle of the server — starting,
// listening, shutting down — can be tested without the model, the storage
// and the memory entries behind it.
type serveDeps struct {
	// loadConfig reads and validates the configuration.
	loadConfig func() (config.Config, error)
	// build assembles the HTTP handler out of the configuration.
	build func(config.Config, *slog.Logger) (http.Handler, error)
}

// runServe starts the local Shiyi service.
func runServe(ctx context.Context, opts Options, stdout, stderr io.Writer) int {
	return runServeWith(ctx, opts, stdout, stderr, defaultServeDeps)
}

// runServeWith starts the service with the given dependencies.
//
// The configuration is loaded and validated first, so that a deployment
// problem is reported before the socket is opened rather than on the first
// request. The start-up log names the listen address, the data directory and
// the shard granularity; it never names the API key.
func runServeWith(ctx context.Context, opts Options, stdout, stderr io.Writer, deps serveDeps) int {
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := deps.loadConfig()
	if err != nil {
		fmt.Fprintf(stderr, "shiyi: %v\n", err)
		return exitFailure
	}

	handler, err := deps.build(cfg, logger)
	if err != nil {
		fmt.Fprintf(stderr, "shiyi: 启动服务失败: %v\n", err)
		return exitFailure
	}

	if closer, ok := handler.(io.Closer); ok {
		defer func() {
			if err := closer.Close(); err != nil {
				logger.Error("关闭认证资源失败")
			}
		}()
	}

	server := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

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
		_ = server.Close()
		fmt.Fprintf(stderr, "shiyi: 关闭服务失败: %v\n", err)
		return exitFailure
	}
	logger.Info("shiyi 服务已停止")
	return exitOK
}
