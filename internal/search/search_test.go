package search_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shiyi/internal/search"
	"shiyi/internal/store"
)

// newRecord builds a memory entry with the given time and original text.
func newRecord(t *testing.T, ts, content string) store.Record {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t.Fatalf("parse time %q: %v", ts, err)
	}
	return store.Record{Version: 1, Content: content, Time: store.NewTime(parsed)}
}

// writeShard writes records as a shard file inside dir.
func writeShard(t *testing.T, dir, name string, records ...store.Record) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create dir for %s: %v", path, err)
	}
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
	return path
}

// timesOf collects the record times in result order.
func timesOf(records []store.Record) []string {
	out := make([]string, 0, len(records))
	for _, rec := range records {
		out = append(out, rec.Time.String())
	}
	return out
}

func requireRG(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rg"); err != nil {
		t.Fatalf("ripgrep (rg) is required by the search seam but was not found in PATH: %v", err)
	}
}

func TestSearchMatches(t *testing.T) {
	requireRG(t)

	const (
		tA = "2026-11-01T10:00:00.000000000Z"
		tB = "2026-11-02T10:00:00.000000000Z"
		tC = "2026-11-03T10:00:00.000000000Z"
	)

	tests := []struct {
		name     string
		records  []store.Record
		keywords []string
		limit    int
		want     []string
	}{
		{
			name: "single keyword hits the newest record only",
			records: []store.Record{
				newRecord(t, tA, "I drink coffee every morning"),
				newRecord(t, tB, "I run at the park"),
				newRecord(t, tC, "I drink tea in the evening"),
			},
			keywords: []string{"coffee"},
			limit:    10,
			want:     []string{tA},
		},
		{
			name: "multiple keywords are combined with OR",
			records: []store.Record{
				newRecord(t, tA, "I drink coffee every morning"),
				newRecord(t, tB, "I run at the park"),
				newRecord(t, tC, "I drink tea in the evening"),
			},
			keywords: []string{"coffee", "park"},
			limit:    10,
			want:     []string{tB, tA},
		},
		{
			name: "matching ignores case",
			records: []store.Record{
				newRecord(t, tA, "I drink COFFEE every morning"),
			},
			keywords: []string{"coffee"},
			limit:    10,
			want:     []string{tA},
		},
		{
			name: "matching ignores the case of the keyword",
			records: []store.Record{
				newRecord(t, tA, "I drink coffee every morning"),
			},
			keywords: []string{"CoFfEe"},
			limit:    10,
			want:     []string{tA},
		},
		{
			name: "chinese is matched as a plain substring",
			records: []store.Record{
				newRecord(t, tA, "我喜欢在北京吃包子"),
				newRecord(t, tB, "我住在上海"),
			},
			keywords: []string{"北京"},
			limit:    10,
			want:     []string{tA},
		},
		{
			name: "a keyword that only occurs in the time value does not match",
			records: []store.Record{
				newRecord(t, tA, "nothing interesting here"),
			},
			keywords: []string{"2026-11"},
			limit:    10,
			want:     []string{},
		},
		{
			name: "a keyword that is only a json field name does not match",
			records: []store.Record{
				newRecord(t, tA, "nothing interesting here"),
			},
			keywords: []string{"content"},
			limit:    10,
			want:     []string{},
		},
		{
			name: "a keyword that is only a json field value does not match",
			records: []store.Record{
				newRecord(t, tA, "nothing interesting here"),
			},
			keywords: []string{"version"},
			limit:    10,
			want:     []string{},
		},
		{
			name: "records are returned newest first",
			records: []store.Record{
				newRecord(t, tC, "coffee number three"),
				newRecord(t, tA, "coffee number one"),
				newRecord(t, tB, "coffee number two"),
			},
			keywords: []string{"coffee"},
			limit:    10,
			want:     []string{tC, tB, tA},
		},
		{
			name: "the limit truncates to the newest records",
			records: []store.Record{
				newRecord(t, tA, "coffee number one"),
				newRecord(t, tB, "coffee number two"),
				newRecord(t, tC, "coffee number three"),
			},
			keywords: []string{"coffee"},
			limit:    2,
			want:     []string{tC, tB},
		},
		{
			name: "a limit of zero falls back to the default",
			records: []store.Record{
				newRecord(t, tA, "coffee number one"),
				newRecord(t, tB, "coffee number two"),
			},
			keywords: []string{"coffee"},
			limit:    0,
			want:     []string{tB, tA},
		},
		{
			name: "a negative limit falls back to the default",
			records: []store.Record{
				newRecord(t, tA, "coffee number one"),
			},
			keywords: []string{"coffee"},
			limit:    -5,
			want:     []string{tA},
		},
		{
			name: "a limit above the maximum is clamped",
			records: []store.Record{
				newRecord(t, tA, "coffee number one"),
				newRecord(t, tB, "coffee number two"),
			},
			keywords: []string{"coffee"},
			limit:    1000,
			want:     []string{tB, tA},
		},
		{
			name: "no record matches",
			records: []store.Record{
				newRecord(t, tA, "I drink coffee every morning"),
			},
			keywords: []string{"bicycle"},
			limit:    10,
			want:     []string{},
		},
		{
			name: "duplicate keywords are searched once",
			records: []store.Record{
				newRecord(t, tA, "coffee"),
			},
			keywords: []string{"coffee", "coffee"},
			limit:    10,
			want:     []string{tA},
		},
		{
			name: "surrounding whitespace is stripped from keywords",
			records: []store.Record{
				newRecord(t, tA, "coffee"),
			},
			keywords: []string{"  coffee  "},
			limit:    10,
			want:     []string{tA},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeShard(t, dir, "2026-11.jsonl", tc.records...)

			got, err := search.New(dir).Search(context.Background(), tc.keywords, tc.limit)
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			if want, have := len(tc.want), len(got); want != have {
				t.Fatalf("got %d records %v, want %d records %v", have, timesOf(got), want, tc.want)
			}
			for i := range tc.want {
				if got[i].Time.String() != tc.want[i] {
					t.Fatalf("record %d: got time %s, want %s (all: %v)", i, got[i].Time.String(), tc.want[i], timesOf(got))
				}
			}
			for _, rec := range got {
				if rec.Content == "" {
					t.Fatalf("record %s came back without its original text", rec.Time.String())
				}
			}
		})
	}
}

