package httpapi_test

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
	"testing"
	"time"

	"shiyi/internal/answer"
	"shiyi/internal/api"
	"shiyi/internal/auth"
	"shiyi/internal/classify"
	"shiyi/internal/config"
	"shiyi/internal/httpapi"
	"shiyi/internal/llm"
	"shiyi/internal/search"
	"shiyi/internal/store"
)

// recordTime is the fixed record time the fake store hands back, so the
// expected wire form is a literal and not something the test recomputes.
var recordTime = time.Date(2026, 11, 4, 15, 0, 0, 123456789, time.UTC)

const recordTimeText = "2026-11-04T15:00:00.123456789Z"

// fakeJudge is a judge that always returns the same verdict.
type fakeJudge struct {
	kind    classify.Kind
	err     error
	calls   int
	content string
	// wait, when set, holds the judge until the context is done. It is
	// used to exercise the request deadline.
	wait bool
}

func (f *fakeJudge) Classify(ctx context.Context, content string) (classify.Kind, error) {
	f.calls++
	f.content = content
	if f.wait {
		<-ctx.Done()
		return "", ctx.Err()
	}
	if f.err != nil {
		return "", f.err
	}
	return f.kind, nil
}

// fakeStore records whether it was asked to write anything.
type fakeStore struct {
	record      store.Record
	err         error
	appendCalls int
	appended    []string
}

func (f *fakeStore) Append(_ context.Context, content string) (store.Record, error) {
	f.appendCalls++
	f.appended = append(f.appended, content)
	if f.err != nil {
		return store.Record{}, f.err
	}
	return f.record, nil
}

func (f *fakeStore) Records(_ context.Context) ([]store.Record, error) {
	return nil, nil
}

// fakeAnswerer is an answerer that always returns the same answer.
type fakeAnswerer struct {
	result answer.Result
	err    error
	calls  int
	query  string
}

func (f *fakeAnswerer) Answer(_ context.Context, query string) (answer.Result, error) {
	f.calls++
	f.query = query
	if f.err != nil {
		return answer.Result{}, f.err
	}
	return f.result, nil
}

// harness wires a server with fake dependencies behind a real HTTP server.
type harness struct {
	server   *httptest.Server
	judge    *fakeJudge
	store    *fakeStore
	answerer *fakeAnswerer
	logs     *bytes.Buffer
}

func newHarness(t *testing.T, configure func(*config.Config)) *harness {
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

	logs := &bytes.Buffer{}
	h := &harness{
		judge:    &fakeJudge{kind: classify.KindRecord},
		store:    &fakeStore{record: store.Record{Version: 1, Content: "被记录的事实", Time: store.NewTime(recordTime)}},
		logs:     logs,
		answerer: &fakeAnswerer{},
	}

	srv, err := httpapi.New(httpapi.Deps{
		Config:   cfg,
		Auth:     developmentAuth(t),
		Judge:    h.judge,
		Store:    h.store,
		Answerer: h.answerer,
		Logger:   slog.New(slog.NewTextHandler(logs, nil)),
	})
	if err != nil {
		t.Fatalf("httpapi.New: %v", err)
	}

	t.Cleanup(func() { _ = srv.Close() })
	h.server = httptest.NewServer(srv.Handler())
	t.Cleanup(h.server.Close)
	return h
}

// request sends a request with the given method and body and returns the
// response and its raw bytes.
func (h *harness) request(t *testing.T, method, path, body string, headers map[string]string) (*http.Response, []byte) {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, h.server.URL+path, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if method == http.MethodPost {
		authorizeDevelopment(t, h.server, req)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := h.server.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, raw
}

// post sends a request body and returns the response and its raw bytes.
func (h *harness) post(t *testing.T, body string, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	return h.request(t, http.MethodPost, "/messages", body, headers)
}

// get sends a GET request and returns the response and its raw bytes.
func (h *harness) get(t *testing.T, path string, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	return h.request(t, http.MethodGet, path, "", headers)
}

// decodeError reads an error response and checks its shape.
func decodeError(t *testing.T, raw []byte) api.ErrorResponse {
	t.Helper()

	var resp api.ErrorResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode error body %q: %v", raw, err)
	}
	if resp.Error.Code == "" {
		t.Errorf("error body %q has no code", raw)
	}
	if resp.Error.Message == "" {
		t.Errorf("error body %q has no message", raw)
	}
	return resp
}

