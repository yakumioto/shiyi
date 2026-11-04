package answer_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"shiyi/internal/answer"
	"shiyi/internal/llm"
	"shiyi/internal/search"
	"shiyi/internal/store"
)

// fakeLLM hands out scripted answers, one per call. It is the seam the
// orchestration is observed through: the test says what the model says and
// checks what the answerer does with it.
type fakeLLM struct {
	mu        sync.Mutex
	responses []*llm.Response
	errs      []error
	requests  []llm.Request
	calls     int
}

// Complete returns the next scripted answer.
func (f *fakeLLM) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.requests = append(f.requests, req)
	index := f.calls
	f.calls++

	if index < len(f.errs) && f.errs[index] != nil {
		return nil, f.errs[index]
	}
	if index >= len(f.responses) {
		return nil, fmt.Errorf("fake llm: no scripted response for call %d", index+1)
	}
	return f.responses[index], nil
}

// requestCount is how many round trips the answerer made.
func (f *fakeLLM) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// lastRequest is the request of the last round trip.
func (f *fakeLLM) lastRequest() llm.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

// fakeSearcher stands in for the records on disk.
type fakeSearcher struct {
	mu      sync.Mutex
	results map[string][]store.Record
	calls   []searchCall
	err     error
}

// searchCall is one call the answerer made to the searcher.
type searchCall struct {
	Keywords []string
	Limit    int
}

// newFakeSearcher returns a searcher that answers by keyword.
func newFakeSearcher() *fakeSearcher {
	return &fakeSearcher{results: map[string][]store.Record{}}
}

// Search returns the records registered for the first keyword.
func (f *fakeSearcher) Search(ctx context.Context, keywords []string, limit int) ([]store.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, searchCall{Keywords: keywords, Limit: limit})
	if f.err != nil {
		return nil, f.err
	}
	for _, keyword := range keywords {
		if records, ok := f.results[keyword]; ok {
			if limit > 0 && len(records) > limit {
				return records[:limit], nil
			}
			return records, nil
		}
	}
	return []store.Record{}, nil
}

// callCount is how often the searcher was called.
func (f *fakeSearcher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// record builds a memory entry.
func record(t *testing.T, ts, content string) store.Record {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t.Fatalf("parse time %q: %v", ts, err)
	}
	return store.Record{Version: 1, Content: content, Time: store.NewTime(parsed)}
}

// toolResponse builds a model answer that asks for the search tool.
func toolResponse(t *testing.T, callID, arguments string) *llm.Response {
	t.Helper()
	return &llm.Response{Choices: []llm.Choice{{
		Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID:       callID,
				Type:     "function",
				Function: llm.FunctionCall{Name: "search_memory", Arguments: arguments},
			}},
		},
		FinishReason: "tool_calls",
	}}}
}

// finalResponse builds a model answer with the final text.
func finalResponse(text string) *llm.Response {
	return &llm.Response{Choices: []llm.Choice{{
		Message:      llm.Message{Role: llm.RoleAssistant, Content: text},
		FinishReason: "stop",
	}}}
}

// answerJSON builds the JSON the model is asked to produce.
func answerJSON(t *testing.T, text string, citations ...string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"answer": text, "citations": citations})
	if err != nil {
		t.Fatalf("marshal answer: %v", err)
	}
	return string(payload)
}

func TestAnswerFindsAndCites(t *testing.T) {
	const (
		tCoffee = "2026-11-01T10:00:00.000000000Z"
		tTea    = "2026-11-02T10:00:00.000000000Z"
	)

	coffee := record(t, tCoffee, "I drink coffee every morning")
	tea := record(t, tTea, "I drink tea in the evening")

	client := &fakeLLM{responses: []*llm.Response{
		toolResponse(t, "call-1", `{"keywords":["coffee"]}`),
		finalResponse(answerJSON(t, "You drink coffee every morning.", tCoffee)),
	}}
	searcher := newFakeSearcher()
	searcher.results["coffee"] = []store.Record{coffee, tea}

	got, err := answer.New(client, searcher, "test-model", 0, answer.DefaultBudget()).Answer(context.Background(), "What do I drink?")
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if got.Status != answer.StatusOK {
		t.Fatalf("got status %q, want %q", got.Status, answer.StatusOK)
	}
	if got.Answer != "You drink coffee every morning." {
		t.Fatalf("got answer %q, want the model's answer", got.Answer)
	}
	if len(got.Citations) != 1 {
		t.Fatalf("got %d citations, want 1", len(got.Citations))
	}
	if got.Citations[0].Time != tCoffee {
		t.Fatalf("got citation time %q, want %q", got.Citations[0].Time, tCoffee)
	}
	if got.Citations[0].Content != "I drink coffee every morning" {
		t.Fatalf("got citation content %q, want the original text", got.Citations[0].Content)
	}
}

