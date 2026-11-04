package store_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"shiyi/internal/store"
)

// newStore opens a store on a fresh temporary directory.
func newStore(t *testing.T, shard string) *store.FS {
	t.Helper()
	s, err := store.New(t.TempDir(), shard)
	if err != nil {
		t.Fatalf("New(%q): %v", shard, err)
	}
	return s
}

// shardFiles lists the *.jsonl files directly under dir.
func shardFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read data dir: %v", err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".jsonl") {
			names = append(names, entry.Name())
		}
	}
	return names
}

// readLines returns every non-empty line of a file.
func readLines(t *testing.T, path string) []string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var lines []string
	for _, line := range strings.Split(string(content), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// TestAppendWritesExactlyTheDocumentedFields guards the on-disk shape: three
// fields, no more.
func TestAppendWritesExactlyTheDocumentedFields(t *testing.T) {
	s := newStore(t, store.ShardMonthly)

	if _, err := s.Append(context.Background(), "我喜欢喝茶"); err != nil {
		t.Fatalf("Append: %v", err)
	}

	files := shardFiles(t, s.Dir())
	if len(files) != 1 {
		t.Fatalf("expected exactly one shard file, got %v", files)
	}

	lines := readLines(t, filepath.Join(s.Dir(), files[0]))
	if len(lines) != 1 {
		t.Fatalf("expected exactly one record line, got %d", len(lines))
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(lines[0]), &raw); err != nil {
		t.Fatalf("record is not JSON: %v", err)
	}
	for _, field := range []string{"version", "content", "time"} {
		if _, ok := raw[field]; !ok {
			t.Errorf("record is missing field %q: %s", field, lines[0])
		}
	}
	if len(raw) != 3 {
		t.Errorf("record has %d fields, want exactly version, content, time: %s", len(raw), lines[0])
	}
}

// TestContentIsPreservedVerbatim guards the promise that the original text is
// stored unchanged.
func TestContentIsPreservedVerbatim(t *testing.T) {
	s := newStore(t, store.ShardMonthly)

	originals := []string{
		"我喜欢喝茶",
		"  前导和尾随空格  ",
		"第一行\n第二行",
		"emoji 🍵 and mixed 中英 text",
		`quotes " and \ backslash`,
	}
	for _, content := range originals {
		rec, err := s.Append(context.Background(), content)
		if err != nil {
			t.Fatalf("Append(%q): %v", content, err)
		}
		if rec.Content != content {
			t.Fatalf("returned content = %q, want verbatim %q", rec.Content, content)
		}
	}

	records, err := s.Records(context.Background())
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if len(records) != len(originals) {
		t.Fatalf("got %d records, want %d", len(records), len(originals))
	}

	// Records come back newest first, so compare against the reversed input.
	for i, want := range originals {
		got := records[len(originals)-1-i].Content
		if got != want {
			t.Errorf("stored content[%d] = %q, want %q", i, got, want)
		}
	}
}

// TestTimeIsStrictlyIncreasing is the identifier guarantee: every record time
// is later than the one before it, with no duplicates.
func TestTimeIsStrictlyIncreasing(t *testing.T) {
	s := newStore(t, store.ShardMonthly)

	const count = 1000
	times := make([]store.Time, 0, count)
	for i := 0; i < count; i++ {
		rec, err := s.Append(context.Background(), "记录")
		if err != nil {
			t.Fatalf("Append #%d: %v", i, err)
		}
		times = append(times, rec.Time)
	}

	for i := 1; i < len(times); i++ {
		if !times[i].After(times[i-1].Time) {
			t.Fatalf("time[%d] = %s is not after time[%d] = %s",
				i, times[i], i-1, times[i-1])
		}
	}

	seen := make(map[string]bool, count)
	for i, ts := range times {
		key := ts.String()
		if seen[key] {
			t.Fatalf("duplicate record time at index %d: %s", i, key)
		}
		seen[key] = true
	}
}

// TestTimesKeepIncreasingAcrossRestart checks that a reopened store never
// reuses or regresses a timestamp.
func TestTimesKeepIncreasingAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	first, err := store.New(dir, store.ShardMonthly)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	before, err := first.Append(context.Background(), "重启前")
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	second, err := store.New(dir, store.ShardMonthly)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	after, err := second.Append(context.Background(), "重启后")
	if err != nil {
		t.Fatalf("Append after reopen: %v", err)
	}

	if !after.Time.After(before.Time.Time) {
		t.Fatalf("time after restart %s is not after %s", after.Time, before.Time)
	}
}