func TestHealthzReportsOK(t *testing.T) {
	t.Parallel()

	h := newHarness(t, nil)

	resp, raw := h.get(t, "/healthz", nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %q)", resp.StatusCode, http.StatusOK, raw)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/json; charset=utf-8", got)
	}

	var payload struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode healthz body %q: %v", raw, err)
	}
	if payload.Status != "ok" {
		t.Errorf("status = %q, want %q", payload.Status, "ok")
	}
}

func TestRecordPathStoresAndAnswers(t *testing.T) {
	t.Parallel()

	h := newHarness(t, nil)
	const content = "我在 2026 年 3 月搬到柏林"

	resp, raw := h.post(t, `{"content":"我在 2026 年 3 月搬到柏林"}`, nil)

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %q)", resp.StatusCode, http.StatusCreated, raw)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/json; charset=utf-8", got)
	}

	var payload api.RecordResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode record body %q: %v", raw, err)
	}
	if payload.Kind != api.KindRecord {
		t.Errorf("kind = %q, want %q", payload.Kind, api.KindRecord)
	}
	if payload.Version != 1 {
		t.Errorf("version = %d, want 1", payload.Version)
	}
	if payload.Time != recordTimeText {
		t.Errorf("time = %q, want %q", payload.Time, recordTimeText)
	}

	if h.store.appendCalls != 1 {
		t.Fatalf("store.Append calls = %d, want 1", h.store.appendCalls)
	}
	if h.store.appended[0] != content {
		t.Errorf("stored content = %q, want %q", h.store.appended[0], content)
	}
	if h.judge.calls != 1 {
		t.Errorf("judge calls = %d, want 1", h.judge.calls)
	}
	if h.judge.content != content {
		t.Errorf("judged content = %q, want %q", h.judge.content, content)
	}
}

func TestQueryPathAnswersWithoutWriting(t *testing.T) {
	t.Parallel()

	h := newHarness(t, nil)
	h.judge.kind = classify.KindQuery
	h.answerer.result = answer.Result{
		Answer: "你在 2026 年 3 月搬到了柏林。",
		Citations: []answer.Citation{
			{Time: recordTimeText, Content: "我在 2026 年 3 月搬到柏林"},
		},
		Status: answer.StatusOK,
	}

	resp, raw := h.post(t, `{"content":"我什么时候搬的柏林？"}`, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %q)", resp.StatusCode, http.StatusOK, raw)
	}

	var payload api.QueryResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode query body %q: %v", raw, err)
	}
	if payload.Kind != api.KindQuery {
		t.Errorf("kind = %q, want %q", payload.Kind, api.KindQuery)
	}
	if payload.Answer != "你在 2026 年 3 月搬到了柏林。" {
		t.Errorf("answer = %q, want the answer of the answerer", payload.Answer)
	}
	if payload.Status != api.StatusOK {
		t.Errorf("status = %q, want %q", payload.Status, api.StatusOK)
	}
	if len(payload.Citations) != 1 {
		t.Fatalf("citations = %d, want 1", len(payload.Citations))
	}
	if payload.Citations[0].Time != recordTimeText {
		t.Errorf("citation time = %q, want %q", payload.Citations[0].Time, recordTimeText)
	}
	if payload.Citations[0].Content != "我在 2026 年 3 月搬到柏林" {
		t.Errorf("citation content = %q, want the original text", payload.Citations[0].Content)
	}

	// A query is never written to a file.
	if h.store.appendCalls != 0 {
		t.Errorf("store.Append calls = %d, want 0: a query must not write anything", h.store.appendCalls)
	}
	if h.answerer.calls != 1 {
		t.Errorf("answerer calls = %d, want 1", h.answerer.calls)
	}
	if h.answerer.query != "我什么时候搬的柏林？" {
		t.Errorf("answered query = %q, want the submitted content", h.answerer.query)
	}
}

