package main

import (
	"strings"
	"testing"
)

func TestFormatResponse(t *testing.T) {
	tests := []struct {
		name string
		resp *Response
		want []string
	}{
		{
			name: "记录打印记录时间",
			resp: &Response{
				Kind:   "record",
				Record: &apiRecord{Kind: "record", Time: "2026-11-04T15:00:00.123456789Z", Version: 1},
			},
			want: []string{"记录时间", "2026-11-04T15:00:00.123456789Z"},
		},
		{
			name: "查询打印回答状态与引用原文",
			resp: &Response{
				Kind: "query",
				Query: &apiQuery{
					Kind:   "query",
					Answer: "你常去公园跑步。",
					Status: "ok",
					Citations: []apiCitation{
						{Time: "2026-11-04T15:00:00.123456789Z", Content: "我每周三去公园跑步"},
						{Time: "2026-11-04T16:00:00.123456789Z", Content: "我喜欢长跑"},
					},
				},
			},
			want: []string{
				"你常去公园跑步。",
				"状态", "ok",
				"2026-11-04T15:00:00.123456789Z", "我每周三去公园跑步",
				"2026-11-04T16:00:00.123456789Z", "我喜欢长跑",
			},
		},
		{
			name: "查询没有引用时仍打印回答与状态",
			resp: &Response{
				Kind: "query",
				Query: &apiQuery{
					Kind:   "query",
					Answer: "没有找到相关记忆",
					Status: "no_match",
				},
			},
			want: []string{"没有找到相关记忆", "状态", "no_match"},
		},
		{
			name: "无法识别说明这条没有被保存",
			resp: &Response{
				Kind:         "unrecognized",
				Unrecognized: &apiUnrecognized{Kind: "unrecognized"},
			},
			want: []string{"无法识别，这条没有被保存"},
		},
		{
			name: "错误打印错误码与信息",
			resp: &Response{
				Err: &apiError{Code: "model_unavailable", Message: "模型暂时不可用"},
			},
			want: []string{"model_unavailable", "模型暂时不可用"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatResponse(tt.resp)
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("formatResponse() = %q，期望包含 %q", got, want)
				}
			}
		})
	}
}

func TestFormatResponseDoesNotLeakEmptyCitationContent(t *testing.T) {
	resp := &Response{
		Kind: "query",
		Query: &apiQuery{
			Kind:      "query",
			Answer:    "回答",
			Status:    "ok",
			Citations: []apiCitation{{Time: "2026-11-04T15:00:00.123456789Z"}},
		},
	}
	got := formatResponse(resp)
	if !strings.Contains(got, "2026-11-04T15:00:00.123456789Z") {
		t.Errorf("formatResponse() = %q，期望包含引用时间", got)
	}
}
