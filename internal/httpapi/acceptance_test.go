package httpapi_test

// This file walks the acceptance checklist from the design appendix, one test
// per line of the checklist. It uses the HTTP entry point, a real store, real
// ripgrep and a scripted model, so each test observes the same surface a user
// would.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shiyi/internal/answer"
	"shiyi/internal/classify"
	"shiyi/internal/config"
	"shiyi/internal/httpapi"
	"shiyi/internal/llm"
	"shiyi/internal/search"
	"shiyi/internal/store"
)

// axHarness is a running service on a temporary data directory.
type axHarness struct {
	t        *testing.T
	server   *httptest.Server
	dir      string
	memory   *store.FS
	judge    *llm.FakeClient
	answerer *llm.FakeClient
}

// newAxHarness starts the service with scripted model turns.
func newAxHarness(t *testing.T, judgeTurns, answerTurns []*llm.Response) *axHarness {
	t.Helper()

	dir := t.TempDir()
	memory, err := store.New(dir, "monthly")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	judgeClient := llm.NewFakeClient(judgeTurns...)
	answerClient := llm.NewFakeClient(answerTurns...)

	cfg := config.Config{
		Listen:       "127.0.0.1:0",
		DataDir:      dir,
		Shard:        config.ShardMonthly,
		MaxBodyBytes: 1024, // small, so the size limit is easy to exercise
		Answer: config.ModelConfig{
			BaseURL:   "http://127.0.0.1:1",
			Model:     "m",
			APIKeyEnv: "ACCEPTANCE_KEY",
			Timeout:   time.Second,
		},
		Classifier: config.ModelConfig{
			BaseURL:   "http://127.0.0.1:1",
			Model:     "m",
			APIKeyEnv: "ACCEPTANCE_KEY",
			Timeout:   time.Second,
		},
		Budget: config.BudgetConfig{
			MaxSearchCalls: 3,
			MaxRecords:     20,
			AnswerTimeout:  30 * time.Second,
			RequestTimeout: 60 * time.Second,
		},
	}

	server, err := httpapi.New(httpapi.Deps{
		Config:   cfg,
		Judge:    classify.New(judgeClient, "m", 0),
		Store:    memory,
		Answerer: answer.New(answerClient, search.New(dir), "m", answer.Budget{}),
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("httpapi.New: %v", err)
	}

	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	return &axHarness{t: t, server: httpServer, dir: dir, memory: memory, judge: judgeClient, answerer: answerClient}
}

// post sends content to POST /messages and returns the status and body.
func (h *axHarness) post(content string) (int, []byte) {
	h.t.Helper()

	body, err := json.Marshal(map[string]string{"content": content})
	if err != nil {
		h.t.Fatalf("encode request: %v", err)
	}
	resp, err := http.Post(h.server.URL+"/messages", "application/json", bytes.NewReader(body))
	if err != nil {
		h.t.Fatalf("POST /messages: %v", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		h.t.Fatalf("read response: %v", err)
	}
	return resp.StatusCode, payload
}

// recordLines returns the raw lines currently stored.
func (h *axHarness) recordLines() []string {
	h.t.Helper()
	entries, err := os.ReadDir(h.dir)
	if err != nil {
		h.t.Fatalf("read data dir: %v", err)
	}
	var lines []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(h.dir, entry.Name()))
		if err != nil {
			h.t.Fatalf("read shard: %v", err)
		}
		for _, line := range strings.Split(string(content), "\n") {
			if strings.TrimSpace(line) != "" {
				lines = append(lines, line)
			}
		}
	}
	return lines
}

// seed writes records directly through the store.
func (h *axHarness) seed(contents ...string) []store.Record {
	h.t.Helper()
	var records []store.Record
	for _, content := range contents {
		rec, err := h.memory.Append(context.Background(), content)
		if err != nil {
			h.t.Fatalf("seed %q: %v", content, err)
		}
		records = append(records, rec)
	}
	return records
}