func TestAnswerOffersTheSearchToolEveryRound(t *testing.T) {
	client := &fakeLLM{responses: []*llm.Response{
		toolResponse(t, "call-1", `{"keywords":["coffee"]}`),
		finalResponse(answerJSON(t, "Done.")),
	}}
	searcher := newFakeSearcher()

	if _, err := answer.New(client, searcher, "test-model", 0, answer.DefaultBudget()).Answer(context.Background(), "q"); err != nil {
		t.Fatalf("Answer: %v", err)
	}

	if client.requestCount() != 2 {
		t.Fatalf("got %d requests, want 2", client.requestCount())
	}
	for i, req := range client.requests {
		if len(req.Tools) != 1 {
			t.Fatalf("request %d: got %d tools, want 1", i+1, len(req.Tools))
		}
		if req.Tools[0].Function.Name != "search_memory" {
			t.Fatalf("request %d: got tool %q, want %q", i+1, req.Tools[0].Function.Name, "search_memory")
		}
		if req.Tools[0].Function.Parameters == nil {
			t.Fatalf("request %d: the tool has no parameter schema", i+1)
		}
	}
}

func TestAnswerSendsThePromptAndTheQuery(t *testing.T) {
	client := &fakeLLM{responses: []*llm.Response{finalResponse(answerJSON(t, "Nothing."))}}
	searcher := newFakeSearcher()

	if _, err := answer.New(client, searcher, "test-model", 0, answer.DefaultBudget()).Answer(context.Background(), "Where do I live?"); err != nil {
		t.Fatalf("Answer: %v", err)
	}

	req := client.lastRequest()
	if len(req.Messages) < 2 {
		t.Fatalf("got %d messages, want at least 2", len(req.Messages))
	}
	if req.Messages[0].Role != llm.RoleSystem {
		t.Fatalf("got role %q for the first message, want %q", req.Messages[0].Role, llm.RoleSystem)
	}
	if !strings.Contains(req.Messages[0].Content, "search_memory") {
		t.Fatalf("the system prompt does not mention the search tool: %q", req.Messages[0].Content)
	}
	if req.Messages[1].Role != llm.RoleUser || req.Messages[1].Content != "Where do I live?" {
		t.Fatalf("got %+v for the second message, want the query as a user message", req.Messages[1])
	}
}

func TestAnswerReturnsNoMatchWithoutEvidence(t *testing.T) {
	client := &fakeLLM{responses: []*llm.Response{
		toolResponse(t, "call-1", `{"keywords":["nothinghere"]}`),
		finalResponse("I know the answer: Paris is the capital of France."),
	}}
	searcher := newFakeSearcher()

	got, err := answer.New(client, searcher, "test-model", 0, answer.DefaultBudget()).Answer(context.Background(), "What is the capital of France?")
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if got.Status != answer.StatusNoMatch {
		t.Fatalf("got status %q, want %q", got.Status, answer.StatusNoMatch)
	}
	if got.Answer != answer.NoMatchAnswer {
		t.Fatalf("got answer %q, want the fixed answer %q", got.Answer, answer.NoMatchAnswer)
	}
	if strings.Contains(got.Answer, "Paris") {
		t.Fatalf("the answer kept the model's own text: %q", got.Answer)
	}
	if len(got.Citations) != 0 {
		t.Fatalf("got %d citations, want none", len(got.Citations))
	}
}

