package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// FakeClient is a scripted Client for tests and fixtures.
//
// It answers in one of three ways, checked in this order:
//
//  1. a pending one-shot error set with SetErrorOnce;
//  2. a persistent error set with SetError;
//  3. the response registered for the tool the conversation is waiting on,
//     and otherwise the next scripted turn.
//
// When the scripted turns run out the client reports ErrUnavailable, so a test
// that makes more calls than it scripted fails loudly instead of hanging.
//
// A FakeClient is safe for concurrent use.
type FakeClient struct {
	mu         sync.Mutex
	turns      []*Response
	next       int
	persistent error
	once       error
	tool       map[string]*Response
	requests   []Request
	calls      int
}

// NewFakeClient returns a fake client that answers with the given turns in
// order. Once the turns are used up it reports ErrUnavailable.
//
// A nil turn is skipped, so a fixture can leave a gap without ending the
// script early.
func NewFakeClient(turns ...*Response) *FakeClient {
	kept := make([]*Response, 0, len(turns))
	for _, turn := range turns {
		if turn != nil {
			kept = append(kept, turn)
		}
	}
	return &FakeClient{
		turns: kept,
		tool:  map[string]*Response{},
	}
}

// SetError makes every following call fail with err. A nil error clears it.
func (f *FakeClient) SetError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.persistent = err
}

// SetErrorOnce makes the next call fail with err; later calls behave as if it
// was never set.
func (f *FakeClient) SetErrorOnce(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.once = err
}

// SetToolResponse registers the answer returned once the conversation waits
// for a result of the named tool.
func (f *FakeClient) SetToolResponse(tool string, resp *Response) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tool[tool] = resp
}

// Requests returns the requests the client received, in order.
func (f *FakeClient) Requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.requests...)
}

// CallCount returns how many times the client was called.
func (f *FakeClient) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// Complete returns the scripted answer for this call.
func (f *FakeClient) Complete(ctx context.Context, req Request) (*Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.requests = append(f.requests, req)
	f.calls++

	if err := ctx.Err(); err != nil {
		// Same convention as the real client: a cancelled context is an
		// unreachable model.
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}

	if f.once != nil {
		err := f.once
		f.once = nil
		return nil, err
	}

	if f.persistent != nil {
		return nil, f.persistent
	}

	if name, ok := pendingTool(req); ok {
		if resp, found := f.tool[name]; found {
			return resp, nil
		}
	}

	if f.next < len(f.turns) {
		resp := f.turns[f.next]
		f.next++
		return resp, nil
	}

	return nil, ErrUnavailable
}

// pendingTool reports the tool whose result the conversation is waiting for:
// the assistant asked for it and a tool message answered it. The tool message
// is matched by call id when it carries one, and by tool name otherwise.
func pendingTool(req Request) (string, bool) {
	asked := map[string]string{}
	for _, msg := range req.Messages {
		if msg.Role == RoleAssistant {
			for _, tc := range msg.ToolCalls {
				asked[tc.Function.Name] = tc.ID
			}
		}
	}
	if len(asked) == 0 {
		return "", false
	}

	for _, msg := range req.Messages {
		if msg.Role != RoleTool {
			continue
		}
		if msg.ToolCallID != "" {
			for name, id := range asked {
				if id == msg.ToolCallID {
					return name, true
				}
			}
			continue
		}
		if _, ok := asked[msg.Name]; ok {
			return msg.Name, true
		}
	}
	return "", false
}

// TextResponse builds a response whose answer is the given text.
func TextResponse(content string) *Response {
	return &Response{
		ID:    "fake-response",
		Model: "fake-model",
		Choices: []Choice{{
			Index: 0,
			Message: Message{
				Role:    RoleAssistant,
				Content: content,
			},
			FinishReason: "stop",
		}},
	}
}

// ToolCallResponse builds a response that asks for one tool call. arguments
// stays the raw JSON string the provider would have sent.
func ToolCallResponse(id, name, args string) *Response {
	return &Response{
		ID:    "fake-response",
		Model: "fake-model",
		Choices: []Choice{{
			Index: 0,
			Message: Message{
				Role: RoleAssistant,
				ToolCalls: []ToolCall{{
					ID:   id,
					Type: "function",
					Function: FunctionCall{
						Name:      name,
						Arguments: args,
					},
				}},
			},
			FinishReason: "tool_calls",
		}},
	}
}

// ParseResponse decodes a provider answer as it arrived on the wire. It lets
// fixtures keep the exact bytes a provider sent.
func ParseResponse(raw string) (*Response, error) {
	var out Response
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("%w: 模型响应无法解析", ErrInvalidResponse)
	}
	return &out, nil
}

// MustParseResponse is ParseResponse for fixtures, where a malformed answer
// is a bug in the test data rather than a case to handle.
func MustParseResponse(raw string) *Response {
	resp, err := ParseResponse(raw)
	if err != nil {
		panic(err)
	}
	return resp
}
