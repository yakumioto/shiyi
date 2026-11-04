package classify_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"shiyi/internal/classify"
	"shiyi/internal/llm"
)

// classifyWith runs the classifier against a scripted model turn.
func classifyWith(t *testing.T, content, modelText string) (classify.Kind, error) {
	t.Helper()
	client := llm.NewFakeClient(llm.TextResponse(modelText))
	judge := classify.New(client, "test-model", 0)
	return judge.Classify(context.Background(), content)
}

// TestClassifyMapsVerdicts covers the three documented verdicts.
func TestClassifyMapsVerdicts(t *testing.T) {
	cases := []struct {
		name      string
		modelText string
		want      classify.Kind
	}{
		{
			name:      "record",
			modelText: `{"kind":"record"}`,
			want:      classify.KindRecord,
		},
		{
			name:      "query",
			modelText: `{"kind":"query"}`,
			want:      classify.KindQuery,
		},
		{
			name:      "unrecognized",
			modelText: `{"kind":"unrecognized"}`,
			want:      classify.KindUnrecognized,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := classifyWith(t, "随便说点什么", tc.modelText)
			if err != nil {
				t.Fatalf("Classify: %v", err)
			}
			if got != tc.want {
				t.Fatalf("Classify = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestMixedInputIsJudgedAQuery is the documented rule for inputs that both
// record and ask: the whole input is answered as a query and nothing is saved.
func TestMixedInputIsJudgedAQuery(t *testing.T) {
	cases := []string{
		`{"kind":"mixed"}`,
		`{"kind":"MIXED"}`,
	}
	for _, modelText := range cases {
		got, err := classifyWith(t, "我明天去杭州，另外我之前说过我喜欢什么茶？", modelText)
		if err != nil {
			t.Fatalf("Classify with %s: %v", modelText, err)
		}
		if got != classify.KindQuery {
			t.Fatalf("mixed verdict %s produced %q, want query", modelText, got)
		}
	}
}

// TestVerdictToleratesSurroundingText covers providers that wrap JSON in prose
// or a code fence.
func TestVerdictToleratesSurroundingText(t *testing.T) {
	cases := []struct {
		name      string
		modelText string
		want      classify.Kind
	}{
		{
			name:      "json in a code fence",
			modelText: "```json\n{\"kind\":\"record\"}\n```",
			want:      classify.KindRecord,
		},
		{
			name:      "json after a sentence",
			modelText: `好的，判定如下：{"kind":"query"}`,
			want:      classify.KindQuery,
		},
		{
			name:      "whitespace inside the value",
			modelText: `{"kind":"  record  "}`,
			want:      classify.KindRecord,
		},
		{
			name:      "braces inside a string",
			modelText: `{"kind":"record","note":"a } brace"}`,
			want:      classify.KindRecord,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := classifyWith(t, "输入", tc.modelText)
			if err != nil {
				t.Fatalf("Classify: %v", err)
			}
			if got != tc.want {
				t.Fatalf("Classify = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestInvalidVerdictIsAnInvalidResponse covers every way the model can answer
// with something unusable. None of these may fall back to a default verdict.
func TestInvalidVerdictIsAnInvalidResponse(t *testing.T) {
	cases := []struct {
		name      string
		modelText string
	}{
		{name: "not JSON at all", modelText: "这是一条记录"},
		{name: "empty", modelText: ""},
		{name: "JSON without kind", modelText: `{"verdict":"record"}`},
		{name: "unknown kind", modelText: `{"kind":"remember"}`},
		{name: "malformed JSON", modelText: `{"kind":`},
		{name: "object nested but no kind", modelText: `{"result":{}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := classifyWith(t, "输入", tc.modelText)
			if err == nil {
				t.Fatal("Classify should reject an unusable verdict")
			}
			if !errors.Is(err, llm.ErrInvalidResponse) {
				t.Fatalf("error should be llm.ErrInvalidResponse, got %v", err)
			}
		})
	}
}

// TestModelUnavailablePropagates covers the 503 path: a provider failure must
// surface unchanged, never as a default verdict.
func TestModelUnavailablePropagates(t *testing.T) {
	client := llm.NewFakeClient()
	client.SetError(llm.ErrUnavailable)

	judge := classify.New(client, "test-model", 0)
	_, err := judge.Classify(context.Background(), "我喜欢喝茶")

	if err == nil {
		t.Fatal("Classify should fail when the model is unavailable")
	}
	if !errors.Is(err, llm.ErrUnavailable) {
		t.Fatalf("error should be llm.ErrUnavailable, got %v", err)
	}
}

// TestOnlyTheCurrentInputIsSent guards the rule that classification has no
// memory: the request carries the prompt and this one input, nothing else.
func TestOnlyTheCurrentInputIsSent(t *testing.T) {
	client := llm.NewFakeClient(llm.TextResponse(`{"kind":"record"}`))
	judge := classify.New(client, "test-model", 0.1)

	const input = "我明天要去杭州"
	if _, err := judge.Classify(context.Background(), input); err != nil {
		t.Fatalf("Classify: %v", err)
	}

	requests := client.Requests()
	if len(requests) != 1 {
		t.Fatalf("got %d model calls, want 1", len(requests))
	}
	req := requests[0]

	if req.Model != "test-model" {
		t.Errorf("model = %q, want the configured model", req.Model)
	}
	if req.Temperature != 0.1 {
		t.Errorf("temperature = %v, want the configured value", req.Temperature)
	}

	var sawPrompt, sawInput bool
	for _, message := range req.Messages {
		switch message.Role {
		case llm.RoleSystem:
			if message.Content == classify.Prompt {
				sawPrompt = true
			}
		case llm.RoleUser:
			if message.Content == input {
				sawInput = true
			}
		}
	}
	if !sawPrompt {
		t.Error("the request should carry the classification prompt")
	}
	if !sawInput {
		t.Error("the request should carry the current input verbatim")
	}
	if got, want := len(req.Messages), 2; got != want {
		t.Errorf("request has %d messages, want exactly prompt + input (%d)", got, want)
	}
}

// TestPromptIsVersionedAndStable guards the documented prompt contract: the
// prompt is a versioned constant, and the frozen test set can identify it.
func TestPromptIsVersionedAndStable(t *testing.T) {
	if classify.PromptVersion != "classify_v1" {
		t.Fatalf("PromptVersion = %q, want classify_v1", classify.PromptVersion)
	}
	for _, want := range []string{"record", "query", "unrecognized", "JSON"} {
		if !strings.Contains(classify.Prompt, want) {
			t.Errorf("prompt should mention %q", want)
		}
	}
	if !strings.Contains(classify.Prompt, "如果消息同时包含记录和询问，判为 query") {
		t.Error("prompt should state that a mixed input is judged a query")
	}
}
