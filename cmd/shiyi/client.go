package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// apiRecord is the body the server returns for a recorded input.
type apiRecord struct {
	Kind    string `json:"kind"`
	Time    string `json:"time"`
	Version int    `json:"version"`
}

// apiCitation binds a statement in an answer to the record it rests on.
type apiCitation struct {
	Time    string `json:"time"`
	Content string `json:"content"`
}

// apiQuery is the body the server returns for a query.
type apiQuery struct {
	Kind      string        `json:"kind"`
	Answer    string        `json:"answer"`
	Citations []apiCitation `json:"citations"`
	Status    string        `json:"status"`
}

// apiUnrecognized is the body the server returns for an input it could not
// judge as either a record or a query.
type apiUnrecognized struct {
	Kind string `json:"kind"`
}

// apiError is the error code and message of an error response.
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// apiErrorEnvelope is the envelope every non-2xx response carries.
type apiErrorEnvelope struct {
	Error apiError `json:"error"`
}

// apiMessageRequest is the request body of POST /messages.
type apiMessageRequest struct {
	Content string `json:"content"`
}

// Response is the normalized shape of one answer from the server.
//
// Exactly one of the payload fields is set: Err for a failure, otherwise
// Record, Query or Unrecognized depending on what the server judged the input
// to be.
type Response struct {
	// Kind is "record", "query" or "unrecognized"; empty for an error.
	Kind string
	// Record is set when the input was recorded.
	Record *apiRecord
	// Query is set when the input was answered.
	Query *apiQuery
	// Unrecognized is set when the input could not be judged.
	Unrecognized *apiUnrecognized
	// Err is set when the server refused or failed to answer.
	Err *apiError
}

// Client talks to a Shiyi service.
//
// The client holds no state between calls: no cache, no history, no retry.
type Client struct {
	// BaseURL is the root address of the service, e.g. http://127.0.0.1:8787.
	BaseURL string
	// HTTPClient performs the requests; nil means http.DefaultClient.
	HTTPClient *http.Client
	// Token is the bearer token to authenticate with; empty means no
	// Authorization header is sent.
	Token string
}

// Send posts one message to the service and returns the normalized answer.
//
// A non-2xx answer is turned into a Response carrying the server's error code
// and message rather than into a Go error, so that the caller can print the
// code the service reported.
func (c *Client) Send(ctx context.Context, content string) (*Response, error) {
	body, err := json.Marshal(apiMessageRequest{Content: content})
	if err != nil {
		return nil, fmt.Errorf("编码请求失败: %w", err)
	}

	endpoint := strings.TrimSuffix(c.BaseURL, "/") + "/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求服务失败: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &Response{Err: parseAPIError(resp.StatusCode, payload)}, nil
	}

	return parseResponse(payload)
}

// parseAPIError reads the error envelope of a failed response.
//
// When the body cannot be understood, the error still names the HTTP status so
// that the message stays actionable.
func parseAPIError(status int, payload []byte) *apiError {
	var envelope apiErrorEnvelope
	if err := json.Unmarshal(payload, &envelope); err == nil && envelope.Error.Code != "" {
		return &apiError{
			Code:    envelope.Error.Code,
			Message: envelope.Error.Message,
		}
	}

	message := strings.TrimSpace(string(payload))
	if message == "" {
		message = http.StatusText(status)
	}
	return &apiError{
		Code:    fmt.Sprintf("http_%d", status),
		Message: fmt.Sprintf("服务返回 HTTP %d: %s", status, message),
	}
}

// parseResponse turns a successful body into a normalized Response.
func parseResponse(payload []byte) (*Response, error) {
	var probe struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(payload, &probe); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	switch probe.Kind {
	case "record":
		var record apiRecord
		if err := json.Unmarshal(payload, &record); err != nil {
			return nil, fmt.Errorf("解析记录响应失败: %w", err)
		}
		return &Response{Kind: record.Kind, Record: &record}, nil
	case "query":
		var query apiQuery
		if err := json.Unmarshal(payload, &query); err != nil {
			return nil, fmt.Errorf("解析查询响应失败: %w", err)
		}
		return &Response{Kind: query.Kind, Query: &query}, nil
	case "unrecognized":
		var unrecognized apiUnrecognized
		if err := json.Unmarshal(payload, &unrecognized); err != nil {
			return nil, fmt.Errorf("解析无法识别响应失败: %w", err)
		}
		return &Response{Kind: unrecognized.Kind, Unrecognized: &unrecognized}, nil
	case "":
		return nil, fmt.Errorf("响应缺少 kind 字段")
	default:
		return nil, fmt.Errorf("未知响应类型 %q", probe.Kind)
	}
}
