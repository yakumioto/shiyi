package httpapi_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"shiyi/internal/answer"
	"shiyi/internal/api"
	"shiyi/internal/classify"
	"shiyi/internal/config"
	"shiyi/internal/httpapi"
	"shiyi/internal/store"
)

// newServer builds a server with fake dependencies and returns it together
// with its fakes, without starting an HTTP server.
func newServer(t *testing.T, configure func(*config.Config)) (*httpapi.Server, *fakeJudge, *fakeStore, *fakeAnswerer) {
	t.Helper()

	cfg := config.Config{
		DataDir:      t.TempDir(),
		MaxBodyBytes: 1 << 20,
		Classifier:   config.ModelConfig{BaseURL: "http://model.invalid", Model: "test-model"},
		Answer:       config.ModelConfig{BaseURL: "http://model.invalid", Model: "test-model"},
		Budget:       config.BudgetConfig{RequestTimeout: 60 * time.Second},
	}
	if configure != nil {
		configure(&cfg)
	}

	judge := &fakeJudge{kind: classify.KindRecord}
	store := &fakeStore{record: store.Record{Version: 1, Content: "被记录的事实", Time: store.NewTime(recordTime)}}
	answerer := &fakeAnswerer{}

	srv, err := httpapi.New(httpapi.Deps{
		Config:   cfg,
		Auth:     developmentAuth(t),
		Judge:    judge,
		Store:    store,
		Answerer: answerer,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("httpapi.New: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return srv, judge, store, answerer
}

// TestWebIsMountedAtTheRoot checks that the web page can be mounted and
// that it takes the place of the root path.
func TestWebIsMountedAtTheRoot(t *testing.T) {
	t.Parallel()

	srv, _, _, _ := newServer(t, nil)

	web := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<html>shiyi</html>")
	})

	srv.HandlerWithWeb(web)

	server := httptest.NewServer(srv.Handler())
	defer server.Close()

	resp, err := server.Client().Get(server.URL + "/")
	if err != nil {
		t.Fatalf("get /: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %q)", resp.StatusCode, http.StatusOK, body)
	}
	if !strings.Contains(string(body), "shiyi") {
		t.Errorf("body = %q, want the page of the web handler", body)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want the one of the web handler", got)
	}
}

// TestTheRootIsEmptyWithoutAWebPage checks that the server answers GET /
// with the error shape until a page is mounted.
func TestTheRootIsEmptyWithoutAWebPage(t *testing.T) {
	t.Parallel()

	srv, _, _, _ := newServer(t, nil)

	server := httptest.NewServer(srv.Handler())
	defer server.Close()

	resp, err := server.Client().Get(server.URL + "/")
	if err != nil {
		t.Fatalf("get /: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (body %q)", resp.StatusCode, http.StatusNotFound, body)
	}

	var payload api.ErrorResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode error body %q: %v", body, err)
	}
	if payload.Error.Code != api.CodeInvalidRequest {
		t.Errorf("code = %q, want %q", payload.Error.Code, api.CodeInvalidRequest)
	}
}

// TestTheOriginalTextIsKept checks that the text that reaches the store is
// the text the user sent, spaces included.
func TestTheOriginalTextIsKept(t *testing.T) {
	t.Parallel()

	const content = "  前后都有空格的一条记录  "

	h := newHarness(t, nil)

	resp, raw := h.post(t, `{"content":"  前后都有空格的一条记录  "}`, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %q)", resp.StatusCode, http.StatusCreated, raw)
	}

	if h.judge.content != content {
		t.Errorf("judged content = %q, want %q", h.judge.content, content)
	}
	if h.store.appendCalls != 1 {
		t.Fatalf("store.Append calls = %d, want 1", h.store.appendCalls)
	}
	if h.store.appended[0] != content {
		t.Errorf("stored content = %q, want %q", h.store.appended[0], content)
	}
}

// TestQueryStatusesArePassedOn checks that every status of an answer
// reaches the client.
func TestQueryStatusesArePassedOn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status answer.Status
		want   string
	}{
		{name: "ok", status: answer.StatusOK, want: api.StatusOK},
		{name: "budget exhausted", status: answer.StatusBudgetExhausted, want: api.StatusBudgetExhausted},
		{name: "no match", status: answer.StatusNoMatch, want: api.StatusNoMatch},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, nil)
			h.judge.kind = classify.KindQuery
			h.answerer.result = answer.Result{
				Answer: answer.NoMatchAnswer,
				Status: tt.status,
			}

			resp, raw := h.post(t, `{"content":"我的记忆里有咖啡吗？"}`, nil)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want %d (body %q)", resp.StatusCode, http.StatusOK, raw)
			}

			var payload api.QueryResponse
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatalf("decode query body %q: %v", raw, err)
			}
			if payload.Status != tt.want {
				t.Errorf("status = %q, want %q", payload.Status, tt.want)
			}
			// An answer without evidence carries no citation.
			if len(payload.Citations) != 0 {
				t.Errorf("citations = %d, want 0", len(payload.Citations))
			}
		})
	}
}

// TestAnEmptyCitationListIsAnEmptyList checks that the citations of an
// answer without evidence are an empty list and not null, so that a client
// can walk them without a nil check.
func TestAnEmptyCitationListIsAnEmptyList(t *testing.T) {
	t.Parallel()

	h := newHarness(t, nil)
	h.judge.kind = classify.KindQuery
	h.answerer.result = answer.Result{Answer: answer.NoMatchAnswer, Status: answer.StatusNoMatch}

	_, raw := h.post(t, `{"content":"我的记忆里有咖啡吗？"}`, nil)

	if !strings.Contains(string(raw), `"citations":[]`) {
		t.Errorf("body = %q, want an empty citation list", raw)
	}
}

// TestWebAssetsReachTheWebHandler checks that the web interface owns its whole
// path space: the page is served at the root and its assets alongside it, not
// swallowed by the API's 404.
func TestWebAssetsReachTheWebHandler(t *testing.T) {
	t.Parallel()

	srv, _, _, _ := newServer(t, nil)

	asset := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		_, _ = io.WriteString(w, "// "+r.URL.Path)
	})
	srv.HandlerWithWeb(asset)

	server := httptest.NewServer(srv.Handler())
	defer server.Close()

	for _, assetPath := range []string{"/app.js", "/style.css", "/nested/page"} {
		resp, err := server.Client().Get(server.URL + assetPath)
		if err != nil {
			t.Fatalf("get %s: %v", assetPath, err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("read body of %s: %v", assetPath, err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s = %d, want 200; the web handler owns the whole path space", assetPath, resp.StatusCode)
		}
		if want := "// " + assetPath; string(body) != want {
			t.Errorf("GET %s body = %q, want %q", assetPath, body, want)
		}
	}
}
