package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientSend(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
		check   func(t *testing.T, resp *Response)
	}{
		{
			name:   "记录响应",
			status: http.StatusCreated,
			body:   `{"kind":"record","time":"2026-11-04T15:00:00.123456789Z","version":1}`,
			check: func(t *testing.T, resp *Response) {
				if resp.Err != nil {
					t.Fatalf("期望成功响应，得到错误 %+v", resp.Err)
				}
				if resp.Record == nil {
					t.Fatal("期望得到记录")
				}
				if resp.Record.Time != "2026-11-04T15:00:00.123456789Z" {
					t.Errorf("记录时间 = %q", resp.Record.Time)
				}
				if resp.Record.Version != 1 {
					t.Errorf("版本 = %d", resp.Record.Version)
				}
			},
		},
		{
			name:   "查询响应",
			status: http.StatusOK,
			body:   `{"kind":"query","answer":"你每周三跑步","citations":[{"time":"2026-11-04T15:00:00.123456789Z","content":"我每周三去公园跑步"}],"status":"ok"}`,
			check: func(t *testing.T, resp *Response) {
				if resp.Query == nil {
					t.Fatal("期望得到查询结果")
				}
				if resp.Query.Answer != "你每周三跑步" {
					t.Errorf("回答 = %q", resp.Query.Answer)
				}
				if resp.Query.Status != "ok" {
					t.Errorf("状态 = %q", resp.Query.Status)
				}
				if len(resp.Query.Citations) != 1 || resp.Query.Citations[0].Content != "我每周三去公园跑步" {
					t.Errorf("引用 = %+v", resp.Query.Citations)
				}
			},
		},
		{
			name:   "无法识别响应",
			status: http.StatusOK,
			body:   `{"kind":"unrecognized"}`,
			check: func(t *testing.T, resp *Response) {
				if resp.Unrecognized == nil {
					t.Fatal("期望得到无法识别")
				}
				if resp.Record != nil || resp.Query != nil {
					t.Error("无法识别时不应带记录或查询")
				}
			},
		},
		{
			name:   "错误响应体解析出错误码与信息",
			status: http.StatusServiceUnavailable,
			body:   `{"error":{"code":"model_unavailable","message":"模型暂时不可用"}}`,
			check: func(t *testing.T, resp *Response) {
				if resp.Err == nil {
					t.Fatal("期望得到错误")
				}
				if resp.Err.Code != "model_unavailable" {
					t.Errorf("错误码 = %q", resp.Err.Code)
				}
				if resp.Err.Message != "模型暂时不可用" {
					t.Errorf("错误信息 = %q", resp.Err.Message)
				}
			},
		},
		{
			name:   "错误响应体无法解析时保留状态码",
			status: http.StatusInternalServerError,
			body:   `not json`,
			check: func(t *testing.T, resp *Response) {
				if resp.Err == nil {
					t.Fatal("期望得到错误")
				}
				if !strings.Contains(resp.Err.Message, "500") {
					t.Errorf("错误信息应含状态码: %q", resp.Err.Message)
				}
			},
		},
		{
			name:   "错误响应体为空时保留状态码",
			status: http.StatusUnauthorized,
			body:   ``,
			check: func(t *testing.T, resp *Response) {
				if resp.Err == nil {
					t.Fatal("期望得到错误")
				}
				if !strings.Contains(resp.Err.Message, "401") {
					t.Errorf("错误信息应含状态码: %q", resp.Err.Message)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				io.WriteString(w, tt.body)
			}))
			defer srv.Close()

			client := &Client{BaseURL: srv.URL}
			resp, err := client.Send(context.Background(), "你好")
			if err != nil {
				t.Fatalf("Send 返回错误: %v", err)
			}
			tt.check(t, resp)
		})
	}
}

func TestClientSendSendsMessageRequest(t *testing.T) {
	var gotPath, gotMethod, gotContentType string
	var gotBody apiMessageRequest
	var gotAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		gotAuth = r.Header.Get("Authorization")
		payload, _ := io.ReadAll(r.Body)
		json.Unmarshal(payload, &gotBody)
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"kind":"record","time":"2026-11-04T15:00:00.123456789Z","version":1}`)
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL}
	if _, err := client.Send(context.Background(), "我喜欢跑步"); err != nil {
		t.Fatalf("Send 返回错误: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("方法 = %q，期望 POST", gotMethod)
	}
	if gotPath != "/messages" {
		t.Errorf("路径 = %q，期望 /messages", gotPath)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q", gotContentType)
	}
	if gotBody.Content != "我喜欢跑步" {
		t.Errorf("请求体 content = %q", gotBody.Content)
	}
	if gotAuth != "" {
		t.Errorf("未配置 token 时不应发送 Authorization 头，得到 %q", gotAuth)
	}
}

func TestClientSendSendsBearerToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{"kind":"unrecognized"}`)
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "s3cret"}
	if _, err := client.Send(context.Background(), "你好"); err != nil {
		t.Fatalf("Send 返回错误: %v", err)
	}
	if gotAuth != "Bearer s3cret" {
		t.Errorf("Authorization = %q，期望 %q", gotAuth, "Bearer s3cret")
	}
}

func TestClientSendReportsUnreachableService(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	client := &Client{BaseURL: url}
	if _, err := client.Send(context.Background(), "你好"); err == nil {
		t.Fatal("期望连接失败时返回错误")
	}
}

func TestClientSendRejectsUnknownResponseKind(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"kind":"wat"}`)
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL}
	if _, err := client.Send(context.Background(), "你好"); err == nil {
		t.Fatal("期望未知 kind 时返回错误")
	}
}
