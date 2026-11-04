package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// maxLineBytes bounds one JSONL line. The API caps a request body at 1 MiB, so
// a record can never need much more than that.
const maxLineBytes = 2 << 20

// recover scans every shard at start-up, isolating an incomplete tail and
// restoring the clock from the newest record.
func (s *FS) recover() error {
	names, err := s.shardNames()
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := s.scanShard(filepath.Join(s.dir, name)); err != nil {
			return err
		}
	}
	return nil
}

// scanShard checks one shard file.
//
// The rule is the one the design appendix states: bytes after the last newline
// are an unfinished write and get moved aside, while a *complete* line that is
// not a record is real damage and stops the start-up instead of being dropped.
func (s *FS) scanShard(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("store: read shard %s: %w", filepath.Base(path), err)
	}
	if len(content) == 0 {
		return nil
	}

	lastNewline := lastNewlineIndex(content)
	completeEnd := lastNewline + 1 // 0 when there is no newline at all
	complete := content[:completeEnd]
	tail := content[completeEnd:]

	lineNo := 0
	for start := 0; start < len(complete); {
		end := indexByte(complete, '\n', start)
		if end < 0 {
			break
		}
		line := complete[start:end]
		lineNo++
		if len(line) > 0 {
			if !s.validLine(line) {
				return fmt.Errorf("%w: shard %s line %d is not a complete record",
					ErrCorrupt, filepath.Base(path), lineNo)
			}
			s.observe(line)
		}
		start = end + 1
	}

	if len(tail) == 0 {
		return nil
	}
	return s.isolateTail(path, tail, int64(completeEnd))
}

// isolateTail moves tail into the corrupt directory and truncates the shard
// back to size. A failure here is reported rather than ignored: leaving the
// damaged bytes in place would make every later start-up fail the same way.
func (s *FS) isolateTail(path string, tail []byte, size int64) error {
	dir := filepath.Join(s.dir, corruptDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("store: create corrupt directory: %w", err)
	}
	name := fmt.Sprintf("%s.%d.tail", filepath.Base(path), time.Now().UnixNano())
	if err := os.WriteFile(filepath.Join(dir, name), tail, 0o644); err != nil {
		return fmt.Errorf("store: isolate damaged tail of %s: %w", filepath.Base(path), err)
	}
	if err := os.Truncate(path, size); err != nil {
		return fmt.Errorf("store: truncate damaged shard %s: %w", filepath.Base(path), err)
	}
	return nil
}

// lastNewlineIndex returns the index of the final newline, or -1.
func lastNewlineIndex(content []byte) int {
	for i := len(content) - 1; i >= 0; i-- {
		if content[i] == '\n' {
			return i
		}
	}
	return -1
}

// validLine reports whether line is a complete record.
//
// A record has exactly three fields and a time; anything else in a shard is
// damage. Checking the field set here, rather than only that the JSON parses,
// is what stops a half-formed line from passing the start-up scan and being
// served later as if it were a memory.
func (s *FS) validLine(line []byte) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(line, &fields); err != nil {
		return false
	}
	if len(fields) != 3 {
		return false
	}
	for _, name := range []string{"version", "content", "time"} {
		if _, ok := fields[name]; !ok {
			return false
		}
	}

	var rec Record
	if err := json.Unmarshal(line, &rec); err != nil {
		return false
	}
	if rec.Version <= 0 {
		return false
	}
	return !rec.Time.IsZero()
}

// observe folds one record into the newest-seen clock.
func (s *FS) observe(line []byte) {
	var rec Record
	if err := json.Unmarshal(line, &rec); err != nil {
		return
	}
	if !s.lastOK || rec.Time.After(s.last.Time) {
		s.last = rec.Time
		s.lastOK = true
	}
}

// readShard reads every record of one shard. Records come back in file order.
func (s *FS) readShard(path string) ([]Record, error) {
	handle, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("store: open shard %s: %w", filepath.Base(path), err)
	}
	defer handle.Close()

	var records []Record
	lineNo := 0
	scanner := bufio.NewScanner(handle)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	for scanner.Scan() {
		lineNo++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec Record
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, fmt.Errorf("%w: shard %s line %d", ErrCorrupt, filepath.Base(path), lineNo)
		}
		records = append(records, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("store: read shard %s: %w", filepath.Base(path), err)
	}
	return records, nil
}

// indexByte is bytes.IndexByte, kept local to avoid an import for one call.
func indexByte(haystack []byte, needle byte, from int) int {
	for i := from; i < len(haystack); i++ {
		if haystack[i] == needle {
			return i
		}
	}
	return -1
}
