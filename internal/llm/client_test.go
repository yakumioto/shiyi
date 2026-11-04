package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// capture records what the fake provider received.
type capture struct {
	mu     sync.Mutex
	method string
	path   string
	header http.Header
	body   map[string]any
}

func (c *capture) get() (string, string, http.Header, map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.method, c.path, c.header, c.body
}

// serve starts a provider stub that answers with the given raw body and
// records the request it received.
func serve(t *testing.T, status int, body string) (*httptest.Server, *capture) {
	t.Helper()
	cap := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		decoded := map[string]any{}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Errorf("decode request body %q: %v", raw, err)
		}
		cap.mu.Lock()
		cap.method = r.Method
		cap.path = r.URL.Path
		cap.header = r.Header.Clone()
		cap.body = decoded
		cap.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, cap
}

const textAnswer = `{"id":"chatcmpl-1","model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`

func TestHTTPClientMapsFailuresToSentinelErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{name: "bad request", status: http.StatusBadRequest, body: `{"error":{"message":"nope"}}`, wantErr: ErrUnavailable},
		{name: "unauthorized", status: http.StatusUnauthorized, body: `{"error":{"message":"nope"}}`, wantErr: ErrUnavailable},
		{name: "server error", status: http.StatusInternalServerError, body: `{"error":{"message":"nope"}}`, wantErr: ErrUnavailable},
		{name: "bad json", status: http.StatusOK, body: `not json at all`, wantErr: ErrInvalidResponse},
		{name: "empty choices", status: http.StatusOK, body: `{"id":"x","choices":[]}`, wantErr: ErrInvalidResponse},
		{name: "missing choices", status: http.StatusOK, body: `{"id":"x"}`, wantErr: ErrInvalidResponse},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := serve(t, tt.status, tt.body)

			client := New(srv.URL, "test-model", "sk-test-key", 5*time.Second, 0)

			_, err := client.Complete(context.Background(), Request{
				Messages: []Message{{Role: RoleUser, Content: "hi"}},
			})
			if err == nil {
				t.Fatalf("Complete: want error %v, got nil", tt.wantErr)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Complete error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestHTTPClientTimeoutIsUnavailable(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})

	client := New(srv.URL, "test-model", "sk-test-key", 50*time.Millisecond, 0)

	_, err := client.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("Complete: want timeout error, got nil")
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("Complete error = %v, want %v", err, ErrUnavailable)
	}
}

func TestHTTPClientContextCancelIsUnavailable(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})

	client := New(srv.URL, "test-model", "sk-test-key", time.Minute, 0)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := client.Complete(ctx, Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("Complete: want cancellation error, got nil")
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("Complete error = %v, want %v", err, ErrUnavailable)
	}
	// The cancellation stays detectable underneath the sentinel.
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Complete error = %v, want it to wrap %v", err, context.Canceled)
	}
}

func TestHTTPClientNeverLeaksKeyOrProviderBody(t *testing.T) {
	const key = "sk-super-secret-key"
	const providerBody = `{"error":{"message":"internal stack trace with sk-super-secret-key"}}`

	srv, _ := serve(t, http.StatusInternalServerError, providerBody)

	client := New(srv.URL, "test-model", key, 5*time.Second, 0)

	_, err := client.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("Complete: want error, got nil")
	}
	if strings.Contains(err.Error(), key) {
		t.Errorf("error message leaks the API key: %v", err)
	}
	if strings.Contains(err.Error(), "stack trace") {
		t.Errorf("error message leaks the provider body: %v", err)
	}
}

func TestHTTPClientLogsStatusWithoutKeyOrBody(t *testing.T) {
	const key = "sk-super-secret-key"

	srv, _ := serve(t, http.StatusUnauthorized, `{"error":{"message":"boom sk-super-secret-key"}}`)

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	client := New(srv.URL, "test-model", key, 5*time.Second, 0).WithLogger(logger)

	_, err := client.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("Complete: want error, got nil")
	}
	if !strings.Contains(buf.String(), "401") {
		t.Errorf("log should record the status code, got: %s", buf.String())
	}
	if strings.Contains(buf.String(), key) {
		t.Errorf("log leaks the API key: %s", buf.String())
	}
	if strings.Contains(buf.String(), "boom") {
		t.Errorf("log leaks the provider body: %s", buf.String())
	}
}

