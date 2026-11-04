package frozen

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

// ClassifyCase is one line of classify.jsonl.
type ClassifyCase struct {
	// Name identifies the case in a failure message.
	Name string `json:"name"`
	// Input is the text submitted to POST /messages.
	Input string `json:"input"`
	// ModelResponse is the text the fake model returns for classification.
	// An empty string means the classifier call fails; see ModelError.
	ModelResponse string `json:"model_response,omitempty"`
	// ModelError makes the classification call fail: "unavailable" or
	// "invalid_response".
	ModelError string `json:"model_error,omitempty"`
	// AnswerTurns are the model turns after a query verdict, for cases whose
	// verdict sends the input down the query path.
	AnswerTurns []Turn `json:"answer_turns,omitempty"`
	// Expect is what the request must produce.
	Expect ClassifyExpect `json:"expect"`
}

// ClassifyExpect is the expected outcome of a classification case.
type ClassifyExpect struct {
	// Kind is the response kind: record, query or unrecognized.
	Kind string `json:"kind,omitempty"`
	// HTTPStatus is the expected status code.
	HTTPStatus int `json:"http_status"`
	// ErrorCode is expected in the error envelope, when the case fails.
	ErrorCode string `json:"error_code,omitempty"`
	// WritesRecord says whether the data directory must gain a record.
	WritesRecord bool `json:"writes_record"`
	// Status is the answer status, for verdicts that go down the query path.
	Status string `json:"status,omitempty"`
}

// QueryCase is one line of query.jsonl.
type QueryCase struct {
	// Name identifies the case in a failure message.
	Name string `json:"name"`
	// Records are written to the data directory before the query runs.
	Records []string `json:"records"`
	// Input is the query to submit.
	Input string `json:"input"`
	// Turns are the model responses, in order. Each turn is either a tool
	// call or a final answer.
	Turns []Turn `json:"turns"`
	// ModelError makes a model call fail at the given point: "unavailable"
	// or "invalid_response". Zero means no injected failure.
	ModelError string `json:"model_error,omitempty"`
	// ClassifyResponse is the text the fake model returns for classification.
	ClassifyResponse string `json:"classify_response,omitempty"`
	// Expect is what the query must produce.
	Expect QueryExpect `json:"expect"`
}

// Turn is one scripted model response.
type Turn struct {
	// ToolCall names the tool the model asks for, with raw JSON arguments.
	ToolCall *ToolCall `json:"tool_call,omitempty"`
	// Text is the model's final answer text.
	Text string `json:"text,omitempty"`
}

// ToolCall is one scripted tool invocation.
type ToolCall struct {
	Name string `json:"name"`
	Args string `json:"args"`
}

// QueryExpect is the expected outcome of a query case.
type QueryExpect struct {
	// HTTPStatus is the expected status code.
	HTTPStatus int `json:"http_status"`
	// Status is the answer status: ok, budget_exhausted or no_match.
	Status string `json:"status,omitempty"`
	// AnswerContains lists substrings the answer must contain.
	AnswerContains []string `json:"answer_contains,omitempty"`
	// AnswerExcludes lists substrings the answer must not contain.
	AnswerExcludes []string `json:"answer_excludes,omitempty"`
	// CitationCount is the exact number of citations expected, or -1 to skip.
	CitationCount int `json:"citation_count"`
	// CitationsCarryContent says every citation must carry the record text.
	CitationsCarryContent bool `json:"citations_carry_content"`
	// ErrorCode is expected in the error envelope, when the case fails.
	ErrorCode string `json:"error_code,omitempty"`
}

// LoadClassifyCases reads every case of a classify fixture file.
func LoadClassifyCases(path string) ([]ClassifyCase, error) {
	var cases []ClassifyCase
	err := readJSONL(path, func(line []byte) error {
		var c ClassifyCase
		if err := json.Unmarshal(line, &c); err != nil {
			return err
		}
		cases = append(cases, c)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cases, nil
}

// LoadQueryCases reads every case of a query fixture file.
func LoadQueryCases(path string) ([]QueryCase, error) {
	var cases []QueryCase
	err := readJSONL(path, func(line []byte) error {
		var c QueryCase
		if err := json.Unmarshal(line, &c); err != nil {
			return err
		}
		cases = append(cases, c)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cases, nil
}

// readJSONL calls handle for each non-blank line of path.
func readJSONL(path string, handle func([]byte) error) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8<<20)

	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if err := handle(line); err != nil {
			return fmt.Errorf("line %d: %w", lineNo, err)
		}
	}
	return scanner.Err()
}
