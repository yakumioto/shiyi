// Package search finds memory entries by literal substring match.
//
// Matching is deliberately literal: a keyword hits a record when it occurs in
// the record's original text, ignoring case. Nothing is indexed, nothing is
// embedded, nothing is inferred.
package search

import (
	"context"

	"shiyi/internal/store"
)

// DefaultLimit is the number of records returned when the caller does not
// ask for a specific amount.
const DefaultLimit = 10

// MaxLimit is the largest number of records a single search may return.
const MaxLimit = 20

// MaxKeywords is the largest number of keywords one search may carry.
const MaxKeywords = 8

// MaxKeywordLen is the longest single keyword accepted, in bytes.
const MaxKeywordLen = 128

// Searcher finds records.
type Searcher interface {
	// Search returns the records whose content contains any of the
	// keywords, newest first.
	Search(ctx context.Context, keywords []string, limit int) ([]store.Record, error)
}