func TestSearchDeduplicatesByTime(t *testing.T) {
	requireRG(t)

	dir := t.TempDir()
	rec := newRecord(t, "2026-11-01T10:00:00.000000000Z", "coffee")
	writeShard(t, dir, "2026-11.jsonl", rec)
	writeShard(t, dir, "2026-10.jsonl", rec)

	got, err := search.New(dir).Search(context.Background(), []string{"coffee"}, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d records %v, want 1", len(got), timesOf(got))
	}
}

func TestSearchIgnoresCorruptDirectory(t *testing.T) {
	requireRG(t)

	dir := t.TempDir()
	writeShard(t, dir, "2026-11.jsonl", newRecord(t, "2026-11-01T10:00:00.000000000Z", "unrelated note"))
	writeShard(t, dir, filepath.Join("corrupt", "2026-11.jsonl"), newRecord(t, "2026-11-02T10:00:00.000000000Z", "coffee in a corrupt shard"))

	got, err := search.New(dir).Search(context.Background(), []string{"coffee"}, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d records %v, want none: the corrupt directory must not be searched", len(got), timesOf(got))
	}
}

func TestSearchReturnsEmptySliceWithoutError(t *testing.T) {
	requireRG(t)

	dir := t.TempDir()
	writeShard(t, dir, "2026-11.jsonl", newRecord(t, "2026-11-01T10:00:00.000000000Z", "unrelated note"))

	got, err := search.New(dir).Search(context.Background(), []string{"bicycle"}, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got == nil {
		t.Fatal("got a nil slice, want an empty one")
	}
	if len(got) != 0 {
		t.Fatalf("got %d records %v, want none", len(got), timesOf(got))
	}
}

func TestSearchWithoutAnyShardFile(t *testing.T) {
	requireRG(t)

	got, err := search.New(t.TempDir()).Search(context.Background(), []string{"coffee"}, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d records %v, want none", len(got), timesOf(got))
	}
}

func TestSearchRejectsInvalidArguments(t *testing.T) {
	requireRG(t)

	long := strings.Repeat("x", search.MaxKeywordLen+1)
	many := make([]string, search.MaxKeywords+1)
	for i := range many {
		many[i] = fmt.Sprintf("k%d", i)
	}

	tests := []struct {
		name     string
		keywords []string
	}{
		{name: "no keywords", keywords: nil},
		{name: "only blank keywords", keywords: []string{"", "   "}},
		{name: "too many keywords", keywords: many},
		{name: "keyword too long", keywords: []string{long}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeShard(t, dir, "2026-11.jsonl", newRecord(t, "2026-11-01T10:00:00.000000000Z", "coffee"))

			got, err := search.New(dir).Search(context.Background(), tc.keywords, 10)
			if err == nil {
				t.Fatalf("Search returned %v, want an error", timesOf(got))
			}
			if !errors.Is(err, search.ErrInvalidArguments) {
				t.Fatalf("got error %v, want one wrapping search.ErrInvalidArguments", err)
			}
		})
	}
}

