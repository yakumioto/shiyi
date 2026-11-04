package main

import (
	"fmt"
	"strings"
)

// formatResponse renders a server answer as plain text for a human reader.
//
// The output is intentionally plain text: it is meant to be read in a terminal
// and to be grepped in a script. Nothing here writes to a stream, so the whole
// function is testable on its own.
func formatResponse(resp *Response) string {
	if resp == nil {
		return "没有响应"
	}

	switch {
	case resp.Err != nil:
		return fmt.Sprintf("错误 %s: %s", resp.Err.Code, resp.Err.Message)
	case resp.Record != nil:
		return fmt.Sprintf("已记录，记录时间 %s", resp.Record.Time)
	case resp.Query != nil:
		return formatQuery(resp.Query)
	case resp.Unrecognized != nil:
		return "无法识别，这条没有被保存"
	default:
		return "没有响应"
	}
}

// formatQuery renders a query answer together with its citations.
func formatQuery(q *apiQuery) string {
	var b strings.Builder
	b.WriteString(q.Answer)
	b.WriteString("\n状态: ")
	b.WriteString(q.Status)
	for _, c := range q.Citations {
		b.WriteString("\n引用 ")
		b.WriteString(c.Time)
		b.WriteString(": ")
		b.WriteString(c.Content)
	}
	return b.String()
}
