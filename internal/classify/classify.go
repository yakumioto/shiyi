// Package classify judges whether an input is a record, a query, or neither.
//
// The judge is a model, never a rule set. The model sees only the current
// input: classification never depends on conversation history.
package classify

import "context"

// Kind is the verdict about one input.
type Kind string

// The three possible verdicts. A mixed input is judged a query.
const (
	KindRecord       Kind = "record"
	KindQuery        Kind = "query"
	KindUnrecognized Kind = "unrecognized"
)

// Judge decides what an input is for.
type Judge interface {
	// Classify judges a single input.
	Classify(ctx context.Context, content string) (Kind, error)
}
