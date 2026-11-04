// Package api defines the HTTP wire contract of Shiyi.
//
// Everything in this package is part of the public surface between the server
// and its clients (CLI, Web, scripts). The values here are frozen: the request
// shape, the response shapes, the error codes and their HTTP status mapping
// come straight from the spec and must not drift.
package api

import "net/http"

// Kinds of responses to POST /messages.
const (
	KindRecord       = "record"
	KindQuery        = "query"
	KindUnrecognized = "unrecognized"
)

// Status of a query answer.
const (
	StatusOK              = "ok"
	StatusBudgetExhausted = "budget_exhausted"
	StatusNoMatch         = "no_match"
)

// Error codes. Every error response carries exactly one of these.
const (
	CodeInvalidRequest       = "invalid_request"
	CodeUnauthorized         = "unauthorized"
	CodeRequestTooLarge      = "request_too_large"
	CodeStorageFailed        = "storage_failed"
	CodeSearchFailed         = "search_failed"
	CodeInternalError        = "internal_error"
	CodeInvalidCitation      = "invalid_citation"
	CodeModelInvalidResponse = "model_invalid_response"
	CodeModelUnavailable     = "model_unavailable"
)

// MessageRequest is the body of POST /messages.
type MessageRequest struct {
	Content string `json:"content"`
}

// RecordResponse is returned with status 201 when the input was judged a record.
type RecordResponse struct {
	Kind    string `json:"kind"`
	Time    string `json:"time"`
	Version int    `json:"version"`
}

// Citation binds a statement in an answer to the record it rests on. The
// quoted original text travels with the citation so a client never has to
// issue a second request to see the evidence.
type Citation struct {
	Time    string `json:"time"`
	Content string `json:"content"`
}

// QueryResponse is returned with status 200 when the input was judged a query.
type QueryResponse struct {
	Kind      string     `json:"kind"`
	Answer    string     `json:"answer"`
	Citations []Citation `json:"citations"`
	Status    string     `json:"status"`
}

// UnrecognizedResponse is returned with status 200 when the input could not be
// judged as either a record or a query.
type UnrecognizedResponse struct {
	Kind string `json:"kind"`
}

// ErrorBody is the payload of an error response.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrorResponse is the shape of every non-2xx response.
type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

// HTTPStatusForCode maps an error code to the HTTP status it is returned with.
func HTTPStatusForCode(code string) int {
	switch code {
	case CodeInvalidRequest:
		return http.StatusBadRequest
	case CodeUnauthorized:
		return http.StatusUnauthorized
	case CodeRequestTooLarge:
		return http.StatusRequestEntityTooLarge
	case CodeStorageFailed, CodeSearchFailed, CodeInternalError:
		return http.StatusInternalServerError
	case CodeInvalidCitation, CodeModelInvalidResponse:
		return http.StatusBadGateway
	case CodeModelUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
