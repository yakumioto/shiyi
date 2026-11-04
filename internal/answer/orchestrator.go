package answer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"shiyi/internal/llm"
	"shiyi/internal/search"
	"shiyi/internal/store"
)

// Budget caps the work a single query may do.
type Budget struct {
	// MaxSearchCalls is how many times the model may call search_memory.
	MaxSearchCalls int

	// MaxRecords is how many different records one query may read in total.
	MaxRecords int

	// AnswerTimeout is the total time the answering phase may take.
	AnswerTimeout time.Duration
}

// DefaultBudget is the budget a query gets when nothing else is configured.
func DefaultBudget() Budget {
	return Budget{
		MaxSearchCalls: 3,
		MaxRecords:     20,
		AnswerTimeout:  30 * time.Second,
	}
}

// answerer answers queries against the records a searcher finds.
type answerer struct {
	client      llm.Client
	searcher    search.Searcher
	model       string
	temperature float64
	budget      Budget
}

// New returns an Answerer that searches with searcher and answers with
// client.
//
// The model name is the one the answering endpoint is configured with. A
// budget field left at zero falls back to the default, so that a zero
// value cannot silently mean "no searching at all".
func New(client llm.Client, searcher search.Searcher, model string, temperature float64, budget Budget) Answerer {
	defaults := DefaultBudget()
	if budget.MaxSearchCalls <= 0 {
		budget.MaxSearchCalls = defaults.MaxSearchCalls
	}
	if budget.MaxRecords <= 0 {
		budget.MaxRecords = defaults.MaxRecords
	}
	if budget.AnswerTimeout <= 0 {
		budget.AnswerTimeout = defaults.AnswerTimeout
	}
	return &answerer{
		client:      client,
		searcher:    searcher,
		model:       model,
		temperature: temperature,
		budget:      budget,
	}
}

// Answer searches the records and answers the query.
//
// The model drives the search: it may call search_memory as often as the
// budget allows, and the records it gets back are the only records the
// answer may cite.
func (a *answerer) Answer(ctx context.Context, query string) (Result, error) {
	// The search phase is bounded by the answering budget. The request itself
	// may live longer, which is what leaves room for the one final answer once
	// the search phase runs out of time.
	searchCtx, cancelSearch := context.WithTimeout(ctx, a.budget.AnswerTimeout)
	defer cancelSearch()

	s := &session{
		answerer: a,
		messages: []llm.Message{
			{Role: llm.RoleSystem, Content: answer_v1},
			{Role: llm.RoleUser, Content: query},
		},
		evidence: map[string]store.Record{},
	}

	for {
		// The search tool is offered on every round, and only on the
		// rounds the budget still allows. Once the budget is spent the
		// model gets one last word without the tool, so that it answers
		// from the evidence it already has.
		withTools := !s.exhausted

		resp, err := a.complete(searchCtx, s, withTools)
		if err != nil {
			// Running out of the answering budget is a spent budget, not a
			// broken provider: the query still deserves an answer built from
			// whatever evidence was gathered before the deadline.
			if ctx.Err() == nil && errors.Is(err, llm.ErrUnavailable) && searchCtx.Err() != nil {
				return a.answerNow(ctx, s)
			}
			return Result{}, err
		}
		choice, err := resp.Choice()
		if err != nil {
			return Result{}, err
		}

		if !withTools {
			if len(s.evidence) == 0 {
				// The budget ran out before anything was found, so there
				// is nothing to answer from. Asking the model would only
				// risk an error over a query whose answer is known.
				return Result{Answer: NoMatchAnswer, Citations: nil, Status: StatusNoMatch}, nil
			}
			return s.finish(choice.Message.Content, StatusBudgetExhausted)
		}
		if len(choice.Message.ToolCalls) == 0 {
			return s.finish(choice.Message.Content, s.status())
		}

		s.messages = append(s.messages, llm.Message{
			Role:      llm.RoleAssistant,
			Content:   choice.Message.Content,
			ToolCalls: choice.Message.ToolCalls,
		})
		for _, call := range choice.Message.ToolCalls {
			text, err := s.runTool(searchCtx, call)
			if err != nil {
				// The search phase deadline can expire inside a search.
				// The request is still alive, so fall back to the answer
				// that the evidence so far supports.
				if ctx.Err() == nil && searchCtx.Err() != nil {
					return a.answerNow(ctx, s)
				}
				return Result{}, err
			}
			s.messages = append(s.messages, llm.Message{
				Role:       llm.RoleTool,
				Content:    text,
				ToolCallID: call.ID,
				Name:       searchToolName,
			})
		}
	}
}

