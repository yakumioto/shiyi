package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestPasswordLoginRecordsAndQueriesRealMemoryWithoutPersistingConversation(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "memory")
	memory, err := store.New(dir, "monthly")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.AuthConfig{PasswordHash: testPasswordHash(t), StateDir: filepath.Join(root, "auth"), PublicOrigin: "https://memory.example"}
	module, err := auth.New(cfg, auth.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { module.Close() })
	judge := llm.NewFakeClient(llm.TextResponse(`{"kind":"record"}`), llm.TextResponse(`{"kind":"query"}`))
	model := llm.NewFakeClient(llm.ToolCallResponse("lookup", "search_memory", `{"keywords":["乌龙茶"],"limit":5}`))
	app, err := httpapi.New(httpapi.Deps{Auth: module, Config: config.Config{DataDir: dir, Answer: config.ModelConfig{BaseURL: "https://model.invalid", Model: "test"}}, Judge: classify.New(judge, "test", 0), Store: memory, Answerer: answer.New(model, search.New(dir), "test", 0, answer.Budget{}), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Close() })
	server := httptest.NewTLSServer(app.Handler())
	t.Cleanup(server.Close)
	h := &authHarness{t: t, cfg: cfg, server: server}
	b := h.browser()
	res, raw := b.request("POST", "/messages", `{"content":"unauthenticated"}`, nil)
	requireStatus(t, res, raw, 401)
	if judge.CallCount() != 0 || model.CallCount() != 0 {
		t.Fatal("unauthenticated input called a model")
	}
	b.status(false)
	res, raw = b.login(loginPassword)
	requireStatus(t, res, raw, 200)
	const original = "  我喜欢喝乌龙茶\n保持原文，含敏感标记 memory-private-marker。  "
	recordBody, _ := json.Marshal(api.MessageRequest{Content: original})
	res, raw = b.request("POST", "/messages", string(recordBody), nil)
	requireStatus(t, res, raw, 201)
	var record api.RecordResponse
	if json.Unmarshal(raw, &record) != nil {
		t.Fatal("invalid record response")
	}
	model.SetToolResponse("search_memory", llm.TextResponse(fmt.Sprintf(`{"answer":"answer-private-marker：你喜欢乌龙茶。","citations":[%q]}`, record.Time)))
	before := snapshotMemoryFiles(t, dir)
	res, raw = b.request("POST", "/messages", `{"content":"query-private-marker：我喜欢什么茶？"}`, nil)
	requireStatus(t, res, raw, 200)
	var result api.QueryResponse
	if json.Unmarshal(raw, &result) != nil {
		t.Fatal("invalid query response")
	}
	if result.Kind != "query" || result.Status != "ok" || len(result.Citations) != 1 || result.Citations[0].Time != record.Time || result.Citations[0].Content != original {
		t.Fatalf("query did not cite exact recorded original: %+v", result)
	}
	if !bytes.Equal(before, snapshotMemoryFiles(t, dir)) {
		t.Fatal("query or answer changed memory files")
	}
	records, err := memory.Records(context.Background())
	if err != nil || len(records) != 1 || records[0].Content != original || records[0].Version != 1 {
		t.Fatal("authentication changed memory semantics")
	}
	// No credentials, query, answer, or original text can hide in auth state.
	entries, err := os.ReadDir(cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(cfg.StateDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range []string{loginPassword, cfg.PasswordHash, "query-private-marker", "answer-private-marker", "memory-private-marker"} {
			if strings.Contains(string(raw), marker) {
				t.Fatal("auth state persisted private application input")
			}
		}
	}
}

func snapshotMemoryFiles(t *testing.T, dir string) []byte {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []byte
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatal("authentication state appeared under memory")
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, []byte(entry.Name())...)
		out = append(out, raw...)
	}
	return out
}