func TestAnswerRefusesACitationOutsideTheEvidence(t *testing.T) {
	const tFound = "2026-11-01T10:00:00.000000000Z"
	const tOther = "2026-11-09T10:00:00.000000000Z"

	client := &fakeLLM{responses: []*llm.Response{
		toolResponse(t, "call-1", `{"keywords":["coffee"]}`),
		finalResponse(answerJSON(t, "You drink coffee.", tOther)),
	}}
	searcher := newFakeSearcher()
	searcher.results["coffee"] = []store.Record{record(t, tFound, "I drink coffee every morning")}

	got, err := answer.New(client, searcher, "test-model", 0, answer.DefaultBudget()).Answer(context.Background(), "What do I drink?")
	if err == nil {
		t.Fatalf("Answer returned %+v, want an error", got)
	}
	if !errors.Is(err, answer.ErrInvalidCitation) {
		t.Fatalf("got error %v, want one wrapping answer.ErrInvalidCitation", err)
	}
}

func TestAnswerRefusesACitationFromAnotherRound(t *testing.T) {
	const tFirst = "2026-11-01T10:00:00.000000000Z"
	const tSecond = "2026-11-02T10:00:00.000000000Z"
	const tNever = "2026-11-09T10:00:00.000000000Z"

	client := &fakeLLM{responses: []*llm.Response{
		toolResponse(t, "call-1", `{"keywords":["coffee"]}`),
		toolResponse(t, "call-2", `{"keywords":["tea"]}`),
		finalResponse(answerJSON(t, "You drink coffee and tea.", tNever)),
	}}
	searcher := newFakeSearcher()
	searcher.results["coffee"] = []store.Record{record(t, tFirst, "I drink coffee every morning")}
	searcher.results["tea"] = []store.Record{record(t, tSecond, "I drink tea in the evening")}

	got, err := answer.New(client, searcher, "test-model", 0, answer.DefaultBudget()).Answer(context.Background(), "What do I drink?")
	if err == nil {
		t.Fatalf("Answer returned %+v, want an error", got)
	}
	if !errors.Is(err, answer.ErrInvalidCitation) {
		t.Fatalf("got error %v, want one wrapping answer.ErrInvalidCitation", err)
	}
}

func TestAnswerAcceptsACitationFromAnotherRound(t *testing.T) {
	const tFirst = "2026-11-01T10:00:00.000000000Z"
	const tSecond = "2026-11-02T10:00:00.000000000Z"

	client := &fakeLLM{responses: []*llm.Response{
		toolResponse(t, "call-1", `{"keywords":["coffee"]}`),
		toolResponse(t, "call-2", `{"keywords":["tea"]}`),
		finalResponse(answerJSON(t, "You drink coffee and tea.", tFirst, tSecond)),
	}}
	searcher := newFakeSearcher()
	searcher.results["coffee"] = []store.Record{record(t, tFirst, "I drink coffee every morning")}
	searcher.results["tea"] = []store.Record{record(t, tSecond, "I drink tea in the evening")}

	got, err := answer.New(client, searcher, "test-model", 0, answer.DefaultBudget()).Answer(context.Background(), "What do I drink?")
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if got.Status != answer.StatusOK {
		t.Fatalf("got status %q, want %q", got.Status, answer.StatusOK)
	}
	if len(got.Citations) != 2 {
		t.Fatalf("got %d citations, want 2", len(got.Citations))
	}
	if got.Citations[0].Time != tFirst || got.Citations[1].Time != tSecond {
		t.Fatalf("got citations %+v, want %q and %q", got.Citations, tFirst, tSecond)
	}
	if got.Citations[1].Content != "I drink tea in the evening" {
		t.Fatalf("got citation content %q, want the original text", got.Citations[1].Content)
	}
}

