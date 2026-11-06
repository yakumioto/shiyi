// Command fixture runs only the browser regression's isolated HTTPS service.
// Control uses stdin/stdout, never an extra production HTTP route.
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/bcrypt"

	"shiyi/internal/answer"
	"shiyi/internal/auth"
	"shiyi/internal/classify"
	"shiyi/internal/config"
	"shiyi/internal/httpapi"
	"shiyi/internal/llm"
	"shiyi/internal/search"
	"shiyi/internal/store"
	"shiyi/internal/web"
)

const password = "browser-test-only-password"

type model struct {
	mu      sync.Mutex
	calls   int
	kind    string
	fail    bool
	answers []*llm.Response
}

func (m *model) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if req.Model == "judge" {
		m.calls++
	}
	if m.fail {
		return nil, llm.ErrUnavailable
	}
	if req.Model == "answer" {
		if len(m.answers) == 0 {
			return nil, llm.ErrUnavailable
		}
		response := m.answers[0]
		m.answers = m.answers[1:]
		return response, nil
	}
	body, err := json.Marshal(map[string]string{"kind": m.kind})
	if err != nil {
		return nil, err
	}
	return llm.TextResponse(string(body)), nil
}

func run() error {
	// The driver owns this directory and removes it only after the process exits.
	root := os.Getenv("SHIYI_BROWSER_TEST_DIR")
	if !filepath.IsAbs(root) {
		return fmt.Errorf("SHIYI_BROWSER_TEST_DIR must be an isolated absolute directory")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	address := listener.Addr().String()
	origin := "https://" + address
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	if err != nil {
		return err
	}
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	authConfig := config.AuthConfig{
		PasswordHash: string(hash), StateDir: filepath.Join(root, "auth"), PublicOrigin: origin,
		DevAnonymous: os.Getenv("SHIYI_BROWSER_DEV_ANONYMOUS") == "1",
	}
	m := &model{kind: "record"}
	cfg := config.Config{
		DataDir:    filepath.Join(root, "memory"),
		Classifier: config.ModelConfig{BaseURL: "http://model.invalid", Model: "test"},
		Answer:     config.ModelConfig{BaseURL: "http://model.invalid", Model: "test"},
	}
	var a auth.Module
	var srv *httpapi.Server
	var tls *httptest.Server
	startService := func(listener net.Listener) error {
		memory, err := store.New(cfg.DataDir, "monthly")
		if err != nil {
			return err
		}
		a, err = auth.New(authConfig, auth.Options{Now: func() time.Time { return time.Unix(0, clock.Load()) }})
		if err != nil {
			return err
		}
		srv, err = httpapi.New(httpapi.Deps{
			Config: cfg, Auth: a, Judge: classify.New(m, "judge", 0), Store: memory,
			Answerer: answer.New(m, search.New(cfg.DataDir), "answer", 0, answer.Budget{}),
			Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
		if err != nil {
			a.Close()
			return err
		}
		tls = httptest.NewUnstartedServer(srv.HandlerWithWeb(web.Handler()))
		tls.Listener.Close()
		tls.Listener = listener
		tls.StartTLS()
		return nil
	}
	stopService := func() error {
		if tls != nil {
			tls.Close() // stop HTTP before releasing auth's single-writer lock
		}
		if srv != nil {
			return srv.Close()
		}
		return nil
	}
	if err := startService(listener); err != nil {
		return err
	}
	defer func() { _ = stopService() }()
	spki := sha256.Sum256(tls.Certificate().RawSubjectPublicKeyInfo)
	out := json.NewEncoder(os.Stdout)
	if err := out.Encode(map[string]any{"origin": origin, "spki": base64.StdEncoding.EncodeToString(spki[:])}); err != nil {
		return err
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var cmd struct {
			ID      int             `json:"id"`
			Op      string          `json:"op"`
			Seconds int64           `json:"seconds"`
			Kind    string          `json:"kind"`
			Fail    bool            `json:"fail"`
			Answers []*llm.Response `json:"answers"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &cmd); err != nil {
			return err
		}
		switch cmd.Op {
		case "advance":
			clock.Add(cmd.Seconds * int64(time.Second))
		case "model":
			m.mu.Lock()
			if cmd.Kind != "" {
				m.kind = cmd.Kind
			}
			m.fail = cmd.Fail
			m.answers = cmd.Answers
			m.mu.Unlock()
		case "restart-service":
			if err := stopService(); err != nil {
				return err
			}
			listener, err := net.Listen("tcp", address)
			if err != nil {
				return err
			}
			// Reconstruct from the same directories/hash via public constructors.
			// Never copy, decode or manufacture persisted authentication state.
			if err := startService(listener); err != nil {
				listener.Close()
				return err
			}
		case "close-auth":
			if err := a.Close(); err != nil {
				return err
			}
		case "stats":
		default:
			return fmt.Errorf("unknown fixture command")
		}
		m.mu.Lock()
		calls := m.calls
		m.mu.Unlock()
		if err := out.Encode(map[string]any{"id": cmd.ID, "calls": calls}); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
