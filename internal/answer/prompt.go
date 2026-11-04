package answer

import (
	"encoding/json"
	"fmt"
	"strings"

	"shiyi/internal/llm"
	"shiyi/internal/search"
)

// answer_v1 is the prompt that drives the answering model. It is versioned
// because a change to it is a change in behaviour, and the frozen test set
// has to follow.
const answer_v1 = `你是个人记忆问答器。只能依据 search_memory 返回的记录回答，不得使用常识或猜测。
每条事实陈述必须用记录的时间引用。没有足够记录时，回答「没有找到相关记忆」，不要编造。
最多调用 3 次 search_memory。准备好后只输出 JSON：

{"answer":"...","citations":["<time>", "..."]}`

// searchToolName is the only tool the model may call.
const searchToolName = "search_memory"

// searchTool describes the search tool to the model.
var searchTool = llm.Tool{
	Type: "function",
	Function: llm.ToolFunction{
		Name:        searchToolName,
		Description: "在记忆条目的原文中按关键词检索。keywords 是 1..8 个关键词，任一关键词在原文里出现即命中。返回记录的记录时间与原文，按记录时间倒序。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"keywords": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "1..8 个关键词，任一命中即可",
				},
				"limit": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"maximum":     search.MaxLimit,
					"description": "最多返回的记录条数，默认 10",
				},
			},
			"required": []string{"keywords"},
		},
	},
}

// toolArgs are the arguments of one search_memory call.
type toolArgs struct {
	Keywords []string `json:"keywords"`
	Limit    *int     `json:"limit"`
}

// parseToolArgs reads the arguments of a search_memory call.
//
// An argument that cannot be read is reported back to the model as a tool
// error; it never stops the query.
func parseToolArgs(raw string) (toolArgs, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return toolArgs{}, fmt.Errorf("the arguments are empty")
	}
	var args toolArgs
	if err := json.Unmarshal([]byte(trimmed), &args); err != nil {
		return toolArgs{}, fmt.Errorf("the arguments are not valid JSON: %v", err)
	}
	return args, nil
}

// toolRecord is one record as the model sees it.
type toolRecord struct {
	Time    string `json:"time"`
	Content string `json:"content"`
}

// encodeToolRecords renders the found records for the model.
func encodeToolRecords(records []toolRecord) string {
	if records == nil {
		records = []toolRecord{}
	}
	out, err := json.Marshal(records)
	if err != nil {
		return "[]"
	}
	return string(out)
}
