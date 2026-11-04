// Package store keeps memory entries in append-only JSONL files.
//
// The implementation of the store lives in the other files of this package;
// this file holds the interface other packages depend on.
package store

import (
	"context"
	"errors"
)

// Errors a store can report.
var (
	// ErrStorageFailed means a write or fsync did not complete. The entry
	// may or may not have reached the disk; the caller must treat the
	// write as failed.
	ErrStorageFailed = errors.New("store: write failed")

	// ErrCorrupt means a shard file holds a damaged line in the middle.
	// The store refuses to start rather than quietly dropping data.
	ErrCorrupt = errors.New("store: corrupt shard")
)

// Store appends memory entries to sharded JSONL files.
//
// A Store is safe for concurrent use.
type Store interface {
	// Append writes content as a new memory entry and returns it.
	Append(ctx context.Context, content string) (Record, error)

	// Records returns every stored record, newest first.
	Records(ctx context.Context) ([]Record, error)
}
