package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"shiyi/internal/api"
	"shiyi/internal/classify"
)

// handleMessages serves POST /messages.
//
// The order of the checks matters. The token is checked before the body is
// read, so an unauthenticated client cannot make the server buffer a body.
// The body limit comes before the JSON is decoded, so an oversized body is
// refused without being parsed. Only then is the input judged.
func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	content, ok := s.readContent(w, r)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.requestTimeout)
	defer cancel()

	kind, err := s.judge.Classify(ctx, content)
	if err != nil {
		s.logger.Error("classifying the input failed", slogError(err))
		s.writeErrorForError(w, err)
		return
	}

	switch kind {
	case classify.KindRecord:
		s.handleRecord(ctx, w, content)
	case classify.KindQuery:
		s.handleQuery(ctx, w, content)
	case classify.KindUnrecognized:
		s.writeJSON(w, http.StatusOK, api.UnrecognizedResponse{Kind: api.KindUnrecognized})
	default:
		// A judge that returns a verdict outside of the three known kinds
		// has broken its contract. The input is not thrown away silently:
		// the client is told that the request failed.
		s.logger.Error("the judge returned an unknown verdict", slogString("kind", string(kind)))
		s.writeError(w, api.CodeInternalError, "内部错误")
	}
}

// readContent reads the submitted content out of the request body.
//
// It returns the content on success. On failure it has already written the
// error response and returns false.
//
// The original text is kept as it was submitted. Trimming is only used to
// decide whether the input is empty; the text that reaches the judge and the
// store is the text the user sent.
func (s *Server) readContent(w http.ResponseWriter, r *http.Request) (string, bool) {
	body := http.MaxBytesReader(w, r.Body, s.maxBodyBytes)
	defer func() { _ = body.Close() }()

	raw, err := io.ReadAll(body)
	if err != nil {
		if isTooLarge(err) {
			s.writeError(w, api.CodeRequestTooLarge, "请求体超过上限")
			return "", false
		}
		s.writeError(w, api.CodeInvalidRequest, "读取请求体失败")
		return "", false
	}

	var req api.MessageRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		s.writeError(w, api.CodeInvalidRequest, "请求体不是合法的 JSON")
		return "", false
	}

	if strings.TrimSpace(req.Content) == "" {
		s.writeError(w, api.CodeInvalidRequest, "content 不能为空")
		return "", false
	}

	return req.Content, true
}

// handleRecord stores the input and reports the record time.
func (s *Server) handleRecord(ctx context.Context, w http.ResponseWriter, content string) {
	rec, err := s.store.Append(ctx, content)
	if err != nil {
		s.logger.Error("storing the record failed", slogError(err))
		s.writeErrorForError(w, err)
		return
	}

	s.writeJSON(w, http.StatusCreated, api.RecordResponse{
		Kind:    api.KindRecord,
		Time:    rec.Time.String(),
		Version: rec.Version,
	})
}

// handleQuery answers the input from the records.
//
// A query never writes anything: the answer travels with the response and is
// not kept anywhere.
func (s *Server) handleQuery(ctx context.Context, w http.ResponseWriter, content string) {
	res, err := s.answerer.Answer(ctx, content)
	if err != nil {
		s.logger.Error("answering the query failed", slogError(err))
		s.writeErrorForError(w, err)
		return
	}

	citations := make([]api.Citation, 0, len(res.Citations))
	for _, c := range res.Citations {
		citations = append(citations, api.Citation{Time: c.Time, Content: c.Content})
	}

	s.writeJSON(w, http.StatusOK, api.QueryResponse{
		Kind:      api.KindQuery,
		Answer:    res.Answer,
		Citations: citations,
		Status:    string(res.Status),
	})
}

// isTooLarge reports whether err is the body limit being hit.
func isTooLarge(err error) bool {
	var maxBytesErr *http.MaxBytesError
	return errors.As(err, &maxBytesErr)
}

// writeErrorForError answers with the error code that belongs to err.
//
// The mapping follows the frozen error table. The message is a fixed,
// hand-written sentence: it never repeats the text of the underlying error,
// because a provider error can carry the request payload or the API key.
func (s *Server) writeErrorForError(w http.ResponseWriter, err error) {
	switch {
	case isModelUnavailable(err):
		s.writeError(w, api.CodeModelUnavailable, "模型暂时不可用")
	case isInvalidModelResponse(err):
		s.writeError(w, api.CodeModelInvalidResponse, "模型返回的内容无法解析")
	case isInvalidCitation(err):
		s.writeError(w, api.CodeInvalidCitation, "引用不在本次检索结果中")
	case isStorageFailed(err):
		s.writeError(w, api.CodeStorageFailed, "写入记录失败")
	case isSearchFailed(err):
		s.writeError(w, api.CodeSearchFailed, "检索记录失败")
	case isTimedOut(err):
		s.writeError(w, api.CodeModelUnavailable, "模型暂时不可用")
	default:
		s.writeError(w, api.CodeInternalError, "内部错误")
	}
}