func TestUnrecognizedIsAnsweredWithoutWriting(t *testing.T) {
	t.Parallel()

	h := newHarness(t, nil)
	h.judge.kind = classify.KindUnrecognized

	resp, raw := h.post(t, `{"content":"今天天气不错"}`, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %q)", resp.StatusCode, http.StatusOK, raw)
	}

	var payload api.UnrecognizedResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode unrecognized body %q: %v", raw, err)
	}
	if payload.Kind != api.KindUnrecognized {
		t.Errorf("kind = %q, want %q", payload.Kind, api.KindUnrecognized)
	}

	if h.store.appendCalls != 0 {
		t.Errorf("store.Append calls = %d, want 0: an unrecognized input must not write anything", h.store.appendCalls)
	}
	if h.answerer.calls != 0 {
		t.Errorf("answerer calls = %d, want 0: an unrecognized input is not a query", h.answerer.calls)
	}
}

func TestInvalidRequestIsRefused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "empty content", body: `{"content":""}`},
		{name: "blank content", body: `{"content":"   \t\n "}`},
		{name: "missing content", body: `{}`},
		{name: "broken json", body: `{"content":`},
		{name: "not an object", body: `["hello"]`},
		{name: "not json at all", body: `hello`},
		{name: "empty body", body: ``},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, nil)

			resp, raw := h.post(t, tt.body, nil)

			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d (body %q)", resp.StatusCode, http.StatusBadRequest, raw)
			}
			if got := decodeError(t, raw).Error.Code; got != api.CodeInvalidRequest {
				t.Errorf("code = %q, want %q", got, api.CodeInvalidRequest)
			}
			if h.store.appendCalls != 0 {
				t.Errorf("store.Append calls = %d, want 0", h.store.appendCalls)
			}
			if h.judge.calls != 0 {
				t.Errorf("judge calls = %d, want 0: an invalid request is never judged", h.judge.calls)
			}
		})
	}
}

func TestBodyOverTheLimitIsRefused(t *testing.T) {
	t.Parallel()

	h := newHarness(t, func(cfg *config.Config) {
		cfg.MaxBodyBytes = 64
	})

	body := `{"content":"` + strings.Repeat("x", 200) + `"}`
	resp, raw := h.post(t, body, nil)

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d (body %q)", resp.StatusCode, http.StatusRequestEntityTooLarge, raw)
	}
	if got := decodeError(t, raw).Error.Code; got != api.CodeRequestTooLarge {
		t.Errorf("code = %q, want %q", got, api.CodeRequestTooLarge)
	}
	if h.judge.calls != 0 {
		t.Errorf("judge calls = %d, want 0: an oversized body is never judged", h.judge.calls)
	}
	if h.store.appendCalls != 0 {
		t.Errorf("store.Append calls = %d, want 0", h.store.appendCalls)
	}
}

// Bearer credentials never authorize the password-mode HTTP boundary.
func TestBearerNeverAuthorizes(t *testing.T) {
	h := newAuthHarness(t)
	b := h.browser()
	for _, value := range []string{"", "Basic secret", "Bearer secret"} {
		res, raw := b.request("POST", "/messages", `{"content":"private"}`, func(r *http.Request) { r.Header.Set("Authorization", value) })
		requireStatus(t, res, raw, 401)
	}
	res, raw := b.request("GET", "/healthz", "", nil)
	requireStatus(t, res, raw, 200)
	if h.judge.calls != 0 || h.memory.appendCalls != 0 {
		t.Fatal("rejected credentials reached business")
	}
}

func TestFailuresAreMappedToTheirCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		judgeErr  error
		storeErr  error
		answerErr error
		wantCode  string
		want      int
	}{
		{
			name:     "the model is unreachable while judging",
			judgeErr: llm.ErrUnavailable,
			wantCode: api.CodeModelUnavailable,
			want:     http.StatusServiceUnavailable,
		},
		{
			name:     "the model answers something unreadable while judging",
			judgeErr: llm.ErrInvalidResponse,
			wantCode: api.CodeModelInvalidResponse,
			want:     http.StatusBadGateway,
		},
		{
			name:      "the model is unreachable while answering",
			judgeErr:  nil,
			answerErr: llm.ErrUnavailable,
			wantCode:  api.CodeModelUnavailable,
			want:      http.StatusServiceUnavailable,
		},
		{
			name:      "the model answers something unreadable while answering",
			answerErr: llm.ErrInvalidResponse,
			wantCode:  api.CodeModelInvalidResponse,
			want:      http.StatusBadGateway,
		},
		{
			name:      "the model cites a record that was not found",
			answerErr: answer.ErrInvalidCitation,
			wantCode:  api.CodeInvalidCitation,
			want:      http.StatusBadGateway,
		},
		{
			name:     "the record cannot be written",
			storeErr: store.ErrStorageFailed,
			wantCode: api.CodeStorageFailed,
			want:     http.StatusInternalServerError,
		},
		{
			name:      "the records cannot be searched",
			answerErr: search.ErrSearchFailed,
			wantCode:  api.CodeSearchFailed,
			want:      http.StatusInternalServerError,
		},
		{
			name:      "something else goes wrong",
			answerErr: errors.New("something else"),
			wantCode:  api.CodeInternalError,
			want:      http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, nil)
			if tt.judgeErr != nil {
				h.judge.err = tt.judgeErr
			}
			if tt.storeErr != nil {
				h.judge.kind = classify.KindRecord
				h.store.err = tt.storeErr
			}
			if tt.answerErr != nil {
				h.judge.kind = classify.KindQuery
				h.answerer.err = tt.answerErr
			}

			resp, raw := h.post(t, `{"content":"一条输入"}`, nil)

			if resp.StatusCode != tt.want {
				t.Fatalf("status = %d, want %d (body %q)", resp.StatusCode, tt.want, raw)
			}
			if got := decodeError(t, raw).Error.Code; got != tt.wantCode {
				t.Errorf("code = %q, want %q", got, tt.wantCode)
			}
			// A failure never writes anything.
			if h.store.appendCalls > 1 {
				t.Errorf("store.Append calls = %d, want at most 1", h.store.appendCalls)
			}
		})
	}
}

func TestErrorMessagesLeakNothing(t *testing.T) {
	t.Parallel()

	const secret = "sk-super-secret-key"
	const content = "我的银行卡号是 6222020200000000"

	h := newHarness(t, nil)
	h.judge.err = llm.ErrUnavailable
	// The error of the model carries everything a provider might send back.
	h.judge.err = errors.Join(llm.ErrUnavailable, errors.New("provider said: "+secret+" "+content))

	resp, raw := h.post(t, `{"content":"`+content+`"}`, nil)

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d (body %q)", resp.StatusCode, http.StatusServiceUnavailable, raw)
	}

	body := string(raw)
	if strings.Contains(body, secret) {
		t.Errorf("the error body leaks the API key: %s", body)
	}
	if strings.Contains(body, content) {
		t.Errorf("the error body leaks the submitted text: %s", body)
	}

	// The log must not hold the text either.
	if logs := h.logs.String(); strings.Contains(logs, content) {
		t.Errorf("the log leaks the submitted text: %s", logs)
	}
	if logs := h.logs.String(); strings.Contains(logs, secret) {
		t.Errorf("the log leaks the API key: %s", logs)
	}
}

func TestUnknownPathsAnswerWithTheErrorShape(t *testing.T) {
	t.Parallel()

	h := newHarness(t, nil)

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "unknown path", method: http.MethodGet, path: "/nope"},
		{name: "messages with the wrong method", method: http.MethodGet, path: "/messages"},
		{name: "healthz with the wrong method", method: http.MethodPost, path: "/healthz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resp, raw := h.request(t, tt.method, tt.path, "", nil)

			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("status = %d, want %d (body %q)", resp.StatusCode, http.StatusNotFound, raw)
			}
			if got := resp.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
				t.Errorf("Content-Type = %q, want application/json; charset=utf-8", got)
			}
			decodeError(t, raw)
		})
	}
}

