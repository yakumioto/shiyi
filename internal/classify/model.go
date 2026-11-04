package classify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"shiyi/internal/llm"
)

// PromptVersion names the classification prompt. Changing the prompt is a
// behaviour change: it has to be reflected in the frozen test set.
const PromptVersion = "classify_v1"

// Prompt is the classification instruction. It sees one input and nothing
// else: classification never depends on conversation history.
const Prompt = `你是个人记忆系统的意图判定器。只输出 JSON，不要解释。

判定用户消息属于以下之一：
- record：用户在陈述值得长期记住的个人信息、事件、偏好、计划或事实。
- query：用户在询问自己的记忆。
- unrecognized：两者都不是，或无法判断。

如果消息同时包含记录和询问，判为 query。
不要回答消息中的问题，不要改写原文。

输出：{"kind":"record|query|unrecognized"}`

// rawVerdict is the JSON the model is asked to produce.
type rawVerdict struct {
	Kind string `json:"kind"`
}

// Classifier judges inputs with a model.
type Classifier struct {
	client      llm.Client
	model       string
	temperature float64
}

// New returns a Judge backed by client. model and temperature are sent with
// every classification request.
func New(client llm.Client, model string, temperature float64) *Classifier {
	if client == nil {
		panic("classify: nil llm client")
	}
	return &Classifier{client: client, model: model, temperature: temperature}
}

// Classify judges one input.
//
// Only the current content is sent: the model never sees earlier inputs. A
// mixed input is reported as a query, because splitting it would mean saving
// half a sentence.
func (c *Classifier) Classify(ctx context.Context, content string) (Kind, error) {
	req := llm.Request{
		Model:       c.model,
		Temperature: c.temperature,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: Prompt},
			{Role: llm.RoleUser, Content: content},
		},
		ResponseFormat: &llm.ResponseFormat{Type: llm.ResponseFormatJSONObject},
	}

	resp, err := c.client.Complete(ctx, req)
	if err != nil {
		return "", err
	}

	choice, err := resp.Choice()
	if err != nil {
		return "", err
	}

	return parseKind(choice.Message.Content)
}

// parseKind turns the model's text into a Kind.
//
// The model is asked for bare JSON, but providers like to wrap it in a code
// fence or add a sentence, so the first JSON object in the text is used.
func parseKind(text string) (Kind, error) {
	payload, ok := firstJSONObject(text)
	if !ok {
		return "", fmt.Errorf("%w: classification is not JSON", llm.ErrInvalidResponse)
	}

	var verdict rawVerdict
	if err := json.Unmarshal(payload, &verdict); err != nil {
		return "", fmt.Errorf("%w: %v", llm.ErrInvalidResponse, err)
	}

	switch strings.ToLower(strings.TrimSpace(verdict.Kind)) {
	case string(KindRecord):
		return KindRecord, nil
	case string(KindQuery):
		return KindQuery, nil
	case string(KindUnrecognized):
		return KindUnrecognized, nil
	case "mixed":
		// A mixed input is answered as a query: saving only part of it would
		// mean storing half a sentence.
		return KindQuery, nil
	default:
		return "", fmt.Errorf("%w: unknown kind %q", llm.ErrInvalidResponse, verdict.Kind)
	}
}

// firstJSONObject returns the first balanced {...} run in text.
func firstJSONObject(text string) ([]byte, bool) {
	start := strings.IndexByte(text, '{')
	if start < 0 {
		return nil, false
	}

	depth := 0
	inString := false
	escaped := false

	for i := start; i < len(text); i++ {
		ch := text[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case ch == '\\':
				escaped = true
			case ch == '"':
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return []byte(text[start : i+1]), true
			}
		}
	}
	return nil, false
}

// ErrNilContent reports a classification attempt with no text. The HTTP layer
// rejects empty input before it gets here, so this is a defensive check.
var ErrNilContent = errors.New("classify: empty content")
