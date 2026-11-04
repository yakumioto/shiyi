package frozen_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	"shiyi/internal/frozen"
	"shiyi/internal/httpapi"
	"shiyi/internal/llm"
	"shiyi/internal/search"
	"shiyi/internal/store"
)

// fixturesRoot is where the frozen versions live, relative to the module root.
const fixturesRoot = "../../testdata/frozen"

// TestFrozenManifestsAreIntact fails when a frozen file was edited or deleted
// in place. This is the guard that makes the set trustworthy.
func TestFrozenManifestsAreIntact(t *testing.T) {
	versions, err := frozen.Versions(fixturesRoot)
	if err != nil {
		t.Fatalf("list frozen versions: %v", err)
	}
	if len(versions) == 0 {
		t.Fatal("no frozen fixture versions found")
	}

	for _, version := range versions {
		t.Run(version, func(t *testing.T) {
			dir := filepath.Join(fixturesRoot, version)
			manifest, err := frozen.LoadManifest(dir)
			if err != nil {
				t.Fatalf("load manifest: %v", err)
			}
			if manifest.Version != version {
				t.Errorf("manifest says version %q, directory is %q", manifest.Version, version)
			}
			if err := frozen.VerifyManifest(dir, manifest); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestFrozenClassification runs every classification fixture through the real
// HTTP entry point with a scripted model.
func TestFrozenClassification(t *testing.T) {
	for _, version := range mustVersions(t) {
		path := filepath.Join(fixturesRoot, version, "classify.jsonl")
		cases, err := frozen.LoadClassifyCases(path)
		if err != nil {
			t.Fatalf("load %s: %v", path, err)
		}
		for _, tc := range cases {
			t.Run(version+"/"+tc.Name, func(t *testing.T) {
				runClassifyCase(t, tc)
			})
		}
	}
}

// TestFrozenQueries runs every query fixture through the real HTTP entry
// point with a scripted model and the real search over a temporary data
// directory.
func TestFrozenQueries(t *testing.T) {
	for _, version := range mustVersions(t) {
		path := filepath.Join(fixturesRoot, version, "query.jsonl")
		cases, err := frozen.LoadQueryCases(path)
		if err != nil {
			t.Fatalf("load %s: %v", path, err)
		}
		for _, tc := range cases {
			t.Run(version+"/"+tc.Name, func(t *testing.T) {
				runQueryCase(t, tc)
			})
		}
	}
}

// mustVersions lists the frozen versions or fails.
func mustVersions(t *testing.T) []string {
	t.Helper()
	versions, err := frozen.Versions(fixturesRoot)
	if err != nil {
		t.Fatalf("list frozen versions: %v", err)
	}
	if len(versions) == 0 {
		t.Fatal("no frozen fixture versions found")
	}
	return versions
}

// runClassifyCase drives one classification fixture.
//
// The classifier and the answerer get separate scripted models, because they
// are separate model calls in the real service too.
func runClassifyCase(t *testing.T, tc frozen.ClassifyCase) {
	t.Helper()

	judgeClient := llm.NewFakeClient()
	if tc.ModelError != "" {
		judgeClient.SetError(modelError(tc.ModelError))
	} else {
		judgeClient = llm.NewFakeClient(llm.TextResponse(tc.ModelResponse))
	}

	answerClient := llm.NewFakeClient(buildAnswerTurns(t, tc.AnswerTurns, nil)...)

	server, dir := newTestServer(t, judgeClient, answerClient)
	defer server.Close()

	body, err := json.Marshal(map[string]string{"content": tc.Input})
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}

	resp, err := http.Post(server.URL+"/messages", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /messages: %v", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	if got, want := resp.StatusCode, tc.Expect.HTTPStatus; got != want {
		t.Fatalf("status = %d, want %d; body: %s", got, want, payload)
	}

	if tc.Expect.ErrorCode != "" {
		var envelope struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(payload, &envelope); err != nil {
			t.Fatalf("error body is not JSON: %v (%s)", err, payload)
		}
		if envelope.Error.Code != tc.Expect.ErrorCode {
			t.Fatalf("error code = %q, want %q", envelope.Error.Code, tc.Expect.ErrorCode)
		}
	}

	if tc.Expect.Kind != "" {
		var parsed struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(payload, &parsed); err != nil {
			t.Fatalf("response is not JSON: %v (%s)", err, payload)
		}
		if parsed.Kind != tc.Expect.Kind {
			t.Fatalf("kind = %q, want %q", parsed.Kind, tc.Expect.Kind)
		}
	}

	if tc.Expect.Status != "" {
		var parsed struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(payload, &parsed); err != nil {
			t.Fatalf("response is not JSON: %v (%s)", err, payload)
		}
		if parsed.Status != tc.Expect.Status {
			t.Fatalf("status = %q, want %q", parsed.Status, tc.Expect.Status)
		}
	}

	// The record/no-record rule is the core promise of the product.
	records := readRecords(t, dir)
	wroteRecord := len(records) > 0
	if wroteRecord != tc.Expect.WritesRecord {
		t.Fatalf("wrote a record = %v, want %v (records: %v)", wroteRecord, tc.Expect.WritesRecord, records)
	}
	if tc.Expect.WritesRecord && len(records) == 1 && records[0] != tc.Input {
		t.Fatalf("stored content = %q, want the input verbatim %q", records[0], tc.Input)
	}
}

// runQueryCase drives one query fixture.
func runQueryCase(t *testing.T, tc frozen.QueryCase) {
	t.Helper()

	dir := t.TempDir()
	memory, err := store.New(dir, "monthly")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	// Write the seeded records and remember their times so fixtures can cite
	// them by position.
	var times []string
	for _, content := range tc.Records {
		rec, err := memory.Append(context.Background(), content)
		if err != nil {
			t.Fatalf("seed record %q: %v", content, err)
		}
		times = append(times, rec.Time.String())
	}

	// The classifier and the answerer are separate model calls in the real
	// service, so the fixture scripts them separately.
	judgeClient := llm.NewFakeClient(llm.TextResponse(tc.ClassifyResponse))

	answerClient := llm.NewFakeClient(buildAnswerTurns(t, tc.Turns, times)...)
	if tc.ModelError != "" {
		answerClient.SetError(modelError(tc.ModelError))
	}

	handler := newHandler(t, judgeClient, answerClient, memory, dir)
	server := httptest.NewServer(handler)
	defer server.Close()

	body, err := json.Marshal(map[string]string{"content": tc.Input})
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}

	resp, err := http.Post(server.URL+"/messages", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /messages: %v", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	if got, want := resp.StatusCode, tc.Expect.HTTPStatus; got != want {
		t.Fatalf("status = %d, want %d; body: %s", got, want, payload)
	}

	if tc.Expect.ErrorCode != "" {
		var envelope struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(payload, &envelope); err != nil {
			t.Fatalf("error body is not JSON: %v (%s)", err, payload)
		}
		if envelope.Error.Code != tc.Expect.ErrorCode {
			t.Fatalf("error code = %q, want %q", envelope.Error.Code, tc.Expect.ErrorCode)
		}
		// A failed query must not have written anything either.
		if got := len(readRecords(t, dir)); got != len(tc.Records) {
			t.Fatalf("a failed query changed the record count: %d, want %d", got, len(tc.Records))
		}
		return
	}

	var body2 struct {
		Kind      string `json:"kind"`
		Answer    string `json:"answer"`
		Status    string `json:"status"`
		Citations []struct {
			Time    string `json:"time"`
			Content string `json:"content"`
		} `json:"citations"`
	}
	if err := json.Unmarshal(payload, &body2); err != nil {
		t.Fatalf("response is not JSON: %v (%s)", err, payload)
	}

	if got, want := body2.Kind, "query"; got != want {
		t.Fatalf("kind = %q, want %q", got, want)
	}
	if tc.Expect.Status != "" && body2.Status != tc.Expect.Status {
		t.Fatalf("status = %q, want %q (answer: %q)", body2.Status, tc.Expect.Status, body2.Answer)
	}
	for _, want := range tc.Expect.AnswerContains {
		if !strings.Contains(body2.Answer, want) {
			t.Errorf("answer %q should contain %q", body2.Answer, want)
		}
	}
	for _, unwanted := range tc.Expect.AnswerExcludes {
		if strings.Contains(body2.Answer, unwanted) {
			t.Errorf("answer %q must not contain %q", body2.Answer, unwanted)
		}
	}
	if tc.Expect.CitationCount >= 0 && len(body2.Citations) != tc.Expect.CitationCount {
		t.Fatalf("got %d citations, want %d", len(body2.Citations), tc.Expect.CitationCount)
	}
	if tc.Expect.CitationsCarryContent {
		for i, citation := range body2.Citations {
			if citation.Content == "" {
				t.Errorf("citation %d carries no original text", i)
			}
			if citation.Time == "" {
				t.Errorf("citation %d carries no record time", i)
			}
		}
	}

	// A query never writes.
	if got, want := len(readRecords(t, dir)), len(tc.Records); got != want {
		t.Fatalf("query wrote to the data directory: %d records, want %d", got, want)
	}
}

// buildAnswerTurns turns fixture turns into scripted model responses,
// substituting the seeded record times for the placeholders.
func buildAnswerTurns(t *testing.T, turns []frozen.Turn, times []string) []*llm.Response {
	t.Helper()

	var responses []*llm.Response
	for i, turn := range turns {
		if turn.ToolCall != nil {
			responses = append(responses, llm.ToolCallResponse(
				fmt.Sprintf("call_%d", i), turn.ToolCall.Name, substitute(turn.ToolCall.Args, times)))
			continue
		}
		responses = append(responses, llm.TextResponse(substitute(turn.Text, times)))
	}
	return responses
}

// substitute replaces __TIME0__, __TIME1__, ... with the seeded record times.
func substitute(text string, times []string) string {
	for i, when := range times {
		text = strings.ReplaceAll(text, fmt.Sprintf("__TIME%d__", i), when)
	}
	return text
}

// modelError maps a fixture failure name onto the sentinel error.
func modelError(name string) error {
	switch name {
	case "unavailable":
		return llm.ErrUnavailable
	case "invalid_response":
		return llm.ErrInvalidResponse
	default:
		return errors.New("unknown model error " + name)
	}
}

// readRecords returns the record texts currently stored in dir.
func readRecords(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read data directory: %v", err)
	}

	var contents []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read shard: %v", err)
		}
		for _, line := range strings.Split(string(content), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var rec struct {
				Content string `json:"content"`
			}
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				t.Fatalf("shard line is not JSON: %v", err)
			}
			contents = append(contents, rec.Content)
		}
	}
	return contents
}