func TestAnswerStopsSearchingWhenTheBudgetIsSpent(t *testing.T) {
	const tCoffee = "2026-11-01T10:00:00.000000000Z"

	// The model keeps asking to search; the third call is the last one the
	// budget allows.
	client := &fakeLLM{responses: []*llm.Response{
		toolResponse(t, "call-1", `{"keywords":["coffee"]}`),
		toolResponse(t, "call-2", `{"keywords":["coffee"]}`),
		toolResponse(t, "call-3", `{"keywords":["coffee"]}`),
		toolResponse(t, "call-4", `{"keywords":["coffee"]}`),
		finalResponse(answerJSON(t, "You drink coffee.", tCoffee)),
	}}
	searcher := newFakeSearcher()
	searcher.results["coffee"] = []store.Record{record(t, tCoffee, "I drink coffee every morning")}

	got, err := answer.New(client, searcher, "test-model", 0, answer.DefaultBudget()).Answer(context.Background(), "What do I drink?")
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if got.Status != answer.StatusBudgetExhausted {
		t.Fatalf("got status %q, want %q", got.Status, answer.StatusBudgetExhausted)
	}
	if got.Answer != "You drink coffee." {
		t.Fatalf("got answer %q, want the model's answer", got.Answer)
	}
	if len(got.Citations) != 1 || got.Citations[0].Time != tCoffee {
		t.Fatalf("got citations %+v, want the record that was found", got.Citations)
	}
	if searcher.callCount() != 3 {
		t.Fatalf("the searcher was called %d times, want 3", searcher.callCount())
	}

	// The last call is the "answer now" call: it must not offer the tool
	// again, or the model would search forever.
	last := client.lastRequest()
	if len(last.Tools) != 0 {
		t.Fatalf("the final request still offers %d tools, want none", len(last.Tools))
	}
}

func TestAnswerRefusesAnInvalidCitationWhenTheBudgetIsSpent(t *testing.T) {
	const tCoffee = "2026-11-01T10:00:00.000000000Z"
	const tNever = "2026-11-09T10:00:00.000000000Z"

	client := &fakeLLM{responses: []*llm.Response{
		toolResponse(t, "call-1", `{"keywords":["coffee"]}`),
		toolResponse(t, "call-2", `{"keywords":["coffee"]}`),
		toolResponse(t, "call-3", `{"keywords":["coffee"]}`),
		toolResponse(t, "call-4", `{"keywords":["coffee"]}`),
		finalResponse(answerJSON(t, "You drink coffee.", tNever)),
	}}
	searcher := newFakeSearcher()
	searcher.results["coffee"] = []store.Record{record(t, tCoffee, "I drink coffee every morning")}

	got, err := answer.New(client, searcher, "test-model", 0, answer.DefaultBudget()).Answer(context.Background(), "What do I drink?")
	if err == nil {
		t.Fatalf("Answer returned %+v, want an error", got)
	}
	if !errors.Is(err, answer.ErrInvalidCitation) {
		t.Fatalf("got error %v, want one wrapping answer.ErrInvalidCitation", err)
	}
}

func TestAnswerCountsRecordsOnlyOnce(t *testing.T) {
	const tCoffee = "2026-11-01T10:00:00.000000000Z"

	// Both rounds find the same record; it counts once against the budget
	// and stays citable.
	client := &fakeLLM{responses: []*llm.Response{
		toolResponse(t, "call-1", `{"keywords":["coffee"]}`),
		toolResponse(t, "call-2", `{"keywords":["coffee"]}`),
		finalResponse(answerJSON(t, "You drink coffee.", tCoffee)),
	}}
	searcher := newFakeSearcher()
	searcher.results["coffee"] = []store.Record{record(t, tCoffee, "I drink coffee every morning")}

	got, err := answer.New(client, searcher, "test-model", 0, answer.DefaultBudget()).Answer(context.Background(), "What do I drink?")
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if got.Status != answer.StatusOK {
		t.Fatalf("got status %q, want %q", got.Status, answer.StatusOK)
	}
	if len(got.Citations) != 1 {
		t.Fatalf("got %d citations, want 1", len(got.Citations))
	}
}

func TestAnswerKeepsGoingAfterInvalidToolArguments(t *testing.T) {
	// Three bad calls use up the three calls the budget allows, so the
	// query ends without a single record: the answer is the fixed one.
	client := &fakeLLM{responses: []*llm.Response{
		toolResponse(t, "call-1", `{"keywords":[]}`),
		toolResponse(t, "call-2", `not json at all`),
		toolResponse(t, "call-3", `{"keywords":"coffee"}`),
		finalResponse("You drink coffee."),
	}}
	searcher := newFakeSearcher()
	searcher.results["coffee"] = []store.Record{record(t, "2026-11-01T10:00:00.000000000Z", "I drink coffee every morning")}

	got, err := answer.New(client, searcher, "test-model", 0, answer.DefaultBudget()).Answer(context.Background(), "What do I drink?")
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if got.Status != answer.StatusNoMatch {
		t.Fatalf("got status %q, want %q", got.Status, answer.StatusNoMatch)
	}
	if got.Answer != answer.NoMatchAnswer {
		t.Fatalf("got answer %q, want the fixed answer", got.Answer)
	}
	if searcher.callCount() != 0 {
		t.Fatalf("the searcher was called %d times, want 0: no call had usable arguments", searcher.callCount())
	}

	// The tool results that report the bad arguments are sent back to the
	// model so that it can correct itself.
	req := client.lastRequest()
	found := false
	for _, msg := range req.Messages {
		if msg.Role == llm.RoleTool && strings.Contains(msg.Content, "search_memory") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no tool result was sent back to the model: %+v", req.Messages)
	}
}