func TestSearchAcceptsTheKeywordLimits(t *testing.T) {
	requireRG(t)

	dir := t.TempDir()
	writeShard(t, dir, "2026-11.jsonl", newRecord(t, "2026-11-01T10:00:00.000000000Z", "coffee"))

	many := make([]string, search.MaxKeywords)
	for i := range many {
		many[i] = fmt.Sprintf("k%d", i)
	}
	many[len(many)-1] = "coffee"

	if _, err := search.New(dir).Search(context.Background(), many, 10); err != nil {
		t.Fatalf("Search with %d keywords: %v", len(many), err)
	}
	if _, err := search.New(dir).Search(context.Background(), []string{strings.Repeat("x", search.MaxKeywordLen)}, 10); err != nil {
		t.Fatalf("Search with a %d byte keyword: %v", search.MaxKeywordLen, err)
	}
}

func TestSearchFailsOnBrokenShardLine(t *testing.T) {
	requireRG(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "2026-11.jsonl")
	line, err := json.Marshal(newRecord(t, "2026-11-01T10:00:00.000000000Z", "coffee"))
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	content := string(line) + "\n" + "{\"version\":1,\"content\":\"truncated\"\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	got, err := search.New(dir).Search(context.Background(), []string{"coffee"}, 10)
	if err == nil {
		t.Fatalf("Search returned %v, want an error for a broken shard line", timesOf(got))
	}
	if !errors.Is(err, search.ErrSearchFailed) {
		t.Fatalf("got error %v, want one wrapping search.ErrSearchFailed", err)
	}
}

func TestSearchSkipsFilesThatAreNotShards(t *testing.T) {
	requireRG(t)

	dir := t.TempDir()
	writeShard(t, dir, "2026-11.jsonl", newRecord(t, "2026-11-01T10:00:00.000000000Z", "unrelated note"))
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("coffee in a text file\n"), 0o644); err != nil {
		t.Fatalf("write notes.txt: %v", err)
	}

	got, err := search.New(dir).Search(context.Background(), []string{"coffee"}, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d records %v, want none: only *.jsonl shards are searched", len(got), timesOf(got))
	}
}

// TestSearchHandlesKeywordsThatLookLikeFlags checks that a keyword which
// looks like a command line flag is searched for and not taken for one.
func TestSearchHandlesKeywordsThatLookLikeFlags(t *testing.T) {
	requireRG(t)

	const tPrice = "2026-11-01T10:00:00.000000000Z"

	dir := t.TempDir()
	writeShard(t, dir, "2026-11.jsonl", newRecord(t, tPrice, "my price is -5 dollars"))

	for _, keyword := range []string{"-5", "--help", "-e"} {
		got, err := search.New(dir).Search(context.Background(), []string{keyword}, 10)
		if err != nil {
			t.Fatalf("Search with %q: %v", keyword, err)
		}
		if keyword == "-5" {
			if len(got) != 1 {
				t.Fatalf("Search with %q: got %d records %v, want 1", keyword, len(got), timesOf(got))
			}
			continue
		}
		if len(got) != 0 {
			t.Fatalf("Search with %q: got %d records %v, want none", keyword, len(got), timesOf(got))
		}
	}
}

// TestSearchHandlesRegularExpressionCharacters checks that a keyword with
// regular expression characters is searched for literally: the dot in "5."
// matches the dot and nothing else.
func TestSearchHandlesRegularExpressionCharacters(t *testing.T) {
	requireRG(t)

	const tA = "2026-11-01T10:00:00.000000000Z"
	const tB = "2026-11-02T10:00:00.000000000Z"

	dir := t.TempDir()
	writeShard(t, dir, "2026-11.jsonl",
		newRecord(t, tA, "the cost is 5.00 dollars"),
		newRecord(t, tB, "the cost is 5x00 dollars"),
	)

	got, err := search.New(dir).Search(context.Background(), []string{"5."}, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].Time.String() != tA {
		t.Fatalf("got %d records %v, want only %s: the keyword is matched literally", len(got), timesOf(got), tA)
	}
}

// TestALargeRecordCanBeFoundAgain covers the widest input the API accepts.
//
// A one-mebibyte input of characters that have to be escaped grows to about
// two mebibytes once it is JSON-encoded. A record that was written must stay
// findable, so the reader has to agree with the writer on how long a line may
// be; otherwise the memory is saved and then silently unsearchable.
func TestALargeRecordCanBeFoundAgain(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{name: "one mebibyte of newlines", content: strings.Repeat("\n", 1<<20) + "needle"},
		{name: "one mebibyte of quotes", content: strings.Repeat(`"`, 1<<20) + "needle"},
		{name: "half a mebibyte of newlines", content: strings.Repeat("\n", 512<<10) + "needle"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			memory, err := store.New(dir, store.ShardMonthly)
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			if _, err := memory.Append(context.Background(), tc.content); err != nil {
				t.Fatalf("append: %v", err)
			}

			got, err := search.New(dir).Search(context.Background(), []string{"needle"}, 5)
			if err != nil {
				t.Fatalf("search a record that was just written: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("got %d records, want the one that was just written", len(got))
			}
			if got[0].Content != tc.content {
				t.Fatalf("the record came back changed")
			}
		})
	}
}
