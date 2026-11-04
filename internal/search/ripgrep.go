package search

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"shiyi/internal/store"
)

// Errors a search can report.
var (
	// ErrInvalidArguments means the keywords or the limit are outside the
	// range a search accepts. The caller turns this into a message for the
	// model rather than into a failed query.
	ErrInvalidArguments = errors.New("search: invalid arguments")

	// ErrSearchFailed means ripgrep could not be run, or a shard file could
	// not be read. The caller maps this onto the search_failed error code.
	ErrSearchFailed = errors.New("search: search failed")
)

// maxRecordBytes bounds one shard line. A record holds the original text of
// an input, which the API caps at one mebibyte.
const maxRecordBytes = 1<<20 + 1<<16

// Ripgrep finds records by pre-filtering shard files with ripgrep and then
// matching the record's original text in Go.
type Ripgrep struct {
	dataDir string
}

// New returns a Searcher over the shard files in dataDir.
//
// The data directory is the only thing the searcher needs; it never reads
// the configuration, so that no package has to depend on another.
func New(dataDir string) Searcher {
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		abs = dataDir
	}
	return &Ripgrep{dataDir: abs}
}

// Search returns the records whose original text contains any of the
// keywords, newest first.
//
// The keywords are trimmed, deduplicated and lowercased before they are
// used. A keyword hits a record when it occurs in the record's original
// text, ignoring case; it never hits on the record's time or on the shape
// of the stored JSON.
func (s *Ripgrep) Search(ctx context.Context, keywords []string, limit int) ([]store.Record, error) {
	terms, err := normalizeKeywords(keywords)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}

	shards, err := s.shardFiles()
	if err != nil {
		return nil, err
	}
	if len(shards) == 0 {
		return []store.Record{}, nil
	}

	candidates, err := s.prefilter(ctx, shards, terms)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return []store.Record{}, nil
	}

	records, err := readRecords(candidates)
	if err != nil {
		return nil, err
	}

	return selectRecords(records, terms, limit), nil
}

// shardFiles lists the shard files of the data directory.
//
// Only the top level of the data directory is searched, and only files
// ending in .jsonl are shards; a directory such as corrupt/ holds the
// damaged leftovers of a crash and is never a source of evidence.
func (s *Ripgrep) shardFiles() ([]string, error) {
	entries, err := os.ReadDir(s.dataDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Nothing has been written yet, so there is nothing to search.
			return nil, nil
		}
		return nil, fmt.Errorf("%w: read data directory: %v", ErrSearchFailed, err)
	}

	var shards []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		shards = append(shards, filepath.Join(s.dataDir, entry.Name()))
	}
	sort.Strings(shards)
	return shards, nil
}

// prefilter asks ripgrep which shard files hold any of the terms.
//
// Ripgrep reads the raw lines of the file, so it also matches a term that
// occurs in the time or in the JSON structure; the Go pass that follows
// throws those away. What ripgrep does buy is not reading every shard in
// full.
func (s *Ripgrep) prefilter(ctx context.Context, shards []string, terms []string) ([]string, error) {
	rg, err := exec.LookPath("rg")
	if err != nil {
		return nil, fmt.Errorf("%w: ripgrep is not installed: %v", ErrSearchFailed, err)
	}

	hits := make(map[string]bool)
	for _, term := range terms {
		// The shard files are named explicitly so that no ignore rule
		// (a .gitignore in the data directory, for instance) can hide a
		// record from the search.
		args := []string{"-l", "-F", "-i", "--no-messages", "--max-depth", "1", "-g", "*.jsonl", "-e", term}
		args = append(args, shards...)

		cmd := exec.CommandContext(ctx, rg, args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			var exitErr *exec.ExitError
			switch {
			case errors.As(err, &exitErr) && exitErr.ExitCode() == 1:
				// No file holds the term. That is an ordinary answer.
				continue
			case errors.As(err, &exitErr):
				return nil, fmt.Errorf("%w: ripgrep exited with %d", ErrSearchFailed, exitErr.ExitCode())
			case ctx.Err() != nil:
				return nil, fmt.Errorf("%w: %v", ErrSearchFailed, ctx.Err())
			default:
				return nil, fmt.Errorf("%w: run ripgrep: %v", ErrSearchFailed, err)
			}
		}

		for _, line := range strings.Split(string(out), "\n") {
			path := strings.TrimSuffix(strings.TrimSuffix(line, "\r"), "\n")
			if path == "" {
				continue
			}
			abs, err := filepath.Abs(path)
			if err != nil {
				abs = path
			}
			hits[filepath.Clean(abs)] = true
		}
	}

	var candidates []string
	for _, shard := range shards {
		if hits[filepath.Clean(shard)] {
			candidates = append(candidates, shard)
		}
	}
	return candidates, nil
}

// readRecords parses the given shard files.
//
// A line that is not a valid memory entry is an error: a shard that cannot
// be read must not be silently reduced to the records that happen to parse,
// because that would quietly drop evidence.
func readRecords(paths []string) ([]store.Record, error) {
	var records []store.Record
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("%w: open shard: %v", ErrSearchFailed, err)
		}

		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 0, 64*1024), maxRecordBytes)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var record store.Record
			if err := json.Unmarshal([]byte(line), &record); err != nil {
				file.Close()
				return nil, fmt.Errorf("%w: %s: %v", ErrSearchFailed, filepath.Base(path), err)
			}
			records = append(records, record)
		}
		err = scanner.Err()
		file.Close()
		if err != nil {
			return nil, fmt.Errorf("%w: read shard: %v", ErrSearchFailed, err)
		}
	}
	return records, nil
}

// selectRecords keeps the records whose original text holds a term, drops
// the duplicates that share a record time, and returns the newest ones.
func selectRecords(records []store.Record, terms []string, limit int) []store.Record {
	hits := make([]store.Record, 0, len(records))
	seen := make(map[string]bool, len(records))
	for _, record := range records {
		if !matchesAny(record.Content, terms) {
			continue
		}
		key := record.Time.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		hits = append(hits, record)
	}

	sort.SliceStable(hits, func(i, j int) bool {
		return hits[i].Time.After(hits[j].Time.Time)
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// matchesAny reports whether the original text holds any of the terms. The
// terms are already lowercased.
func matchesAny(content string, terms []string) bool {
	lowered := strings.ToLower(content)
	for _, term := range terms {
		if strings.Contains(lowered, term) {
			return true
		}
	}
	return false
}

// normalizeKeywords trims, drops the empty keywords, removes the duplicates
// and checks the result against the accepted range.
func normalizeKeywords(keywords []string) ([]string, error) {
	seen := make(map[string]bool, len(keywords))
	terms := make([]string, 0, len(keywords))
	for _, keyword := range keywords {
		trimmed := strings.TrimSpace(keyword)
		if trimmed == "" {
			continue
		}
		if len(trimmed) > MaxKeywordLen {
			return nil, fmt.Errorf("%w: a keyword is longer than %d bytes", ErrInvalidArguments, MaxKeywordLen)
		}
		lowered := strings.ToLower(trimmed)
		if seen[lowered] {
			continue
		}
		seen[lowered] = true
		terms = append(terms, lowered)
	}

	if len(terms) == 0 {
		return nil, fmt.Errorf("%w: no keyword given", ErrInvalidArguments)
	}
	if len(terms) > MaxKeywords {
		return nil, fmt.Errorf("%w: more than %d keywords given", ErrInvalidArguments, MaxKeywords)
	}
	return terms, nil
}