// TestAnswerKeepsGoingAfterInvalidToolArgumentsWithRoomToSpare checks that
// a model that has to correct itself still finds the record afterwards.
func TestAnswerKeepsGoingAfterInvalidToolArgumentsWithRoomToSpare(t *testing.T) {
	const tCoffee = "2026-11-01T10:00:00.000000000Z"

	client := &fakeLLM{responses: []*llm.Response{
		toolResponse(t, "call-1", `{"keywords":[]}`),
		toolResponse(t, "call-2", `{"keywords":["coffee"]}`),
		finalResponse(answerJSON(t, "You drink coffee.", tCoffee)),
	}}
	searcher := newFakeSearcher()
	searcher.results["coffee"] = []store.Record{record(t, tCoffee, "I drink coffee every morning")}

	got, err := answer.New(client, searcher, "test-model", 0, answer.DefaultBudget()).Answer(context.Background(), "What do I drink?")
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if got.Status != answer.StatusOK {
		t.Fatalf("got status %q, want %q", got.Status, answer.StatusOK)
	}
	if got.Answer != "You drink coffee." {
		t.Fatalf("got answer %q, want the model's answer", got.Answer)
	}
	if len(got.Citations) != 1 {
		t.Fatalf("got %d citations, want 1", len(got.Citations))
	}
	if searcher.callCount() != 1 {
		t.Fatalf("the searcher was called %d times, want 1", searcher.callCount())
	}
}

// TestAnswerRefusesATooLongKeyword checks that a keyword beyond the
// accepted length is reported to the model instead of ending the query.
func TestAnswerRefusesATooLongKeyword(t *testing.T) {
	const tCoffee = "2026-11-01T10:00:00.000000000Z"

	long := strings.Repeat("x", search.MaxKeywordLen+1)
	client := &fakeLLM{responses: []*llm.Response{
		toolResponse(t, "call-1", `{"keywords":["`+long+`"]}`),
		toolResponse(t, "call-2", `{"keywords":["coffee"]}`),
		finalResponse(answerJSON(t, "You drink coffee.", tCoffee)),
	}}
	searcher := newFakeSearcher()
	searcher.results["coffee"] = []store.Record{record(t, tCoffee, "I drink coffee every morning")}

	got, err := answer.New(client, searcher, "test-model", 0, answer.DefaultBudget()).Answer(context.Background(), "What do I drink?")
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if len(got.Citations) != 1 {
		t.Fatalf("got %d citations, want 1", len(got.Citations))
	}
}

func TestAnswerRefusesAnUnparsableAnswer(t *testing.T) {
	const tCoffee = "2026-11-01T10:00:00.000000000Z"

	client := &fakeLLM{responses: []*llm.Response{
		toolResponse(t, "call-1", `{"keywords":["coffee"]}`),
		finalResponse("I drink coffee, I think."),
	}}
	searcher := newFakeSearcher()
	searcher.results["coffee"] = []store.Record{record(t, tCoffee, "I drink coffee every morning")}

	got, err := answer.New(client, searcher, "test-model", 0, answer.DefaultBudget()).Answer(context.Background(), "What do I drink?")
	if err == nil {
		t.Fatalf("Answer returned %+v, want an error", got)
	}
	if !errors.Is(err, llm.ErrInvalidResponse) {
		t.Fatalf("got error %v, want one wrapping llm.ErrInvalidResponse", err)
	}
}

