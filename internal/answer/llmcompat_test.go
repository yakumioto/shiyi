package answer_test

import (
	"context"
	"testing"

	"shiyi/internal/answer"
	"shiyi/internal/llm"
	"shiyi/internal/store"
)

// TestAnswerWithTheSharedFakeClient runs the answering with the fake
// client that the llm package ships, so that the orchestration is known to
// work with the same fake the frozen test set will use.
func TestAnswerWithTheSharedFakeClient(t *testing.T) {
	const tCoffee = "2026-11-01T10:00:00.000000000Z"

	client := llm.NewFakeClient(
		llm.ToolCallResponse("call-1", "search_memory", `{"keywords":["coffee"]}`),
		llm.TextResponse(answerJSON(t, "You drink coffee every morning.", tCoffee)),
	)

	searcher := newFakeSearcher()
	searcher.results["coffee"] = []store.Record{record(t, tCoffee, "I drink coffee every morning")}

	got, err := answer.New(client, searcher, "test-model", answer.DefaultBudget()).Answer(context.Background(), "What do I drink?")
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if got.Status != answer.StatusOK {
		t.Fatalf("got status %q, want %q", got.Status, answer.StatusOK)
	}
	if got.Answer != "You drink coffee every morning." {
		t.Fatalf("got answer %q, want the model's answer", got.Answer)
	}
	if len(got.Citations) != 1 || got.Citations[0].Time != tCoffee {
		t.Fatalf("got citations %+v, want the record that was found", got.Citations)
	}
	if got.Citations[0].Content != "I drink coffee every morning" {
		t.Fatalf("got citation content %q, want the original text", got.Citations[0].Content)
	}
}

// TestAnswerWithTheSharedFakeClientRunsOutOfTurns checks that a model
// which never stops searching is stopped by the budget rather than by the
// end of the script.
func TestAnswerWithTheSharedFakeClientRunsOutOfTurns(t *testing.T) {
	const tCoffee = "2026-11-01T10:00:00.000000000Z"

	client := llm.NewFakeClient(
		llm.ToolCallResponse("call-1", "search_memory", `{"keywords":["coffee"]}`),
		llm.ToolCallResponse("call-2", "search_memory", `{"keywords":["coffee"]}`),
		llm.ToolCallResponse("call-3", "search_memory", `{"keywords":["coffee"]}`),
		llm.ToolCallResponse("call-4", "search_memory", `{"keywords":["coffee"]}`),
		llm.TextResponse(answerJSON(t, "You drink coffee.", tCoffee)),
	)

	searcher := newFakeSearcher()
	searcher.results["coffee"] = []store.Record{record(t, tCoffee, "I drink coffee every morning")}

	got, err := answer.New(client, searcher, "test-model", answer.DefaultBudget()).Answer(context.Background(), "What do I drink?")
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if got.Status != answer.StatusBudgetExhausted {
		t.Fatalf("got status %q, want %q", got.Status, answer.StatusBudgetExhausted)
	}
	if searcher.callCount() != 3 {
		t.Fatalf("the searcher was called %d times, want 3", searcher.callCount())
	}
}
