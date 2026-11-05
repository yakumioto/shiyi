package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"shiyi/internal/auth"
	"shiyi/internal/config"
)

func serviceAuthConfig(t *testing.T) config.Config {
	t.Helper()
	root := t.TempDir()
	hash, err := bcrypt.GenerateFromPassword([]byte("service-test-password"), 10)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHIYI_SERVICE_TEST_KEY", "sk-service-test-marker")
	model := config.ModelConfig{BaseURL: "http://127.0.0.1:1", Model: "test", APIKeyEnv: "SHIYI_SERVICE_TEST_KEY", Timeout: time.Second}
	return config.Config{Listen: freeListenAddress(t), DataDir: filepath.Join(root, "memory"), Shard: config.ShardMonthly, MaxBodyBytes: 1 << 20, Answer: model, Classifier: model, Budget: config.BudgetConfig{MaxSearchCalls: 3, MaxRecords: 20, AnswerTimeout: time.Second, RequestTimeout: time.Second}, Auth: config.AuthConfig{PasswordHash: string(hash), StateDir: filepath.Join(root, "auth"), PublicOrigin: "https://memory.example"}}
}

func TestRealServeKeepsAuthenticationAvailableAndReleasesStateAfterShutdown(t *testing.T) {
	cfg := serviceAuthConfig(t)
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var logs strings.Builder
	done := make(chan int, 1)
	go func() {
		done <- runServeWith(ctx, Options{Mode: ModeServe}, io.Discard, &logs, serveDeps{loadConfig: func() (config.Config, error) { return cfg, nil }, build: buildServer})
	}()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	ready := false
	for !ready {
		select {
		case <-deadline.C:
			cancel()
			<-done
			t.Fatal("real service did not start")
		case <-ticker.C:
			res, err := http.Get("http://" + cfg.Listen + "/auth/session")
			if err == nil {
				io.Copy(io.Discard, res.Body)
				res.Body.Close()
				if res.StatusCode != 200 {
					cancel()
					<-done
					t.Fatalf("auth depends on unreachable model: %d", res.StatusCode)
				}
				ready = true
			}
		}
	}
	m, err := auth.New(cfg.Auth, auth.Options{})
	if m != nil {
		m.Close()
	}
	if !errors.Is(err, auth.ErrUnavailable) {
		cancel()
		<-done
		t.Fatal("real serve did not own auth directory lock")
	}
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("shutdown failed: %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not finish")
	}
	m, err = auth.New(cfg.Auth, auth.Options{})
	if err != nil {
		t.Fatalf("shutdown leaked authentication lock: %v", err)
	}
	m.Close()
	for _, secret := range []string{cfg.Auth.PasswordHash, "service-test-password", "sk-service-test-marker"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("service logs leaked credential")
		}
	}
}

func TestFailedHTTPAssemblyClosesAuthenticationModule(t *testing.T) {
	cfg := serviceAuthConfig(t)
	cfg.Answer.Model = ""
	cfg.Classifier.Model = ""
	handler, err := buildServer(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil {
		if closer, ok := handler.(io.Closer); ok {
			closer.Close()
		}
		t.Fatal("invalid HTTP service unexpectedly assembled")
	}
	m, err := auth.New(cfg.Auth, auth.Options{})
	if err != nil {
		t.Fatalf("failed construction leaked authentication lock: %v", err)
	}
	m.Close()
}