func TestAnswerRefusesAnEmptyAnswer(t *testing.T) {
	const tCoffee = "2026-11-01T10:00:00.000000000Z"

	client := &fakeLLM{responses: []*llm.Response{
		toolResponse(t, "call-1", `{"keywords":["coffee"]}`),
		finalResponse("   "),
	}}
	searcher := newFakeSearcher()
	searcher.results["coffee"] = []store.Record{record(t, tCoffee, "I drink coffee every morning")}

	if _, err := answer.New(client, searcher, "test-model", 0, answer.DefaultBudget()).Answer(context.Background(), "What do I drink?"); !errors.Is(err, llm.ErrInvalidResponse) {
		t.Fatalf("got error %v, want one wrapping llm.ErrInvalidResponse", err)
	}
}

func TestAnswerPassesTheModelUnavailable(t *testing.T) {
	client := &fakeLLM{errs: []error{llm.ErrUnavailable}}
	searcher := newFakeSearcher()

	got, err := answer.New(client, searcher, "test-model", 0, answer.DefaultBudget()).Answer(context.Background(), "q")
	if err == nil {
		t.Fatalf("Answer returned %+v, want an error", got)
	}
	if !errors.Is(err, llm.ErrUnavailable) {
		t.Fatalf("got error %v, want one wrapping llm.ErrUnavailable", err)
	}
}

func TestAnswerReportsASearchFailure(t *testing.T) {
	client := &fakeLLM{responses: []*llm.Response{
		toolResponse(t, "call-1", `{"keywords":["coffee"]}`),
		finalResponse(answerJSON(t, "You drink coffee.")),
	}}
	searcher := newFakeSearcher()
	searcher.err = search.ErrSearchFailed

	got, err := answer.New(client, searcher, "test-model", 0, answer.DefaultBudget()).Answer(context.Background(), "What do I drink?")
	if err == nil {
		t.Fatalf("Answer returned %+v, want an error", got)
	}
	if !errors.Is(err, search.ErrSearchFailed) {
		t.Fatalf("got error %v, want one wrapping search.ErrSearchFailed", err)
	}
}

func TestAnswerRespectsTheRecordBudget(t *testing.T) {
	// The budget allows one record; the searcher would hand out two.
	const tFirst = "2026-11-01T10:00:00.000000000Z"
	const tSecond = "2026-11-02T10:00:00.000000000Z"

	client := &fakeLLM{responses: []*llm.Response{
		toolResponse(t, "call-1", `{"keywords":["coffee"],"limit":5}`),
		finalResponse(answerJSON(t, "You drink coffee.", tFirst)),
	}}
	searcher := newFakeSearcher()
	searcher.results["coffee"] = []store.Record{
		record(t, tFirst, "I drink coffee every morning"),
		record(t, tSecond, "I drink coffee in the evening"),
	}

	budget := answer.Budget{MaxSearchCalls: 3, MaxRecords: 1, AnswerTimeout: 30 * time.Second}
	got, err := answer.New(client, searcher, "test-model", 0, budget).Answer(context.Background(), "What do I drink?")
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if len(got.Citations) != 1 {
		t.Fatalf("got %d citations, want 1", len(got.Citations))
	}
	if got.Citations[0].Time != tFirst {
		t.Fatalf("got citation %q, want %q", got.Citations[0].Time, tFirst)
	}
}

func TestAnswerStopsWhenTheTimeIsUp(t *testing.T) {
	// The model never answers; the deadline of the answering phase ends it.
	client := &slowLLM{}
	searcher := newFakeSearcher()

	budget := answer.Budget{MaxSearchCalls: 3, MaxRecords: 20, AnswerTimeout: 50 * time.Millisecond}
	start := time.Now()
	got, err := answer.New(client, searcher, "test-model", 0, budget).Answer(context.Background(), "q")
	if err == nil {
		t.Fatalf("Answer returned %+v, want an error", got)
	}
	if !errors.Is(err, llm.ErrUnavailable) {
		t.Fatalf("got error %v, want one wrapping llm.ErrUnavailable", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("the answer took %v, want it to stop at the deadline", elapsed)
	}
}

// slowLLM never answers in time.
type slowLLM struct{}

// Complete waits for the context to end.
func (slowLLM) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	<-ctx.Done()
	return nil, fmt.Errorf("llm: %w", ctx.Err())
}