// recordVerdict scripts the classifier to answer "record".
func recordVerdict() []*llm.Response {
	return []*llm.Response{llm.TextResponse(`{"kind":"record"}`)}
}

// queryVerdict scripts the classifier to answer "query".
func queryVerdict() []*llm.Response {
	return []*llm.Response{llm.TextResponse(`{"kind":"query"}`)}
}

// TestAcceptanceRecordRowHasExactlyThreeFields covers the first checklist
// line: a record is one JSONL line with version, content and time and nothing
// else.
func TestAcceptanceRecordRowHasExactlyThreeFields(t *testing.T) {
	h := newAxHarness(t, recordVerdict(), nil)

	status, body := h.post("我喜欢喝乌龙茶")
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", status, body)
	}

	lines := h.recordLines()
	if len(lines) != 1 {
		t.Fatalf("got %d record lines, want 1", len(lines))
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(lines[0]), &fields); err != nil {
		t.Fatalf("record line is not JSON: %v", err)
	}
	if len(fields) != 3 {
		t.Fatalf("record has %d fields, want exactly 3: %s", len(fields), lines[0])
	}
	for _, name := range []string{"version", "content", "time"} {
		if _, ok := fields[name]; !ok {
			t.Errorf("record is missing %q: %s", name, lines[0])
		}
	}
}

