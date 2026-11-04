package store_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shiyi/internal/store"
)

// TestCompleteTailWithoutNewlineIsKept guards against losing a record.
//
// A shard whose last line is a complete record but has no trailing newline
// holds a real memory entry: the newline was lost, not the record. The
// start-up scan has to keep it and give the file back its newline, because
// dropping it would silently delete a record the user believes is saved.
func TestCompleteTailWithoutNewlineIsKept(t *testing.T) {
	dir := t.TempDir()
	shard := filepath.Join(dir, "2026-11.jsonl")

	line := `{"version":1,"content":"完整记录但换行丢了","time":"2026-11-04T12:00:00.000000000Z"}`
	if err := os.WriteFile(shard, []byte(line), 0o644); err != nil {
		t.Fatalf("write shard: %v", err)
	}

	s, err := store.New(dir, store.ShardMonthly)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	records, err := s.Records(context.Background())
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("完整记录应被保留，读到 %d 条（应为 1 条）", len(records))
	}
	if records[0].Content != "完整记录但换行丢了" {
		t.Fatalf("content = %q, want %q", records[0].Content, "完整记录但换行丢了")
	}

	// The shard ends with a newline again, so the next append fits.
	data, err := os.ReadFile(shard)
	if err != nil {
		t.Fatalf("read shard: %v", err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatalf("分片应以换行符结尾，得到 %q", string(data))
	}
}

// TestLargeRecordCanBeReadBack guards against losing a record that needs
// escaping.
//
// The API accepts an input of one mebibyte. Its JSON encoding is longer
// than the input when the input holds characters that have to be escaped, so
// the reader has to allow for a line that is twice as long as the limit.
func TestLargeRecordCanBeReadBack(t *testing.T) {
	dir := t.TempDir()

	s, err := store.New(dir, store.ShardMonthly)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// A mebibyte of quotes: every one of them takes two bytes in JSON.
	content := strings.Repeat("\"", 1<<20)
	if _, err := s.Append(context.Background(), content); err != nil {
		t.Fatalf("Append: %v", err)
	}

	reopened, err := store.New(dir, store.ShardMonthly)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	records, err := reopened.Records(context.Background())
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("应读到 1 条记录，读到 %d 条", len(records))
	}
	if records[0].Content != content {
		t.Fatalf("原文应逐字保留，长度应为 %d，得到 %d", len(content), len(records[0].Content))
	}
}