// newTestConfig builds a configuration that points at dir.
func newTestConfig(dir string) config.Config {
	return config.Config{
		Listen:       "127.0.0.1:0",
		DataDir:      dir,
		Shard:        config.ShardMonthly,
		MaxBodyBytes: config.DefaultMaxBodyBytes,
		Answer: config.ModelConfig{
			BaseURL:   "http://127.0.0.1:1",
			Model:     "fixture-model",
			APIKeyEnv: "FROZEN_TEST_KEY",
			Timeout:   time.Second,
		},
		Classifier: config.ModelConfig{
			BaseURL:   "http://127.0.0.1:1",
			Model:     "fixture-model",
			APIKeyEnv: "FROZEN_TEST_KEY",
			Timeout:   time.Second,
		},
		Budget: config.BudgetConfig{
			MaxSearchCalls: config.DefaultMaxSearchCalls,
			MaxRecords:     config.DefaultMaxRecords,
			AnswerTimeout:  config.DefaultAnswerTimeout,
			RequestTimeout: config.DefaultRequestTimeout,
		},
	}
}

// newHandler assembles the real HTTP handler around the given model client.
func newHandler(t *testing.T, judgeClient, answerClient llm.Client, memory store.Store, dir string) http.Handler {
	t.Helper()

	cfg := newTestConfig(dir)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	server, err := httpapi.New(httpapi.Deps{
		Config: cfg,
		Judge:  classify.New(judgeClient, cfg.Classifier.Model, cfg.Classifier.Temperature),
		Store:  memory,
		Answerer: answer.New(answerClient, search.New(dir), cfg.Answer.Model, cfg.Answer.Temperature, answer.Budget{
			MaxSearchCalls: cfg.Budget.MaxSearchCalls,
			MaxRecords:     cfg.Budget.MaxRecords,
			AnswerTimeout:  cfg.Budget.AnswerTimeout,
		}),
		Logger: logger,
	})
	if err != nil {
		t.Fatalf("httpapi.New: %v", err)
	}
	return server.Handler()
}

// newTestServer wraps newHandler in an httptest server plus its data directory.
func newTestServer(t *testing.T, judgeClient, answerClient llm.Client) (*httptest.Server, string) {
	t.Helper()

	dir := t.TempDir()
	memory, err := store.New(dir, "monthly")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	handler := newHandler(t, judgeClient, answerClient, memory, dir)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server, dir
}