// TestAcceptanceTimesIncreaseAndCanBeCited covers the second checklist line.
func TestAcceptanceTimesIncreaseAndCanBeCited(t *testing.T) {
	h := newAxHarness(t, append(recordVerdict(), recordVerdict()...), nil)

	var times []string
	for _, content := range []string{"第一条", "第二条"} {
		status, body := h.post(content)
		if status != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body: %s", status, body)
		}
		var resp struct {
			Time string `json:"time"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			t.Fatalf("response is not JSON: %v", err)
		}
		times = append(times, resp.Time)
	}

	if !(times[1] > times[0]) {
		t.Fatalf("record times are not increasing: %q then %q", times[0], times[1])
	}
	// The time must be usable as a citation anchor: parseable RFC3339Nano.
	if _, err := time.Parse(time.RFC3339Nano, times[0]); err != nil {
		t.Fatalf("record time %q is not RFC3339Nano: %v", times[0], err)
	}
}

// TestAcceptanceQueryLeavesNoTrace covers the third checklist line.
func TestAcceptanceQueryLeavesNoTrace(t *testing.T) {
	h := newAxHarness(t, queryVerdict(), []*llm.Response{
		llm.ToolCallResponse("c1", "search_memory", `{"keywords":["乌龙茶"],"limit":5}`),
		llm.TextResponse(`{"answer":"没有找到相关记忆","citations":[]}`),
	})

	before := filesIn(t, h.dir)
	status, _ := h.post("我喜欢什么茶？")
	after := filesIn(t, h.dir)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if before != after {
		t.Fatalf("a query changed the data directory: %q -> %q", before, after)
	}
}

// TestAcceptanceUnrecognizedLeavesNoTrace covers the fourth checklist line.
func TestAcceptanceUnrecognizedLeavesNoTrace(t *testing.T) {
	h := newAxHarness(t, []*llm.Response{llm.TextResponse(`{"kind":"unrecognized"}`)}, nil)

	before := filesIn(t, h.dir)
	status, body := h.post("今天天气不错")
	after := filesIn(t, h.dir)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", status, body)
	}
	if before != after {
		t.Fatalf("an unrecognized input changed the data directory: %q -> %q", before, after)
	}
}

// TestAcceptanceMixedInputIsAQuery covers the fifth checklist line.
func TestAcceptanceMixedInputIsAQuery(t *testing.T) {
	h := newAxHarness(t,
		[]*llm.Response{llm.TextResponse(`{"kind":"mixed"}`)},
		[]*llm.Response{llm.TextResponse(`{"answer":"没有找到相关记忆","citations":[]}`)},
	)

	before := filesIn(t, h.dir)
	status, body := h.post("我明天去杭州，另外我之前说过喜欢什么茶？")
	after := filesIn(t, h.dir)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", status, body)
	}
	var resp struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if resp.Kind != "query" {
		t.Fatalf("kind = %q, want query for a mixed input", resp.Kind)
	}
	if before != after {
		t.Fatal("a mixed input wrote a record; it must be answered as a query only")
	}
}

// TestAcceptanceModelUnavailableWritesNothing covers the sixth checklist line.
func TestAcceptanceModelUnavailableWritesNothing(t *testing.T) {
	judge := llm.NewFakeClient()
	judge.SetError(llm.ErrUnavailable)

	dir := t.TempDir()
	memory, err := store.New(dir, "monthly")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	cfg := config.Config{
		DataDir:      dir,
		MaxBodyBytes: 1 << 20,
		Answer:       config.ModelConfig{BaseURL: "http://x", Model: "m", APIKeyEnv: "K", Timeout: time.Second},
		Classifier:   config.ModelConfig{BaseURL: "http://x", Model: "m", APIKeyEnv: "K", Timeout: time.Second},
		Budget:       config.BudgetConfig{MaxSearchCalls: 3, MaxRecords: 20, AnswerTimeout: time.Second, RequestTimeout: time.Second},
	}

	server, err := httpapi.New(httpapi.Deps{
		Config:   cfg,
		Judge:    classify.New(judge, "m", 0),
		Store:    memory,
		Answerer: answer.New(llm.NewFakeClient(), search.New(dir), "m", answer.Budget{}),
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("httpapi.New: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	h := &axHarness{t: t, server: httpServer, dir: dir, memory: memory}

	status, body := h.post("我喜欢喝乌龙茶")
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body: %s", status, body)
	}
	if lines := h.recordLines(); len(lines) != 0 {
		t.Fatalf("a failed classification wrote %d records; it must write none", len(lines))
	}
}

// TestAcceptanceNoMatchDoesNotAnswerFromKnowledge covers the seventh line.
func TestAcceptanceNoMatchDoesNotAnswerFromKnowledge(t *testing.T) {
	h := newAxHarness(t, queryVerdict(), []*llm.Response{
		llm.ToolCallResponse("c1", "search_memory", `{"keywords":["车"],"limit":5}`),
		llm.TextResponse(`{"answer":"巴黎是法国的首都，你的车应该是黑色的。","citations":[]}`),
	})

	status, body := h.post("我的车是什么颜色？")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", status, body)
	}

	var resp struct {
		Answer    string `json:"answer"`
		Status    string `json:"status"`
		Citations []any  `json:"citations"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if resp.Status != "no_match" {
		t.Fatalf("status = %q, want no_match", resp.Status)
	}
	if resp.Answer != "没有找到相关记忆" {
		t.Fatalf("answer = %q, want the fixed no-match text", resp.Answer)
	}
	if strings.Contains(resp.Answer, "巴黎") {
		t.Fatalf("the model's general knowledge leaked into the answer: %q", resp.Answer)
	}
	if len(resp.Citations) != 0 {
		t.Fatalf("a no-match answer must carry no citations, got %d", len(resp.Citations))
	}
}

// TestAcceptanceCitationOutsideEvidenceIsRefused covers the eighth line.
func TestAcceptanceCitationOutsideEvidenceIsRefused(t *testing.T) {
	h := newAxHarness(t, queryVerdict(), []*llm.Response{
		llm.ToolCallResponse("c1", "search_memory", `{"keywords":["乌龙茶"],"limit":5}`),
		llm.TextResponse(`{"answer":"你喜欢喝乌龙茶。","citations":["2001-02-03T04:05:06.000000000Z"]}`),
	})

	records := h.seed("我喜欢喝乌龙茶")

	status, body := h.post("我喜欢喝什么茶？")
	if status != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body: %s (seeded %v)", status, body, records)
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("error body is not JSON: %v", err)
	}
	if envelope.Error.Code != "invalid_citation" {
		t.Fatalf("code = %q, want invalid_citation", envelope.Error.Code)
	}
	if bytes.Contains(body, []byte("你喜欢喝乌龙茶。")) {
		t.Fatal("the answer body must not be returned when the citation is refused")
	}
}