// TestConcurrentAppendsProduceUniqueTimes is the race-detector exercise for the
// write lock.
func TestConcurrentAppendsProduceUniqueTimes(t *testing.T) {
	s := newStore(t, store.ShardMonthly)

	const workers, perWorker = 8, 25

	var wg sync.WaitGroup
	results := make([][]store.Time, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				rec, err := s.Append(context.Background(), "并发记录")
				if err != nil {
					t.Errorf("Append: %v", err)
					return
				}
				results[worker] = append(results[worker], rec.Time)
			}
		}(w)
	}
	wg.Wait()

	unique := make(map[string]bool, workers*perWorker)
	total := 0
	for _, batch := range results {
		for _, ts := range batch {
			key := ts.String()
			if unique[key] {
				t.Fatalf("duplicate time %s across concurrent writers", key)
			}
			unique[key] = true
			total++
		}
	}
	if want := workers * perWorker; total != want {
		t.Fatalf("collected %d times, want %d", total, want)
	}
}

// TestShardGranularities checks that each setting writes the documented file
// name for the current UTC period.
func TestShardGranularities(t *testing.T) {
	now := time.Now().UTC()

	cases := []struct {
		shard string
		name  string
	}{
		{shard: store.ShardMonthly, name: now.Format("2006-01") + ".jsonl"},
		{shard: store.ShardWeekly, name: isoWeekName(now)},
		{shard: store.ShardDaily, name: now.Format("2006-01-02") + ".jsonl"},
	}
	for _, tc := range cases {
		t.Run(tc.shard, func(t *testing.T) {
			s := newStore(t, tc.shard)
			if _, err := s.Append(context.Background(), "分片"); err != nil {
				t.Fatalf("Append: %v", err)
			}
			files := shardFiles(t, s.Dir())
			if len(files) != 1 {
				t.Fatalf("expected one shard, got %v", files)
			}
			if files[0] != tc.name {
				t.Fatalf("shard file = %q, want %q", files[0], tc.name)
			}
		})
	}
}

// isoWeekName mirrors the store's ISO-week naming.
func isoWeekName(t time.Time) string {
	year, week := t.ISOWeek()
	return time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC).Format("2006") +
		"-W" + twoDigits(week) + ".jsonl"
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

// TestUnknownShardIsAnError checks that a typo cannot silently change where
// records land.
func TestUnknownShardIsAnError(t *testing.T) {
	_, err := store.New(t.TempDir(), "fortnightly")
	if err == nil {
		t.Fatal("New should reject an unknown shard")
	}
	if !strings.Contains(err.Error(), "fortnightly") {
		t.Fatalf("error should name the offending shard: %v", err)
	}
}

// TestRecordsSurviveReopen checks that a successful write is really on disk.
func TestRecordsSurviveReopen(t *testing.T) {
	dir := t.TempDir()

	first, err := store.New(dir, store.ShardMonthly)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := first.Append(context.Background(), "确认成功的记录"); err != nil {
		t.Fatalf("Append: %v", err)
	}

	second, err := store.New(dir, store.ShardMonthly)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	records, err := second.Records(context.Background())
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records after reopen, want 1", len(records))
	}
	if got, want := records[0].Content, "确认成功的记录"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

// TestHalfLineIsIsolatedAndEarlierRecordsSurvive covers crash damage at the end
// of a shard.
func TestHalfLineIsIsolatedAndEarlierRecordsSurvive(t *testing.T) {
	dir := t.TempDir()

	first, err := store.New(dir, store.ShardMonthly)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := first.Append(context.Background(), "完好记录"); err != nil {
		t.Fatalf("Append: %v", err)
	}

	files := shardFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("expected one shard, got %v", files)
	}
	path := filepath.Join(dir, files[0])

	// Simulate a crash midway through the next line.
	handle, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open shard: %v", err)
	}
	if _, err := handle.WriteString(`{"version":1,"content":"被截断的`); err != nil {
		t.Fatalf("write partial line: %v", err)
	}
	handle.Close()

	reopened, err := store.New(dir, store.ShardMonthly)
	if err != nil {
		t.Fatalf("New should succeed after isolating a half line: %v", err)
	}

	records, err := reopened.Records(context.Background())
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records, want the one written before the crash", len(records))
	}
	if got, want := records[0].Content, "完好记录"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}

	// The damaged bytes must be preserved, not deleted.
	tails, err := os.ReadDir(filepath.Join(dir, "corrupt"))
	if err != nil {
		t.Fatalf("the damaged tail should be kept in corrupt/: %v", err)
	}
	if len(tails) != 1 {
		t.Fatalf("got %d isolated tails, want 1", len(tails))
	}
	isolated, err := os.ReadFile(filepath.Join(dir, "corrupt", tails[0].Name()))
	if err != nil {
		t.Fatalf("read isolated tail: %v", err)
	}
	if !strings.Contains(string(isolated), "被截断的") {
		t.Fatalf("isolated tail does not hold the damaged bytes: %q", isolated)
	}
}