func TestHTTPClientJoinsBaseURLWithoutDoubleSlash(t *testing.T) {
	tests := []struct {
		name    string
		baseURL func(srvURL string) string
		want    string
	}{
		{name: "plain", baseURL: func(u string) string { return u }, want: "/chat/completions"},
		{name: "trailing slash", baseURL: func(u string) string { return u + "/" }, want: "/chat/completions"},
		{name: "several trailing slashes", baseURL: func(u string) string { return u + "///" }, want: "/chat/completions"},
		{name: "path prefix", baseURL: func(u string) string { return u + "/v1" }, want: "/v1/chat/completions"},
		{name: "path prefix with slash", baseURL: func(u string) string { return u + "/v1/" }, want: "/v1/chat/completions"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, cap := serve(t, http.StatusOK, textAnswer)

			client := New(tt.baseURL(srv.URL), "test-model", "sk-test-key", 5*time.Second, 0)

			if _, err := client.Complete(context.Background(), Request{
				Messages: []Message{{Role: RoleUser, Content: "hi"}},
			}); err != nil {
				t.Fatalf("Complete: %v", err)
			}

			_, path, _, _ := cap.get()
			if path != tt.want {
				t.Errorf("path = %q, want %q", path, tt.want)
			}
		})
	}
}

func TestHTTPClientSendsToolsAndToolChoice(t *testing.T) {
	srv, cap := serve(t, http.StatusOK, textAnswer)

	client := New(srv.URL, "test-model", "sk-test-key", 5*time.Second, 0)

	toolChoice := &ToolChoice{Type: "function"}
	toolChoice.Function = &struct {
		Name string `json:"name"`
	}{Name: "search_memory"}

	_, err := client.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools: []Tool{{
			Type: "function",
			Function: ToolFunction{
				Name:        "search_memory",
				Description: "搜索记忆",
				Parameters:  map[string]any{"type": "object"},
			},
		}},
		ToolChoice: toolChoice,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	_, _, _, body := cap.get()

	tools, ok := body["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want one tool", body["tools"])
	}
	tool, ok := tools[0].(map[string]any)
	if !ok {
		t.Fatalf("tools[0] = %#v, want an object", tools[0])
	}
	if got := tool["type"]; got != "function" {
		t.Errorf("tools[0].type = %v, want %q", got, "function")
	}
	fn, ok := tool["function"].(map[string]any)
	if !ok {
		t.Fatalf("tools[0].function = %#v, want an object", tool["function"])
	}
	if got := fn["name"]; got != "search_memory" {
		t.Errorf("tools[0].function.name = %v, want %q", got, "search_memory")
	}

	choice, ok := body["tool_choice"].(map[string]any)
	if !ok {
		t.Fatalf("tool_choice = %#v, want an object", body["tool_choice"])
	}
	if got := choice["type"]; got != "function" {
		t.Errorf("tool_choice.type = %v, want %q", got, "function")
	}
}

func TestHTTPClientParsesToolCalls(t *testing.T) {
	const body = `{"id":"chatcmpl-2","model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"search_memory","arguments":"{\"keywords\":[\"咖啡\"],\"limit\":5}"}}]},"finish_reason":"tool_calls"}]}`

	srv, _ := serve(t, http.StatusOK, body)

	client := New(srv.URL, "test-model", "sk-test-key", 5*time.Second, 0)

	resp, err := client.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	choice, err := resp.Choice()
	if err != nil {
		t.Fatalf("Choice: %v", err)
	}
	if len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("tool_calls = %#v, want exactly one", choice.Message.ToolCalls)
	}
	tc := choice.Message.ToolCalls[0]
	if tc.ID != "call_1" {
		t.Errorf("tool_calls[0].id = %q, want %q", tc.ID, "call_1")
	}
	if tc.Type != "function" {
		t.Errorf("tool_calls[0].type = %q, want %q", tc.Type, "function")
	}
	if tc.Function.Name != "search_memory" {
		t.Errorf("tool_calls[0].function.name = %q, want %q", tc.Function.Name, "search_memory")
	}
	// Arguments stay the raw JSON string the provider sent; the caller
	// decides how to decode them.
	if want := `{"keywords":["咖啡"],"limit":5}`; tc.Function.Arguments != want {
		t.Errorf("tool_calls[0].function.arguments = %q, want %q", tc.Function.Arguments, want)
	}
}

func TestHTTPClientSendsResponseFormatWhenRequested(t *testing.T) {
	srv, cap := serve(t, http.StatusOK, textAnswer)

	client := New(srv.URL, "test-model", "sk-test-key", 5*time.Second, 0)

	_, err := client.Complete(context.Background(), Request{
		Messages:       []Message{{Role: RoleUser, Content: "hi"}},
		ResponseFormat: &ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	_, _, _, body := cap.get()

	format, ok := body["response_format"].(map[string]any)
	if !ok {
		t.Fatalf("response_format = %#v, want an object", body["response_format"])
	}
	if got := format["type"]; got != "json_object" {
		t.Errorf("response_format.type = %v, want %q", got, "json_object")
	}
}

func TestHTTPClientOmitsResponseFormatWhenUnset(t *testing.T) {
	srv, cap := serve(t, http.StatusOK, textAnswer)

	client := New(srv.URL, "test-model", "sk-test-key", 5*time.Second, 0)

	if _, err := client.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	_, _, _, body := cap.get()

	if _, present := body["response_format"]; present {
		t.Errorf("response_format should be omitted, got %#v", body["response_format"])
	}
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHTTPClientUsesInjectedHTTPClient(t *testing.T) {
	srv, _ := serve(t, http.StatusOK, textAnswer)

	used := false
	client := New(srv.URL, "test-model", "sk-test-key", 5*time.Second, 0).
		WithHTTPClient(&http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				used = true
				return http.DefaultTransport.RoundTrip(r)
			}),
		})

	if _, err := client.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if !used {
		t.Error("the injected HTTP client was not used")
	}
}