// TestAcceptanceBudgetExhaustedKeepsValidCitations covers the ninth line.
func TestAcceptanceBudgetExhaustedKeepsValidCitations(t *testing.T) {
	dir := t.TempDir()
	memory, err := store.New(dir, "monthly")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	var seeded []store.Record
	for _, content := range []string{"我喜欢喝乌龙茶", "我住在杭州", "我养了一只猫"} {
		rec, err := memory.Append(context.Background(), content)
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
		seeded = append(seeded, rec)
	}

	// The budget allows three searches. The fourth call is refused, which is
	// what marks the answer as budget_exhausted; the model then answers from
	// the evidence it already has.
	answerTurns := []*llm.Response{
		llm.ToolCallResponse("c1", "search_memory", `{"keywords":["乌龙茶"],"limit":5}`),
		llm.ToolCallResponse("c2", "search_memory", `{"keywords":["杭州"],"limit":5}`),
		llm.ToolCallResponse("c3", "search_memory", `{"keywords":["猫"],"limit":5}`),
		llm.ToolCallResponse("c4", "search_memory", `{"keywords":["偏好"],"limit":5}`),
		llm.TextResponse(fmt.Sprintf(
			`{"answer":"你喜欢喝乌龙茶。","citations":[%q]}`,
			seeded[0].Time.String())),
	}

	httpServer := serve(t, dir, memory, queryVerdict(), answerTurns)
	h := &axHarness{t: t, server: httpServer, dir: dir, memory: memory}

	status, body := h.post("我的所有偏好是什么？")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", status, body)
	}

	var resp struct {
		Status    string `json:"status"`
		Citations []struct {
			Time    string `json:"time"`
			Content string `json:"content"`
		} `json:"citations"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if resp.Status != "budget_exhausted" {
		t.Fatalf("status = %q, want budget_exhausted", resp.Status)
	}
	if len(resp.Citations) != 1 {
		t.Fatalf("got %d citations, want 1", len(resp.Citations))
	}
	if resp.Citations[0].Time != seeded[0].Time.String() {
		t.Fatalf("citation time = %q, want %q", resp.Citations[0].Time, seeded[0].Time)
	}
	if resp.Citations[0].Content != "我喜欢喝乌龙茶" {
		t.Fatalf("citation content = %q, want the original text", resp.Citations[0].Content)
	}
}

// filesIn returns a stable description of every file in dir.
func filesIn(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read dir: %v", err)
	}
	var parts []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		parts = append(parts, fmt.Sprintf("%s:%d", entry.Name(), info.Size()))
	}
	return strings.Join(parts, ",")
}

// serve starts the service on an existing data directory and store.
func serve(t *testing.T, dir string, memory store.Store, judgeTurns, answerTurns []*llm.Response) *httptest.Server {
	t.Helper()

	cfg := config.Config{
		DataDir:      dir,
		MaxBodyBytes: 1 << 20,
		Answer:       config.ModelConfig{BaseURL: "http://x", Model: "m", APIKeyEnv: "K", Timeout: time.Second},
		Classifier:   config.ModelConfig{BaseURL: "http://x", Model: "m", APIKeyEnv: "K", Timeout: time.Second},
		Budget:       config.BudgetConfig{MaxSearchCalls: 3, MaxRecords: 20, AnswerTimeout: time.Second, RequestTimeout: time.Second},
	}

	server, err := httpapi.New(httpapi.Deps{
		Config:   cfg,
		Judge:    classify.New(llm.NewFakeClient(judgeTurns...), "m", 0),
		Store:    memory,
		Answerer: answer.New(llm.NewFakeClient(answerTurns...), search.New(dir), "m", answer.Budget{}),
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("httpapi.New: %v", err)
	}

	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	return httpServer
}