func TestRequestDeadlineEndsTheRequest(t *testing.T) {
	t.Parallel()

	h := newHarness(t, func(cfg *config.Config) {
		cfg.Budget.RequestTimeout = 20 * time.Millisecond
	})
	h.judge.wait = true

	resp, raw := h.post(t, `{"content":"一条输入"}`, nil)

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d (body %q)", resp.StatusCode, http.StatusServiceUnavailable, raw)
	}
	if got := decodeError(t, raw).Error.Code; got != api.CodeModelUnavailable {
		t.Errorf("code = %q, want %q", got, api.CodeModelUnavailable)
	}
	if h.store.appendCalls != 0 {
		t.Errorf("store.Append calls = %d, want 0", h.store.appendCalls)
	}
}

func TestNewRefusesAnUnusableConfiguration(t *testing.T) {
	t.Parallel()

	base := config.Config{
		DataDir:    t.TempDir(),
		Classifier: config.ModelConfig{BaseURL: "http://model.invalid", Model: "test-model"},
		Answer:     config.ModelConfig{BaseURL: "http://model.invalid", Model: "test-model"},
	}

	tests := []struct {
		name    string
		mutate  func(*config.Config)
		wantErr string
	}{
		{
			name:    "no data directory",
			mutate:  func(cfg *config.Config) { cfg.DataDir = "" },
			wantErr: "data directory",
		},
		{
			name:    "no model endpoint",
			mutate:  func(cfg *config.Config) { cfg.Answer.BaseURL = "" },
			wantErr: "model endpoint",
		},
		{
			name:    "no model name",
			mutate:  func(cfg *config.Config) { cfg.Answer.Model = "" },
			wantErr: "model is configured",
		},
		{
			name: "no classifier and no answer model",
			mutate: func(cfg *config.Config) {
				cfg.Answer.BaseURL = ""
				cfg.Answer.Model = ""
			},
			wantErr: "model endpoint",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := base
			cfg.DataDir = t.TempDir()
			tt.mutate(&cfg)

			_, err := httpapi.New(httpapi.Deps{
				Config:   cfg,
				Auth:     developmentAuth(t),
				Judge:    &fakeJudge{kind: classify.KindRecord},
				Store:    &fakeStore{},
				Answerer: &fakeAnswerer{},
				Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
			})
			if err == nil {
				t.Fatalf("New returned no error, want one mentioning %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

func TestNewNeedsItsCollaborators(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		DataDir:    t.TempDir(),
		Classifier: config.ModelConfig{BaseURL: "http://model.invalid", Model: "test-model"},
		Answer:     config.ModelConfig{BaseURL: "http://model.invalid", Model: "test-model"},
	}

	tests := []struct {
		name string
		deps httpapi.Deps
	}{
		{
			name: "no judge",
			deps: httpapi.Deps{Config: cfg, Store: &fakeStore{}, Answerer: &fakeAnswerer{}},
		},
		{
			name: "no store",
			deps: httpapi.Deps{Config: cfg, Judge: &fakeJudge{}, Answerer: &fakeAnswerer{}},
		},
		{
			name: "no answerer",
			deps: httpapi.Deps{Config: cfg, Judge: &fakeJudge{}, Store: &fakeStore{}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := httpapi.New(tt.deps); err == nil {
				t.Fatalf("New returned no error, want one")
			}
		})
	}
}

// Existing business regression tests use the real explicit development module;
// the browser still obtains a bound challenge over HTTP before every POST.
func developmentAuth(t *testing.T) auth.Module {
	t.Helper()
	m, err := auth.New(config.AuthConfig{DevAnonymous: true, PublicOrigin: "http://memory.test"}, auth.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}
func authorizeDevelopment(t *testing.T, s *httptest.Server, r *http.Request) {
	t.Helper()
	res, err := s.Client().Get(s.URL + api.PathAuthSession)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var session api.SessionResponse
	if err := json.NewDecoder(res.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	for _, c := range res.Cookies() {
		r.AddCookie(c)
	}
	r.Header.Set("Origin", "http://memory.test")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set(api.HeaderCSRFToken, session.CSRFToken)
}
func developmentPost(t *testing.T, s *httptest.Server, body io.Reader) (*http.Response, error) {
	t.Helper()
	r, err := http.NewRequest("POST", s.URL+"/messages", body)
	if err != nil {
		return nil, err
	}
	authorizeDevelopment(t, s, r)
	return s.Client().Do(r)
}
