package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// maxResponseBytes bounds how much of a provider answer is read. A model that
// streams an endless body must not be able to exhaust the process memory.
const maxResponseBytes = 8 << 20 // 8 MiB

// Who owns the model and the temperature:
//
// A caller that names a model in its Request owns the request: the model and
// the temperature in the Request are sent as they are. A Request without a
// model is filled in from the Options the client was built with.
//
// This keeps the temperature a caller passes to classify.New or answer.New
// effective, while a bare client still behaves like a configured default.

// Options describes one OpenAI-compatible model endpoint.
//
// The API key is never stored in the configuration file; it is read at run
// time from an environment variable and handed over here.
type Options struct {
	// BaseURL is the API root, for example "https://api.deepseek.com" or
	// "https://host/v1". A trailing slash is allowed.
	BaseURL string

	// Model is the model name sent with every request.
	Model string

	// APIKey is the bearer token. It is only ever used for the
	// Authorization header and never logged.
	APIKey string

	// Timeout bounds a single round trip.
	Timeout time.Duration

	// Temperature is the value sent with every request.
	Temperature float64

	// HTTPClient is the transport used for the round trip. A nil value
	// means a client with no timeout of its own; the request timeout is
	// applied through the request context either way.
	HTTPClient *http.Client

	// Logger receives diagnostics. A nil value means the default logger.
	Logger *slog.Logger
}

// HTTPClient talks to an OpenAI-compatible Chat Completions endpoint.
//
// It never lets provider detail escape: errors carry a status-level message
// only, and the API key is used for the Authorization header only.
type HTTPClient struct {
	baseURL     string
	model       string
	apiKey      string
	timeout     time.Duration
	temperature float64
	http        *http.Client
	logger      *slog.Logger
}

// New returns a client for the given endpoint.
func New(opts Options) *HTTPClient {
	c := &HTTPClient{
		baseURL:     opts.BaseURL,
		model:       opts.Model,
		apiKey:      opts.APIKey,
		timeout:     opts.Timeout,
		temperature: opts.Temperature,
		http:        &http.Client{},
		logger:      slog.Default(),
	}
	if opts.HTTPClient != nil {
		c.http = opts.HTTPClient
	}
	if opts.Logger != nil {
		c.logger = opts.Logger
	}
	return c
}

// Complete performs one Chat Completions round trip.
//
// A request that names a model is self-contained: it supplies its own model
// and temperature, so a caller that configures them (the classifier and the
// answerer do) keeps full control. A request without a model falls back to
// the values the client was configured with.
func (c *HTTPClient) Complete(ctx context.Context, req Request) (*Response, error) {
	payload := req
	if payload.Model == "" {
		payload.Model = c.model
		payload.Temperature = c.temperature
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: 请求无法序列化", ErrInvalidResponse)
	}

	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), bytes.NewReader(body))
	if err != nil {
		c.logger.Error("模型请求无法构造", "错误", c.scrub(err.Error()))
		return nil, fmt.Errorf("%w: 模型服务地址无法使用", ErrUnavailable)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	res, err := c.http.Do(httpReq)
	if err != nil {
		c.logger.Warn("模型请求失败", "错误", c.scrub(err.Error()))
		// A cancelled or expired context still means the model could not be
		// reached; the cause stays wrapped so callers can tell them apart.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("%w: %w", ErrUnavailable, ctxErr)
		}
		return nil, fmt.Errorf("%w: 无法连接模型服务", ErrUnavailable)
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode > 299 {
		c.logger.Warn("模型服务返回错误状态", "状态码", res.StatusCode)
		return nil, fmt.Errorf("%w: 模型服务返回 HTTP %d", ErrUnavailable, res.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes+1))
	if err != nil {
		c.logger.Warn("模型响应读取失败", "错误", c.scrub(err.Error()))
		return nil, fmt.Errorf("%w: 模型响应无法读取", ErrInvalidResponse)
	}
	if int64(len(raw)) > maxResponseBytes {
		c.logger.Warn("模型响应超过大小上限", "上限", maxResponseBytes)
		return nil, fmt.Errorf("%w: 模型响应超过大小上限", ErrInvalidResponse)
	}

	var out Response
	if err := json.Unmarshal(raw, &out); err != nil {
		c.logger.Warn("模型响应无法解析", "错误", c.scrub(err.Error()))
		return nil, fmt.Errorf("%w: 模型响应无法解析", ErrInvalidResponse)
	}
	if len(out.Choices) == 0 {
		c.logger.Warn("模型响应没有可用结果")
		return nil, fmt.Errorf("%w: 模型响应没有可用结果", ErrInvalidResponse)
	}
	return &out, nil
}

// scrub removes the API key from a string that is about to be logged, so the
// key can never reach the log even if a provider echoes it back.
func (c *HTTPClient) scrub(s string) string {
	if c.apiKey == "" {
		return s
	}
	return strings.ReplaceAll(s, c.apiKey, "[redacted]")
}

// endpoint returns the chat completions URL of the configured base.
func (c *HTTPClient) endpoint() string {
	return strings.TrimRight(c.baseURL, "/") + "/chat/completions"
}

// withTimeout bounds the round trip by the configured timeout, keeping the
// caller's context as the parent so a cancellation propagates.
func (c *HTTPClient) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, c.timeout)
}
