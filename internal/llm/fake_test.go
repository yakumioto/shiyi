package llm

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestFakeClientAnswersTurnsInOrder(t *testing.T) {
	first := TextResponse("first")
	second := TextResponse("second")

	fake := NewFakeClient(first, second)

	resp, err := fake.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp != first {
		t.Errorf("first response = %#v, want %#v", resp, first)
	}

	resp, err = fake.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "again"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp != second {
		t.Errorf("second response = %#v, want %#v", resp, second)
	}
}

func TestFakeClientReportsUnavailableWhenTurnsRunOut(t *testing.T) {
	fake := NewFakeClient(TextResponse("only"))

	if _, err := fake.Complete(context.Background(), Request{}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	_, err := fake.Complete(context.Background(), Request{})
	if err == nil {
		t.Fatal("Complete: want error, got nil")
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("Complete error = %v, want %v", err, ErrUnavailable)
	}
}

func TestFakeClientReturnsConfiguredError(t *testing.T) {
	fake := NewFakeClient()
	fake.SetError(ErrInvalidResponse)

	_, err := fake.Complete(context.Background(), Request{})
	if err == nil {
		t.Fatal("Complete: want error, got nil")
	}
	if !errors.Is(err, ErrInvalidResponse) {
		t.Errorf("Complete error = %v, want %v", err, ErrInvalidResponse)
	}

	// A persistent error keeps failing every call.
	if _, err := fake.Complete(context.Background(), Request{}); !errors.Is(err, ErrInvalidResponse) {
		t.Errorf("second Complete error = %v, want %v", err, ErrInvalidResponse)
	}
}

func TestFakeClientErrorOnceAffectsOnlyNextCall(t *testing.T) {
	fake := NewFakeClient(TextResponse("ok"))
	fake.SetErrorOnce(ErrUnavailable)

	if _, err := fake.Complete(context.Background(), Request{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("first Complete error = %v, want %v", err, ErrUnavailable)
	}
	resp, err := fake.Complete(context.Background(), Request{})
	if err != nil {
		t.Fatalf("second Complete: %v", err)
	}
	if resp == nil {
		t.Error("second Complete: want a response, got nil")
	}
}

func TestFakeClientRecordsRequests(t *testing.T) {
	fake := NewFakeClient(TextResponse("ok"))

	sent := Request{
		Model:    "test-model",
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
	}
	if _, err := fake.Complete(context.Background(), sent); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	requests := fake.Requests()
	if len(requests) != 1 {
		t.Fatalf("Requests() = %d entries, want 1", len(requests))
	}
	if requests[0].Model != "test-model" {
		t.Errorf("Requests()[0].Model = %q, want %q", requests[0].Model, "test-model")
	}
	if len(requests[0].Messages) != 1 || requests[0].Messages[0].Content != "hello" {
		t.Errorf("Requests()[0].Messages = %#v, want one message with content %q", requests[0].Messages, "hello")
	}
	if fake.CallCount() != 1 {
		t.Errorf("CallCount() = %d, want 1", fake.CallCount())
	}
}

func TestFakeClientAnswersToolResultByToolName(t *testing.T) {
	answer := TextResponse(`{"answer":"ok","citations":[]}`)

	fake := NewFakeClient(ToolCallResponse("call_1", "search_memory", `{"keywords":["咖啡"]}`))
	fake.SetToolResponse("search_memory", answer)

	// The first call has no tool result yet, so the turn is used.
	resp, err := fake.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	choice, err := resp.Choice()
	if err != nil {
		t.Fatalf("Choice: %v", err)
	}
	if _, ok := ToolCallFor(choice, "search_memory"); !ok {
		t.Fatalf("first response has no search_memory tool call: %#v", choice.Message.ToolCalls)
	}

	// The second call carries the tool result, so the registered answer wins.
	resp, err = fake.Complete(context.Background(), Request{
		Messages: []Message{
			{Role: RoleUser, Content: "hi"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call_1", Type: "function", Function: FunctionCall{Name: "search_memory", Arguments: `{"keywords":["咖啡"]}`}}}},
			{Role: RoleTool, ToolCallID: "call_1", Name: "search_memory", Content: "[]"},
		},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp != answer {
		t.Errorf("response = %#v, want the tool response %#v", resp, answer)
	}
}

func TestFakeClientHelpersBuildUsableResponses(t *testing.T) {
	text := TextResponse("hello")
	choice, err := text.Choice()
	if err != nil {
		t.Fatalf("Choice: %v", err)
	}
	if choice.Message.Content != "hello" {
		t.Errorf("content = %q, want %q", choice.Message.Content, "hello")
	}
	if choice.Message.Role != RoleAssistant {
		t.Errorf("role = %q, want %q", choice.Message.Role, RoleAssistant)
	}

	call := ToolCallResponse("call_9", "search_memory", `{"keywords":["x"]}`)
	choice, err = call.Choice()
	if err != nil {
		t.Fatalf("Choice: %v", err)
	}
	tc, ok := ToolCallFor(choice, "search_memory")
	if !ok {
		t.Fatalf("no search_memory tool call in %#v", choice.Message.ToolCalls)
	}
	if tc.ID != "call_9" {
		t.Errorf("tool call id = %q, want %q", tc.ID, "call_9")
	}
	if tc.Function.Arguments != `{"keywords":["x"]}` {
		t.Errorf("tool call arguments = %q, want %q", tc.Function.Arguments, `{"keywords":["x"]}`)
	}
}

func TestFakeClientReportsCancelledContextAsUnavailable(t *testing.T) {
	fake := NewFakeClient(TextResponse("ok"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := fake.Complete(ctx, Request{})
	if err == nil {
		t.Fatal("Complete: want error, got nil")
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("Complete error = %v, want %v", err, ErrUnavailable)
	}
}

func TestParseResponseBuildsFixtureResponses(t *testing.T) {
	const raw = `{"id":"chatcmpl-1","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`

	resp, err := ParseResponse(raw)
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	choice, err := resp.Choice()
	if err != nil {
		t.Fatalf("Choice: %v", err)
	}
	if choice.Message.Content != "hi" {
		t.Errorf("content = %q, want %q", choice.Message.Content, "hi")
	}

	if _, err := ParseResponse("not json"); !errors.Is(err, ErrInvalidResponse) {
		t.Errorf("ParseResponse error = %v, want %v", err, ErrInvalidResponse)
	}

	if got := MustParseResponse(raw); got == nil {
		t.Error("MustParseResponse returned nil")
	}
}

func TestFakeClientSkipsNilTurns(t *testing.T) {
	first := TextResponse("first")
	second := TextResponse("second")

	fake := NewFakeClient(nil, first, nil, second)

	resp, err := fake.Complete(context.Background(), Request{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp != first {
		t.Errorf("response = %#v, want %#v", resp, first)
	}

	resp, err = fake.Complete(context.Background(), Request{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp != second {
		t.Errorf("response = %#v, want %#v", resp, second)
	}
}

func TestFakeClientIsSafeForConcurrentUse(t *testing.T) {
	fake := NewFakeClient(
		TextResponse("a"), TextResponse("b"), TextResponse("c"),
		TextResponse("d"), TextResponse("e"), TextResponse("f"),
	)

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := fake.Complete(context.Background(), Request{
				Messages: []Message{{Role: RoleUser, Content: fmt.Sprintf("m%d", i)}},
			}); err != nil && !errors.Is(err, ErrUnavailable) {
				t.Errorf("Complete: %v", err)
			}
			_ = fake.Requests()
			_ = fake.CallCount()
		}(i)
	}
	wg.Wait()

	if got := fake.CallCount(); got != 6 {
		t.Errorf("CallCount() = %d, want 6", got)
	}
}