func TestAnswerIgnoresDuplicateCitations(t *testing.T) {
	const tCoffee = "2026-11-01T10:00:00.000000000Z"

	client := &fakeLLM{responses: []*llm.Response{
		toolResponse(t, "call-1", `{"keywords":["coffee"]}`),
		finalResponse(answerJSON(t, "You drink coffee.", tCoffee, tCoffee)),
	}}
	searcher := newFakeSearcher()
	searcher.results["coffee"] = []store.Record{record(t, tCoffee, "I drink coffee every morning")}

	got, err := answer.New(client, searcher, "test-model", 0, answer.DefaultBudget()).Answer(context.Background(), "What do I drink?")
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if len(got.Citations) != 1 {
		t.Fatalf("got %d citations, want 1", len(got.Citations))
	}
}

func TestAnswerKeepsTheCitationOrderOfTheModel(t *testing.T) {
	const tFirst = "2026-11-01T10:00:00.000000000Z"
	const tSecond = "2026-11-02T10:00:00.000000000Z"

	client := &fakeLLM{responses: []*llm.Response{
		toolResponse(t, "call-1", `{"keywords":["coffee"]}`),
		finalResponse(answerJSON(t, "You drink coffee and tea.", tSecond, tFirst)),
	}}
	searcher := newFakeSearcher()
	searcher.results["coffee"] = []store.Record{
		record(t, tFirst, "I drink coffee every morning"),
		record(t, tSecond, "I drink tea in the evening"),
	}

	got, err := answer.New(client, searcher, "test-model", 0, answer.DefaultBudget()).Answer(context.Background(), "What do I drink?")
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if len(got.Citations) != 2 {
		t.Fatalf("got %d citations, want 2", len(got.Citations))
	}
	if got.Citations[0].Time != tSecond || got.Citations[1].Time != tFirst {
		t.Fatalf("got citations %+v, want %q first", got.Citations, tSecond)
	}
}

func TestAnswerKeepsTheEvidenceAcrossRounds(t *testing.T) {
	const tFirst = "2026-11-01T10:00:00.000000000Z"
	const tSecond = "2026-11-02T10:00:00.000000000Z"

	client := &fakeLLM{responses: []*llm.Response{
		toolResponse(t, "call-1", `{"keywords":["coffee"]}`),
		toolResponse(t, "call-2", `{"keywords":["tea"]}`),
		finalResponse(answerJSON(t, "You drink coffee and tea.", tFirst)),
	}}
	searcher := newFakeSearcher()
	searcher.results["coffee"] = []store.Record{record(t, tFirst, "I drink coffee every morning")}
	searcher.results["tea"] = []store.Record{record(t, tSecond, "I drink tea in the evening")}

	got, err := answer.New(client, searcher, "test-model", 0, answer.DefaultBudget()).Answer(context.Background(), "What do I drink?")
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	// The second round's record is evidence even though the answer does not
	// cite it, and the first round's record is still citable.
	if len(got.Citations) != 1 || got.Citations[0].Time != tFirst {
		t.Fatalf("got citations %+v, want only %q", got.Citations, tFirst)
	}
}

// TestAnswerShowsTheOriginalTextToTheModel checks that the tool result
// carries the record's original text: the model cannot answer from a
// record time alone.
func TestAnswerShowsTheOriginalTextToTheModel(t *testing.T) {
	const tCoffee = "2026-11-01T10:00:00.000000000Z"

	client := &fakeLLM{responses: []*llm.Response{
		toolResponse(t, "call-1", `{"keywords":["coffee"]}`),
		finalResponse(answerJSON(t, "You drink coffee.", tCoffee)),
	}}
	searcher := newFakeSearcher()
	searcher.results["coffee"] = []store.Record{record(t, tCoffee, "I drink coffee every morning")}

	if _, err := answer.New(client, searcher, "test-model", 0, answer.DefaultBudget()).Answer(context.Background(), "What do I drink?"); err != nil {
		t.Fatalf("Answer: %v", err)
	}

	var toolResult string
	for _, msg := range client.lastRequest().Messages {
		if msg.Role == llm.RoleTool {
			toolResult = msg.Content
		}
	}
	if toolResult == "" {
		t.Fatal("no tool result was sent to the model")
	}
	if !strings.Contains(toolResult, "I drink coffee every morning") {
		t.Fatalf("the tool result does not carry the original text: %q", toolResult)
	}
	if !strings.Contains(toolResult, tCoffee) {
		t.Fatalf("the tool result does not carry the record time: %q", toolResult)
	}
}

