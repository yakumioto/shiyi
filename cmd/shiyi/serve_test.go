package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"shiyi/internal/config"
)

// freeListenAddress returns a loopback address nothing is listening on.
func freeListenAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("占用端口失败: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()
	return addr
}

// serveDepsFor returns dependencies that serve a trivial handler on addr.
func serveDepsFor(addr string, loadErr error) serveDeps {
	return serveDeps{
		loadConfig: func() (config.Config, error) {
			if loadErr != nil {
				return config.Config{}, loadErr
			}
			return config.Config{
				Listen:  addr,
				DataDir: "/tmp/shiyi-test-data",
				Shard:   config.ShardMonthly,
			}, nil
		},
		build: func(cfg config.Config, logger *slog.Logger) (http.Handler, error) {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("ok"))
			}), nil
		},
	}
}

func TestRunServeReportsConfigProblem(t *testing.T) {
	var out, errOut strings.Builder

	code := runServeWith(
		context.Background(),
		Options{Mode: ModeServe},
		&out,
		&errOut,
		serveDepsFor("", errors.New("配置文件不存在")),
	)

	if code == 0 {
		t.Errorf("配置有问题时退出码应非零，得到 %d", code)
	}
	if !strings.Contains(errOut.String(), "配置文件不存在") {
		t.Errorf("标准错误 %q 应说明配置问题", errOut.String())
	}
}

func TestRunServeStartsAndStops(t *testing.T) {
	addr := freeListenAddress(t)
	var out, errOut strings.Builder

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		done <- runServeWith(ctx, Options{Mode: ModeServe}, &out, &errOut, serveDepsFor(addr, nil))
	}()

	// Wait for the service to answer.
	started := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/healthz")
		if err == nil {
			resp.Body.Close()
			started = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !started {
		cancel()
		t.Fatalf("服务没有启动: %v", errOut.String())
	}

	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("退出码 = %d，期望 0（stderr: %q）", code, errOut.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("服务没有在停止信号后退出")
	}

	logs := errOut.String()
	for _, want := range []string{addr, "/tmp/shiyi-test-data", "monthly"} {
		if !strings.Contains(logs, want) {
			t.Errorf("启动日志 %q 缺少 %q", logs, want)
		}
	}
}

func TestRunServeLogHasNoSecret(t *testing.T) {
	addr := freeListenAddress(t)
	var out, errOut strings.Builder

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		done <- runServeWith(ctx, Options{Mode: ModeServe}, &out, &errOut, serveDepsFor(addr, nil))
	}()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if resp, err := http.Get("http://" + addr + "/healthz"); err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done

	if strings.Contains(errOut.String(), "sk-") || strings.Contains(errOut.String(), "api_key") {
		t.Errorf("启动日志不应包含密钥: %q", errOut.String())
	}
}
