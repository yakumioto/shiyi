package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestFormatResponseWithoutPayload(t *testing.T) {
	tests := []struct {
		name string
		resp *Response
		want string
	}{
		{
			name: "空响应",
			resp: nil,
			want: "没有响应",
		},
		{
			name: "没有任何字段",
			resp: &Response{},
			want: "没有响应",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatResponse(tt.resp); got != tt.want {
				t.Errorf("formatResponse() = %q，期望 %q", got, tt.want)
			}
		})
	}
}

func TestParseResponseRejectsBadBodies(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "不是 JSON", body: `not json`},
		{name: "缺少 kind", body: `{"answer":"x"}`},
		{name: "未知 kind", body: `{"kind":"wat"}`},
		{name: "记录响应字段类型不对", body: `{"kind":"record","time":"x","version":"y"}`},
		{name: "查询响应字段类型不对", body: `{"kind":"query","answer":1}`},
		{name: "无法识别响应字段类型不对", body: `{"kind":1}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseResponse([]byte(tt.body)); err == nil {
				t.Errorf("parseResponse(%q) 期望返回错误", tt.body)
			}
		})
	}
}

func TestParseResponseAcceptsKinds(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "记录", body: `{"kind":"record","time":"2026-11-04T15:00:00.123456789Z","version":1}`, want: "record"},
		{name: "查询", body: `{"kind":"query","answer":"a","citations":[],"status":"ok"}`, want: "query"},
		{name: "无法识别", body: `{"kind":"unrecognized"}`, want: "unrecognized"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := parseResponse([]byte(tt.body))
			if err != nil {
				t.Fatalf("parseResponse() 返回错误: %v", err)
			}
			if resp.Kind != tt.want {
				t.Errorf("Kind = %q，期望 %q", resp.Kind, tt.want)
			}
		})
	}
}

func TestRunVersion(t *testing.T) {
	var out, errOut bytes.Buffer

	code := run(context.Background(), []string{"--version"}, fakeFactory(&fakeSender{}), strings.NewReader(""), &out, &errOut)

	if code != 0 {
		t.Errorf("退出码 = %d，期望 0", code)
	}
	if !strings.Contains(out.String(), "shiyi") {
		t.Errorf("输出 %q 应含版本号", out.String())
	}
}

func TestRunSendWithoutSender(t *testing.T) {
	var out, errOut bytes.Buffer

	code := run(context.Background(), []string{"你好"}, fakeFactory(nil), strings.NewReader(""), &out, &errOut)

	if code == 0 {
		t.Error("没有服务连接时退出码应非零")
	}
	if errOut.Len() == 0 {
		t.Error("没有服务连接时应写入标准错误")
	}
}

func TestRunREPLWithoutSender(t *testing.T) {
	var out, errOut bytes.Buffer

	code := run(context.Background(), nil, fakeFactory(nil), strings.NewReader("你好\n"), &out, &errOut)

	if code == 0 {
		t.Error("没有服务连接时退出码应非零")
	}
	if errOut.Len() == 0 {
		t.Error("没有服务连接时应写入标准错误")
	}
}
