// Package store keeps memory entries in append-only JSONL files.
//
// This file holds the frozen on-disk shape of a memory entry. The
// implementation of the store lives in the other files of this package.
package store

import (
	"encoding/json"
	"fmt"
	"time"
)

// TimeLayout is the canonical wire form of a record time: UTC, RFC3339, with
// exactly nine fractional digits. Fixing the width keeps the textual form
// ordered the same way as the moment it names.
const TimeLayout = "2006-01-02T15:04:05.000000000Z"

// Time is the server-generated timestamp of a memory entry. It is unique,
// strictly increasing, and doubles as the entry's identifier and as the
// anchor a citation points at.
type Time struct {
	time.Time
}

// NewTime returns t as a UTC timestamp truncated to nanosecond precision.
func NewTime(t time.Time) Time {
	return Time{t.UTC().Truncate(time.Nanosecond)}
}

// MarshalJSON writes the canonical form.
func (t Time) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.String())
}

// UnmarshalJSON parses the canonical form, and tolerates plain RFC3339Nano.
func (t *Time) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("decode record time: %w", err)
	}
	parsed, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return fmt.Errorf("parse record time %q: %w", s, err)
	}
	t.Time = parsed.UTC()
	return nil
}

// String returns the canonical form of t.
func (t Time) String() string {
	return t.UTC().Format(TimeLayout)
}

// Record is one immutable memory entry. Its JSON form has exactly three
// fields, in this order: version, content, time.
type Record struct {
	Version int    `json:"version"`
	Content string `json:"content"`
	Time    Time   `json:"time"`
}
