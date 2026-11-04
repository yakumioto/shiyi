// Package answer turns a query into a cited answer.
//
// The model may search the records several times. Every statement in the
// answer has to rest on a record that was actually returned during this
// query; a citation to anything else is refused rather than served.
package answer

import (
	"context"
	"errors"
)

// Status tells the client how complete an answer is.
type Status string

// The possible statuses of an answer.
const (
	// StatusOK means the model answered within budget.
	StatusOK Status = "ok"

	// StatusBudgetExhausted means the search budget ran out and the answer
	// was produced from the evidence gathered so far. It may be partial.
	StatusBudgetExhausted Status = "budget_exhausted"

	// StatusNoMatch means the search turned up nothing, so the fixed
	// "no memory found" answer is returned and the model's own text is
	// discarded.
	StatusNoMatch Status = "no_match"
)

// NoMatchAnswer is the fixed answer returned when nothing was found. The
// model's own text is dropped in that case, so it can never answer from
// general knowledge instead of from the records.
const NoMatchAnswer = "没有找到相关记忆"

// ErrInvalidCitation means the model cited a record that was not returned by
// this query's searches.
var ErrInvalidCitation = errors.New("answer: citation outside of search results")

// Citation binds a statement in an answer to the record it rests on.
type Citation struct {
	Time    string `json:"time"`
	Content string `json:"content"`
}

// Result is an answer together with its evidence.
type Result struct {
	Answer    string
	Citations []Citation
	Status    Status
}

// Answerer answers a query.
type Answerer interface {
	// Answer searches the records and answers the query.
	Answer(ctx context.Context, query string) (Result, error)
}
