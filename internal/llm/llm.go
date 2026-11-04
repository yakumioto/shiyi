// Package llm talks to an OpenAI-compatible Chat Completions endpoint.
//
// The types here are the frozen boundary between Shiyi and whichever model
// provider the user configured. Everything above this package works in terms
// of these types and never touches HTTP directly.
package llm

import (
	"context"
	"errors"
)

// Sentinel errors. Callers map these onto the API error codes; the underlying
// provider error is never surfaced to a client, because it can carry the
// request payload or the API key.
var (
	// ErrUnavailable means the model could not be reached, timed out, or
	// answered with a non-2xx status.
	ErrUnavailable = errors.New("llm: model unavailable")

	// ErrInvalidResponse means the model answered with a 2xx status but the
	// payload could not be understood.
	ErrInvalidResponse = errors.New("llm: invalid model response")
)

// Role is the role of a message in a conversation.
type Role string

// The roles used by Shiyi.
const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is one entry of a conversation.
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// ToolCall is a tool invocation requested by the model.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall names the tool and carries its raw JSON arguments.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Tool describes a tool the model may call.
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction is the schema of a callable tool.
type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ToolChoice constrains how the model may use the offered tools.
type ToolChoice struct {
	Type     string `json:"type"`
	Function *struct {
		Name string `json:"name"`
	} `json:"function,omitempty"`
}

// ResponseFormat asks the model to answer with JSON. Providers that support
// it honour the request; providers that do not ignore the field.
type ResponseFormat struct {
	Type string `json:"type"`
}

// The response format that constrains the answer to a JSON object.
const ResponseFormatJSONObject = "json_object"

// Request is a Chat Completions request.
type Request struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	Temperature    float64         `json:"temperature"`
	Tools          []Tool          `json:"tools,omitempty"`
	ToolChoice     *ToolChoice     `json:"tool_choice,omitempty"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
}

// Response is the part of a Chat Completions answer Shiyi reads.
type Response struct {
	ID      string   `json:"id"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
}

// Choice is one completion candidate.
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Client is the seam every model call goes through. Tests inject a fake
// implementation; the real one speaks HTTP.
type Client interface {
	// Complete performs one Chat Completions round trip.
	Complete(ctx context.Context, req Request) (*Response, error)
}

// Choice returns the first choice of resp, or ErrInvalidResponse when the
// model answered with no usable choice.
func (r *Response) Choice() (Choice, error) {
	if r == nil || len(r.Choices) == 0 {
		return Choice{}, ErrInvalidResponse
	}
	return r.Choices[0], nil
}

// ToolCallFor returns the first tool call of the given name in the choice.
func ToolCallFor(c Choice, name string) (ToolCall, bool) {
	for _, tc := range c.Message.ToolCalls {
		if tc.Function.Name == name {
			return tc, true
		}
	}
	return ToolCall{}, false
}
