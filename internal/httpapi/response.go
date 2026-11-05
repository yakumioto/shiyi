package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"shiyi/internal/api"
)

// slogError returns a log attribute that describes err without leaking it.
//
// The detail of an error can carry the submitted text, the API key or the
// raw body the model answered with, so only the kind of the failure is
// logged.
func slogError(err error) slog.Attr {
	return slog.String("error", errorKind(err))
}

// errorKind names the kind of a failure without repeating its detail.
func errorKind(err error) string {
	switch {
	case err == nil:
		return ""
	case isTooLarge(err):
		return "request body too large"
	case isModelUnavailable(err):
		return "model unavailable"
	case isInvalidModelResponse(err):
		return "invalid model response"
	case isInvalidCitation(err):
		return "citation outside of the search results"
	case isStorageFailed(err):
		return "storage failed"
	case isSearchFailed(err):
		return "search failed"
	case isTimedOut(err):
		return "request timed out"
	default:
		return "internal error"
	}
}

// writeError answers with an error code and its message.
//
// The status code always comes from the frozen table of the API, so the code
// and the status can never drift apart.
func (s *Server) writeError(w http.ResponseWriter, code, message string) {
	s.writeJSON(w, api.HTTPStatusForCode(code), apiErrorResponse(code, message))
}

// writeJSON answers with the JSON encoding of v.
func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		s.logger.Error("encoding the response failed", slogError(err))
		body = []byte(`{"error":{"code":"internal_error","message":"内部错误"}}`)
		status = 500
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		s.logger.Warn("writing the response failed", slogError(err))
	}
}

// apiErrorResponse builds the error body of the API.
func apiErrorResponse(code, message string) api.ErrorResponse {
	return api.ErrorResponse{Error: api.ErrorBody{Code: code, Message: message}}
}

// withRequestLog logs one line per request.
//
// The line holds fixed method/route labels, status and duration. It does
// not hold the submitted content: the original text must not reach the log.
func (s *Server) withRequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.logger.Info("request",
			slog.String("method", logMethod(r.Method)),
			slog.String("path", logRoute(r.URL.Path)),
			slog.Int("status", rec.status),
			slog.Duration("duration", time.Since(start)),
		)
	})
}

// statusRecorder remembers the status code that was written.
type statusRecorder struct {
	http.ResponseWriter
	status  int
	written bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if !r.written {
		r.status = status
		r.written = true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.written {
		r.written = true
	}
	return r.ResponseWriter.Write(b)
}

// Only fixed route/method labels enter logs. Unknown paths and verbs are
// attacker-controlled and may contain credentials or private original text.
func logRoute(path string) string {
	switch path {
	case "/", "/app.js", "/style.css", "/healthz", "/messages", api.PathAuthSession, api.PathAuthLogin, api.PathAuthLogout, api.PathAuthLogoutAll:
		return path
	default:
		return "unmatched"
	}
}
func logMethod(method string) string {
	switch method {
	case "GET", "POST", "HEAD", "OPTIONS", "PUT", "DELETE", "PATCH":
		return method
	default:
		return "other"
	}
}
