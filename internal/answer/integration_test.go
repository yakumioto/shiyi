package answer_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"shiyi/internal/answer"
	"shiyi/internal/llm"
	"shiyi/internal/search"
	"shiyi/internal/store"
)

// writeRecords writes records as a shard file in dir.
func writeRecords(t *testing.T, dir string, records ...store.Record) {
	t.Helper()
	path := filepath.Join(dir, "2026-11.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	for _, rec := range records {
		line, err := json.Marshal(rec)
		if err != nil {
			t.Fatalf("marshal record: %v", err)
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
}

// TestAnswerWithTheRealSearcher runs the answering over the real search,
// so that the two packages are seen to work together: the model asks for a
// keyword, ripgrep finds the shard, and the record it holds becomes the
// evidence the answer cites.
func TestAnswerWithTheRealSearcher(t *testing.T) {
	const tCoffee = "2026-11-01T10:00:00.000000000Z"
	const tTea = "2026-11-02T10:00:00.000000000Z"

	dir := t.TempDir()
	writeRecords(t, dir,
		record(t, tCoffee, "I drink coffee every morning"),
		record(t, tTea, "I drink tea in the evening"),
	)

	client := &fakeLLM{responses: []*llm.Response{
		toolResponse(t, "call-1", `{"keywords":["coffee"]}`),
		finalResponse(answerJSON(t, "You drink coffee every morning.", tCoffee)),
	}}

	got, err := answer.New(client, search.New(dir), "test-model", 0, answer.DefaultBudget()).Answer(context.Background(), "What do I drink in the morning?")
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if got.Status != answer.StatusOK {
		t.Fatalf("got status %q, want %q", got.Status, answer.StatusOK)
	}
	if got.Answer != "You drink coffee every morning." {
		t.Fatalf("got answer %q, want the model's answer", got.Answer)
	}
	if len(got.Citations) != 1 {
		t.Fatalf("got %d citations, want 1", len(got.Citations))
	}
	if got.Citations[0].Time != tCoffee {
		t.Fatalf("got citation time %q, want %q", got.Citations[0].Time, tCoffee)
	}
	if got.Citations[0].Content != "I drink coffee every morning" {
		t.Fatalf("got citation content %q, want the original text", got.Citations[0].Content)
	}
}

// TestAnswerWithTheRealSearcherFindsNothing runs the answering over the
// real search when the records hold nothing of the query.
func TestAnswerWithTheRealSearcherFindsNothing(t *testing.T) {
	dir := t.TempDir()
	writeRecords(t, dir, record(t, "2026-11-01T10:00:00.000000000Z", "I drink coffee every morning"))

	client := &fakeLLM{responses: []*llm.Response{
		toolResponse(t, "call-1", `{"keywords":["bicycle"]}`),
		finalResponse("I believe you ride a bicycle."),
	}}

	got, err := answer.New(client, search.New(dir), "test-model", 0, answer.DefaultBudget()).Answer(context.Background(), "Do I ride a bicycle?")
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if got.Status != answer.StatusNoMatch {
		t.Fatalf("got status %q, want %q", got.Status, answer.StatusNoMatch)
	}
	if got.Answer != answer.NoMatchAnswer {
		t.Fatalf("got answer %q, want the fixed answer", got.Answer)
	}
	if len(got.Citations) != 0 {
		t.Fatalf("got %d citations, want none", len(got.Citations))
	}
}

// TestAnswerWithTheRealSearcherLeavesTheFilesAlone checks that answering
// writes nothing: a query must not change the data directory.
func TestAnswerWithTheRealSearcherLeavesTheFilesAlone(t *testing.T) {
	dir := t.TempDir()
	writeRecords(t, dir, record(t, "2026-11-01T10:00:00.000000000Z", "I drink coffee every morning"))

	before, err := os.ReadFile(filepath.Join(dir, "2026-11.jsonl"))
	if err != nil {
		t.Fatalf("read shard: %v", err)
	}

	client := &fakeLLM{responses: []*llm.Response{
		toolResponse(t, "call-1", `{"keywords":["coffee"]}`),
		finalResponse(answerJSON(t, "You drink coffee.", "2026-11-01T10:00:00.000000000Z")),
	}}

	if _, err := answer.New(client, search.New(dir), "test-model", 0, answer.DefaultBudget()).Answer(context.Background(), "What do I drink?"); err != nil {
		t.Fatalf("Answer: %v", err)
	}

	after, err := os.ReadFile(filepath.Join(dir, "2026-11.jsonl"))
	if err != nil {
		t.Fatalf("read shard: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("the shard changed:\nbefore %q\nafter  %q", before, after)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d files in the data directory, want 1", len(entries))
	}
}