// answerNow produces the answer of a query whose search budget is spent.
//
// It asks the model once more, without the tool, over the evidence gathered so
// far, and reports the answer as partial. When nothing was found at all, the
// fixed no-match answer replaces the model's text.
func (a *answerer) answerNow(ctx context.Context, s *session) (Result, error) {
	s.exhausted = true

	// The search phase was bounded by the answering budget; this last call
	// gets a fresh slice of it, so a provider that never answers cannot hold
	// the request open forever.
	nowCtx, cancel := context.WithTimeout(ctx, a.budget.AnswerTimeout)
	defer cancel()

	resp, err := a.complete(nowCtx, s, false)
	if err != nil {
		return Result{}, err
	}
	choice, err := resp.Choice()
	if err != nil {
		return Result{}, err
	}
	return s.conclude(choice.Message.Content)
}

// complete performs one round trip with the model. The search tool is
// offered unless the search budget is spent.
func (a *answerer) complete(ctx context.Context, s *session, withTools bool) (*llm.Response, error) {
	req := llm.Request{
		Model:       a.model,
		Messages:    s.messages,
		Temperature: a.temperature,
	}
	if withTools {
		req.Tools = []llm.Tool{searchTool}
	}

	resp, err := a.client.Complete(ctx, req)
	if err != nil {
		if errors.Is(err, llm.ErrUnavailable) || errors.Is(err, llm.ErrInvalidResponse) {
			return nil, err
		}
		if ctx.Err() != nil {
			// The deadline of the answering phase ran out, so the model
			// did not answer in time.
			return nil, fmt.Errorf("%w: %v", llm.ErrUnavailable, ctx.Err())
		}
		return nil, err
	}
	return resp, nil
}

// session holds the state of one query.
type session struct {
	answerer *answerer

	// messages is the conversation with the model.
	messages []llm.Message

	// evidence holds every record a search returned during this query, by
	// record time. It is what the answer may cite.
	evidence map[string]store.Record

	// searchCalls counts the tool calls the model made.
	searchCalls int

	// exhausted records that the search budget is spent.
	exhausted bool
}

// status is the status the finished answer carries.
func (s *session) status() Status {
	if s.exhausted {
		return StatusBudgetExhausted
	}
	return StatusOK
}

// runTool executes one tool call and returns the text of the tool result.
//
// A call with unusable arguments is answered with an error text rather than
// with a failure: the model may correct itself and try again, and the
// query goes on. A search that fails for a real reason (ripgrep is
// missing, a shard is damaged) ends the query, because that is a failure of
// the server and not of the model.
//
// Every call the model makes counts against the search budget, whether or
// not it searched: the budget bounds the number of rounds, so a model that
// keeps asking cannot keep the query alive forever.
func (s *session) runTool(ctx context.Context, call llm.ToolCall) (string, error) {
	if s.searchCalls >= s.answerer.budget.MaxSearchCalls {
		s.exhausted = true
		return fmt.Sprintf("%s: the search budget of this query is spent. Answer with the records you already have.", searchToolName), nil
	}
	s.searchCalls++

	if call.Function.Name != searchToolName {
		return fmt.Sprintf("%s: there is no such tool. Call %s instead.", call.Function.Name, searchToolName), nil
	}

	args, err := parseToolArgs(call.Function.Arguments)
	if err != nil {
		return fmt.Sprintf("%s: %v. Call it again with valid arguments.", searchToolName, err), nil
	}
	if len(args.Keywords) == 0 {
		return fmt.Sprintf("%s: no keyword given. Call it again with 1 to %d keywords.", searchToolName, search.MaxKeywords), nil
	}
	if len(args.Keywords) > search.MaxKeywords {
		return fmt.Sprintf("%s: %d keywords given, at most %d are allowed. Call it again with fewer keywords.", searchToolName, len(args.Keywords), search.MaxKeywords), nil
	}
	for _, keyword := range args.Keywords {
		if len(keyword) > search.MaxKeywordLen {
			return fmt.Sprintf("%s: a keyword is longer than %d bytes. Call it again with shorter keywords.", searchToolName, search.MaxKeywordLen), nil
		}
	}

	remaining := s.answerer.budget.MaxRecords - len(s.evidence)
	if remaining <= 0 {
		s.exhausted = true
		return fmt.Sprintf("%s: the record budget of this query is spent. Answer with the records you already have.", searchToolName), nil
	}

	limit := search.DefaultLimit
	if args.Limit != nil && *args.Limit > 0 {
		limit = *args.Limit
	}
	if limit > search.MaxLimit {
		limit = search.MaxLimit
	}
	if limit > remaining {
		limit = remaining
	}

	records, err := s.answerer.searcher.Search(ctx, args.Keywords, limit)
	if err != nil {
		if errors.Is(err, search.ErrInvalidArguments) {
			return fmt.Sprintf("%s: %v. Call it again with valid arguments.", searchToolName, err), nil
		}
		// A search that failed for a real reason ends the query.
		return "", err
	}

	found := make([]toolRecord, 0, len(records))
	for _, record := range records {
		key := record.Time.String()
		if _, seen := s.evidence[key]; !seen {
			if len(s.evidence) >= s.answerer.budget.MaxRecords {
				// The record budget is spent; this record does not become
				// evidence and is not shown to the model either.
				continue
			}
			s.evidence[key] = record
		}
		found = append(found, toolRecord{Time: key, Content: record.Content})
	}
	return encodeToolRecords(found), nil
}

