package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Shard granularities accepted by New.
const (
	ShardMonthly = "monthly"
	ShardWeekly  = "weekly"
	ShardDaily   = "daily"
)

// corruptDir is the sub-directory that holds recovered half-lines. It is never
// searched and never treated as a shard.
const corruptDir = "corrupt"

// shardSuffix is the extension of every shard file.
const shardSuffix = ".jsonl"

// FS is the store implementation behind the Store interface.
type FS struct {
	dir    string
	shard  string
	now    func() time.Time
	mu     sync.Mutex
	last   Time
	lastOK bool
}

// New opens the store rooted at dataDir, recovering any crash damage and
// restoring the clock so that record times keep increasing across restarts.
//
// shard is one of "monthly", "weekly" or "daily". An unknown value is an
// error rather than a silent fallback, so a typo cannot quietly change where
// records land.
func New(dataDir string, shard string) (*FS, error) {
	if !validShard(shard) {
		return nil, fmt.Errorf("store: unknown shard %q, want one of monthly, weekly, daily", shard)
	}
	if strings.TrimSpace(dataDir) == "" {
		return nil, fmt.Errorf("store: data directory must not be empty")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("store: create data directory: %w", err)
	}

	s := &FS{dir: dataDir, shard: shard, now: time.Now}

	if err := s.recover(); err != nil {
		return nil, err
	}
	return s, nil
}

// Dir returns the directory the store writes to.
func (s *FS) Dir() string { return s.dir }

// Append writes content as a new memory entry.
//
// The record time is the later of the wall clock and one nanosecond after the
// previous record, so times are strictly increasing and unique even when the
// system clock stands still or steps backwards.
func (s *FS) Append(ctx context.Context, content string) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	rec := Record{Version: 1, Content: content, Time: s.nextTime()}

	line, err := json.Marshal(rec)
	if err != nil {
		return Record{}, fmt.Errorf("%w: encode record: %v", ErrStorageFailed, err)
	}
	line = append(line, '\n')

	name := s.shardName(rec.Time)
	path := filepath.Join(s.dir, name)

	created, err := appendLine(path, line)
	if err != nil {
		return Record{}, fmt.Errorf("%w: %v", ErrStorageFailed, err)
	}
	// A brand-new shard file needs its directory entry flushed too, otherwise
	// the file itself can vanish after a crash even though its bytes survived.
	if created {
		if err := syncDir(s.dir); err != nil {
			return Record{}, fmt.Errorf("%w: %v", ErrStorageFailed, err)
		}
	}

	return rec, nil
}

// Records returns every stored record, newest first.
func (s *FS) Records(ctx context.Context) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	names, err := s.shardNames()
	if err != nil {
		return nil, err
	}

	var all []Record
	for _, name := range names {
		records, err := s.readShard(filepath.Join(s.dir, name))
		if err != nil {
			return nil, err
		}
		all = append(all, records...)
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].Time.After(all[j].Time.Time)
	})
	return all, nil
}

// nextTime returns the next record time. The caller holds the lock.
func (s *FS) nextTime() Time {
	now := NewTime(s.now())
	if !s.lastOK {
		s.last, s.lastOK = now, true
		return now
	}
	if !now.After(s.last.Time) {
		now = NewTime(s.last.Add(time.Nanosecond))
	}
	s.last = now
	return now
}

// shardName returns the shard file name for t.
func (s *FS) shardName(t Time) string {
	utc := t.UTC()
	switch s.shard {
	case ShardDaily:
		return utc.Format("2006-01-02") + shardSuffix
	case ShardWeekly:
		year, week := utc.ISOWeek()
		return fmt.Sprintf("%04d-W%02d%s", year, week, shardSuffix)
	default:
		return utc.Format("2006-01") + shardSuffix
	}
}

// shardNames lists the shard files at the top of the data directory, sorted.
// A missing directory is not an error; an unreadable one is.
func (s *FS) shardNames() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: list data directory: %w", err)
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), shardSuffix) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// appendLine appends line to path, syncing it before returning. The second
// return value reports whether the file was created.
func appendLine(path string, line []byte) (bool, error) {
	_, statErr := os.Stat(path)
	created := os.IsNotExist(statErr)

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return false, fmt.Errorf("open shard: %w", err)
	}
	if _, err := file.Write(line); err != nil {
		file.Close()
		return false, fmt.Errorf("write record: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return false, fmt.Errorf("sync record: %w", err)
	}
	if err := file.Close(); err != nil {
		return false, fmt.Errorf("close shard: %w", err)
	}
	return created, nil
}

// syncDir flushes a directory entry so a freshly created file survives a crash.
func syncDir(dir string) error {
	handle, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open data directory: %w", err)
	}
	defer handle.Close()
	if err := handle.Sync(); err != nil {
		return fmt.Errorf("sync data directory: %w", err)
	}
	return nil
}

// validShard reports whether shard is a supported granularity.
func validShard(shard string) bool {
	switch shard {
	case ShardMonthly, ShardWeekly, ShardDaily:
		return true
	}
	return false
}
