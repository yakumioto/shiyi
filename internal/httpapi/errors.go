package httpapi

import (
	"context"
	"errors"

	"shiyi/internal/answer"
	"shiyi/internal/llm"
	"shiyi/internal/search"
	"shiyi/internal/store"
)

// The predicates below name the kind of a failure. They exist so that the
// error mapping and the log line cannot drift apart: both ask the same
// questions of the same error.

// isModelUnavailable reports whether err means the model could not be
// reached or did not answer in time.
func isModelUnavailable(err error) bool {
	return errors.Is(err, llm.ErrUnavailable)
}

// isInvalidModelResponse reports whether err means the model answered with
// something that could not be understood.
func isInvalidModelResponse(err error) bool {
	return errors.Is(err, llm.ErrInvalidResponse)
}

// isInvalidCitation reports whether err means the model cited a record that
// this query did not return.
func isInvalidCitation(err error) bool {
	return errors.Is(err, answer.ErrInvalidCitation)
}

// isStorageFailed reports whether err means a write did not complete.
func isStorageFailed(err error) bool {
	return errors.Is(err, store.ErrStorageFailed)
}

// isSearchFailed reports whether err means the records could not be
// searched.
func isSearchFailed(err error) bool {
	return errors.Is(err, search.ErrSearchFailed)
}

// isTimedOut reports whether err is the deadline of the request running out.
func isTimedOut(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}