// conclude turns the final text of a budget-exhausted query into a Result.
func (s *session) conclude(content string) (Result, error) {
	if len(s.evidence) == 0 {
		return Result{Answer: NoMatchAnswer, Citations: nil, Status: StatusNoMatch}, nil
	}
	return s.finish(content, StatusBudgetExhausted)
}

// finish turns the final text of the model into the result.
//
// The check for an empty evidence set comes before the parsing of the
// answer: when the search found nothing, the answer is the fixed "no
// memory found" one whatever the model wrote, and the model's text is
// dropped so that it cannot answer from general knowledge. Only when there
// is evidence does the model's output have to be a well-formed answer.
func (s *session) finish(content string, status Status) (Result, error) {
	if len(s.evidence) == 0 {
		return Result{Answer: NoMatchAnswer, Citations: nil, Status: StatusNoMatch}, nil
	}

	answer, citations, err := parseAnswer(content)
	if err != nil {
		return Result{}, err
	}

	result := make([]Citation, 0, len(citations))
	seen := make(map[string]bool, len(citations))
	for _, citation := range citations {
		record, ok := s.lookup(citation)
		if !ok {
			return Result{}, fmt.Errorf("%w: %q was not returned by this query", ErrInvalidCitation, citation)
		}
		key := record.Time.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, Citation{Time: key, Content: record.Content})
	}

	return Result{Answer: answer, Citations: result, Status: status}, nil
}

// lookup finds the record a citation names.
//
// The record time is the identity of a record, so a citation that spells
// that instant differently still names the same record; what is checked is
// that it names a record this query actually read.
func (s *session) lookup(citation string) (store.Record, bool) {
	if record, ok := s.evidence[citation]; ok {
		return record, true
	}
	parsed, err := time.Parse(time.RFC3339Nano, citation)
	if err != nil {
		return store.Record{}, false
	}
	for _, record := range s.evidence {
		if record.Time.Time.Equal(parsed) {
			return record, true
		}
	}
	return store.Record{}, false
}

// modelAnswer is the JSON the model is asked to produce.
type modelAnswer struct {
	Answer    *string  `json:"answer"`
	Citations []string `json:"citations"`
}

// parseAnswer reads the final answer of the model.
func parseAnswer(content string) (string, []string, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "", nil, fmt.Errorf("%w: the model returned no answer", llm.ErrInvalidResponse)
	}

	parsed, err := decodeAnswer(trimmed)
	if err != nil {
		// Models like to wrap their JSON in a markdown fence even when
		// told not to; the fence is not part of the answer.
		unfenced := stripCodeFence(trimmed)
		if unfenced == trimmed {
			return "", nil, fmt.Errorf("%w: %v", llm.ErrInvalidResponse, err)
		}
		parsed, err = decodeAnswer(unfenced)
		if err != nil {
			return "", nil, fmt.Errorf("%w: %v", llm.ErrInvalidResponse, err)
		}
	}
	if parsed.Answer == nil {
		return "", nil, fmt.Errorf("%w: the answer has no \"answer\" field", llm.ErrInvalidResponse)
	}
	return *parsed.Answer, parsed.Citations, nil
}

// decodeAnswer reads one JSON document as an answer.
func decodeAnswer(text string) (modelAnswer, error) {
	var parsed modelAnswer
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return modelAnswer{}, err
	}
	return parsed, nil
}

// stripCodeFence removes a markdown code fence around the JSON.
func stripCodeFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	if newline := strings.IndexByte(s, '\n'); newline >= 0 {
		// Drop the language tag of the opening fence.
		if tag := strings.TrimSpace(s[:newline]); tag == "" || !strings.ContainsAny(tag, " \t") {
			s = s[newline+1:]
		}
	}
	if end := strings.LastIndex(s, "```"); end >= 0 {
		s = s[:end]
	}
	return strings.TrimSpace(s)
}