func TestHTTPClientKeepsRequestModelWhenSet(t *testing.T) {
	srv, cap := serve(t, http.StatusOK, textAnswer)

	client := New(srv.URL, "configured-model", "sk-test-key", 5*time.Second, 0)

	if _, err := client.Complete(context.Background(), Request{
		Model:    "request-model",
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	_, _, _, body := cap.get()
	if got := body["model"]; got != "request-model" {
		t.Errorf("model = %v, want %q", got, "request-model")
	}
}

func TestHTTPClientReportsUnreachableEndpoint(t *testing.T) {
	// A server that is closed right away leaves nothing to connect to.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	client := New(url, "test-model", "sk-test-key", 2*time.Second, 0)

	_, err := client.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("Complete: want error, got nil")
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("Complete error = %v, want %v", err, ErrUnavailable)
	}
}

func TestHTTPClientReportsUnbuildableURLAsUnavailable(t *testing.T) {
	// A control character in the base URL makes the request impossible to
	// build at all: the model cannot be reached.
	client := New("http://example.com/\x7f", "test-model", "sk-test-key", time.Second, 0)

	_, err := client.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("Complete: want error, got nil")
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("Complete error = %v, want %v", err, ErrUnavailable)
	}
}

func TestHTTPClientRejectsOversizedResponse(t *testing.T) {
	// A provider that streams an endless body must not be able to exhaust
	// the process memory. The body is padded with a valid JSON string so
	// that only the size limit can reject it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"`)
		chunk := strings.Repeat("a", 64*1024)
		for i := 0; i < 512; i++ {
			if _, err := io.WriteString(w, chunk); err != nil {
				return
			}
		}
		_, _ = io.WriteString(w, `"}}]}`)
	}))
	t.Cleanup(srv.Close)

	client := New(srv.URL, "test-model", "sk-test-key", 10*time.Second, 0)

	_, err := client.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("Complete: want error, got nil")
	}
	if !errors.Is(err, ErrInvalidResponse) {
		t.Errorf("Complete error = %v, want %v", err, ErrInvalidResponse)
	}
}

func TestHTTPClientParsesToolCallWithNullContent(t *testing.T) {
	// Providers answer a tool call with a null content field; that must not
	// be read as an invalid response.
	const body = `{"id":"chatcmpl-3","choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"search_memory","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`

	srv, _ := serve(t, http.StatusOK, body)

	client := New(srv.URL, "test-model", "sk-test-key", 5*time.Second, 0)

	resp, err := client.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	choice, err := resp.Choice()
	if err != nil {
		t.Fatalf("Choice: %v", err)
	}
	if len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("tool_calls = %#v, want exactly one", choice.Message.ToolCalls)
	}
	if choice.Message.Content != "" {
		t.Errorf("content = %q, want empty", choice.Message.Content)
	}
}

func TestHTTPClientSendsOpenAICompatibleRequest(t *testing.T) {
	srv, cap := serve(t, http.StatusOK, textAnswer)

	client := New(srv.URL, "test-model", "sk-test-key", 5*time.Second, 0.25)

	resp, err := client.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	method, path, header, body := cap.get()

	if method != http.MethodPost {
		t.Errorf("method = %q, want %q", method, http.MethodPost)
	}
	if path != "/chat/completions" {
		t.Errorf("path = %q, want %q", path, "/chat/completions")
	}
	if got, want := header.Get("Authorization"), "Bearer sk-test-key"; got != want {
		t.Errorf("Authorization = %q, want %q", got, want)
	}
	if got, want := header.Get("Content-Type"), "application/json"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}
	if got := body["model"]; got != "test-model" {
		t.Errorf("model = %v, want %q", got, "test-model")
	}
	if got := body["temperature"]; got != 0.25 {
		t.Errorf("temperature = %v, want 0.25", got)
	}

	messages, ok := body["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("messages = %#v, want one message", body["messages"])
	}
	msg, ok := messages[0].(map[string]any)
	if !ok {
		t.Fatalf("messages[0] = %#v, want an object", messages[0])
	}
	if got := msg["role"]; got != "user" {
		t.Errorf("messages[0].role = %v, want %q", got, "user")
	}
	if got := msg["content"]; got != "hi" {
		t.Errorf("messages[0].content = %v, want %q", got, "hi")
	}

	choice, err := resp.Choice()
	if err != nil {
		t.Fatalf("Choice: %v", err)
	}
	if got := choice.Message.Content; got != "hello" {
		t.Errorf("content = %q, want %q", got, "hello")
	}
}