// TestTimeBudgetProducesABudgetExhaustedAnswer covers the rule that running out
// of the answering budget is a partial answer, not a service failure: the
// evidence gathered before the deadline is still answered from, and the status
// says so.
func TestTimeBudgetProducesABudgetExhaustedAnswer(t *testing.T) {
	tea := record(t, "2026-11-04T10:00:00.000000000Z", "我喜欢喝乌龙茶")
	searcher := newFakeSearcher()
	searcher.results["乌龙茶"] = []store.Record{tea}

	// The first round searches; the second round stalls past the deadline; the
	// call made after the deadline answers from the evidence already gathered.
	client := &stallingLLM{
		first: llm.ToolCallResponse("c1", "search_memory", `{"keywords":["乌龙茶"],"limit":5}`),
		last:  llm.TextResponse(fmt.Sprintf(`{"answer":"你喜欢喝乌龙茶。","citations":[%q]}`, tea.Time.String())),
	}

	budget := answer.Budget{MaxSearchCalls: 3, MaxRecords: 20, AnswerTimeout: 120 * time.Millisecond}
	got, err := answer.New(client, searcher, "test-model", 0, budget).Answer(context.Background(), "我喜欢什么茶？")
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}

	if got.Status != answer.StatusBudgetExhausted {
		t.Fatalf("status = %q, want %q", got.Status, answer.StatusBudgetExhausted)
	}
	if len(got.Citations) != 1 {
		t.Fatalf("got %d citations, want 1", len(got.Citations))
	}
	if got.Citations[0].Content != "我喜欢喝乌龙茶" {
		t.Fatalf("citation content = %q, want the original text", got.Citations[0].Content)
	}
}

// stallingLLM answers the first call immediately, then never answers until its
// context ends, then answers the call that comes after the deadline.
type stallingLLM struct {
	mu    sync.Mutex
	calls int
	first *llm.Response
	last  *llm.Response
}

func (s *stallingLLM) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	s.mu.Lock()
	s.calls++
	call := s.calls
	s.mu.Unlock()

	switch call {
	case 1:
		return s.first, nil
	case 2:
		// Hold the call until the answering budget expires.
		<-ctx.Done()
		return nil, fmt.Errorf("llm: %w", ctx.Err())
	default:
		return s.last, nil
	}
}

// TestTimeBudgetWithoutEvidenceIsNoMatch covers the other side of the same
// rule: a deadline that expires before anything was found must not invent an
// answer, and must not turn into a service error either.
func TestTimeBudgetWithoutEvidenceIsNoMatch(t *testing.T) {
	searcher := newFakeSearcher() // knows nothing

	client := &stallingLLM{
		first: llm.ToolCallResponse("c1", "search_memory", `{"keywords":["车"],"limit":5}`),
		last:  llm.TextResponse(`{"answer":"巴黎是法国首都。","citations":[]}`),
	}

	budget := answer.Budget{MaxSearchCalls: 3, MaxRecords: 20, AnswerTimeout: 120 * time.Millisecond}
	got, err := answer.New(client, searcher, "test-model", 0, budget).Answer(context.Background(), "我的车是什么颜色？")
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}

	if got.Status != answer.StatusNoMatch {
		t.Fatalf("status = %q, want %q", got.Status, answer.StatusNoMatch)
	}
	if got.Answer != answer.NoMatchAnswer {
		t.Fatalf("answer = %q, want the fixed no-match text", got.Answer)
	}
	if len(got.Citations) != 0 {
		t.Fatalf("got %d citations, want none", len(got.Citations))
	}
}

// TestAnswerUsesTheConfiguredTemperature covers the rule that the answering
// model's temperature comes from the configuration rather than being fixed.
func TestAnswerUsesTheConfiguredTemperature(t *testing.T) {
	client := &fakeLLM{responses: []*llm.Response{finalResponse(`{"answer":"ok","citations":[]}`)}}
	searcher := newFakeSearcher()

	if _, err := answer.New(client, searcher, "test-model", 0.3, answer.DefaultBudget()).
		Answer(context.Background(), "q"); err != nil {
		t.Fatalf("Answer: %v", err)
	}

	if got, want := client.lastRequest().Temperature, 0.3; got != want {
		t.Fatalf("temperature = %v, want the configured %v", got, want)
	}
}