// TestDamagedMiddleLineStopsStartup covers damage in the middle of a shard: the
// store must refuse to start rather than quietly drop data.
func TestDamagedMiddleLineStopsStartup(t *testing.T) {
	dir := t.TempDir()

	first, err := store.New(dir, store.ShardMonthly)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, content := range []string{"第一条", "第二条", "第三条"} {
		if _, err := first.Append(context.Background(), content); err != nil {
			t.Fatalf("Append(%q): %v", content, err)
		}
	}

	files := shardFiles(t, dir)
	path := filepath.Join(dir, files[0])
	lines := readLines(t, path)
	if len(lines) != 3 {
		t.Fatalf("expected 3 record lines, got %d", len(lines))
	}

	// Damage the middle line while keeping the file's line structure.
	lines[1] = `{"version":1,"content":`
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write damaged shard: %v", err)
	}

	_, err = store.New(dir, store.ShardMonthly)
	if err == nil {
		t.Fatal("New should refuse to start on a shard with a damaged middle line")
	}
	if !strings.Contains(err.Error(), files[0]) {
		t.Errorf("error should name the shard file: %v", err)
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error should name the line number: %v", err)
	}
}

// TestRecordsAreNewestFirst covers the documented ordering.
func TestRecordsAreNewestFirst(t *testing.T) {
	s := newStore(t, store.ShardMonthly)

	inputs := []string{"最旧", "中间", "最新"}
	for _, content := range inputs {
		if _, err := s.Append(context.Background(), content); err != nil {
			t.Fatalf("Append(%q): %v", content, err)
		}
	}

	records, err := s.Records(context.Background())
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if len(records) != len(inputs) {
		t.Fatalf("got %d records, want %d", len(records), len(inputs))
	}
	want := []string{"最新", "中间", "最旧"}
	for i, expected := range want {
		if records[i].Content != expected {
			t.Errorf("records[%d] = %q, want %q", i, records[i].Content, expected)
		}
	}
}

// TestEmptyStoreHasNoRecords covers the empty case.
func TestEmptyStoreHasNoRecords(t *testing.T) {
	s := newStore(t, store.ShardMonthly)
	records, err := s.Records(context.Background())
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("got %d records from an empty store, want 0", len(records))
	}
}

// TestCorruptDirectoryIsNotAShard checks that recovered tails never appear as
// records.
func TestCorruptDirectoryIsNotAShard(t *testing.T) {
	s := newStore(t, store.ShardMonthly)

	corrupt := filepath.Join(s.Dir(), "corrupt")
	if err := os.MkdirAll(corrupt, 0o755); err != nil {
		t.Fatalf("create corrupt dir: %v", err)
	}
	stray := `{"version":1,"content":"should not be read","time":"2026-11-04T15:00:00.123456789Z"}` + "\n"
	if err := os.WriteFile(filepath.Join(corrupt, "2026-11.jsonl.1.tail"), []byte(stray), 0o644); err != nil {
		t.Fatalf("write stray tail: %v", err)
	}

	records, err := s.Records(context.Background())
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("corrupt/ must not be searched; got %d records", len(records))
	}
}

// TestAppendRejectsCancelledContext keeps a cancelled request from writing.
func TestAppendRejectsCancelledContext(t *testing.T) {
	s := newStore(t, store.ShardMonthly)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := s.Append(ctx, "不该写入"); err == nil {
		t.Fatal("Append should fail on a cancelled context")
	}
	if files := shardFiles(t, s.Dir()); len(files) != 0 {
		t.Fatalf("a cancelled Append must not write: %v", files)
	}
}
