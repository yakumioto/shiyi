package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeService is a Shiyi service that answers POST /messages with canned
// bodies. It lets the CLI be exercised end to end over HTTP without a
// network or a model.
type fakeService struct {
	responses []string
	statuses  []int
	requests  []string
	auth      []string
}

func (f *fakeService) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, _ := io.ReadAll(r.Body)
		f.requests = append(f.requests, string(payload))
		f.auth = append(f.auth, r.Header.Get("Authorization"))

		status := http.StatusOK
		body := `{"kind":"unrecognized"}`
		if len(f.statuses) > 0 {
			status = f.statuses[0]
			f.statuses = f.statuses[1:]
		}
		if len(f.responses) > 0 {
			body = f.responses[0]
			f.responses = f.responses[1:]
		}
		w.WriteHeader(status)
		io.WriteString(w, body)
	})
}

func TestCLIEndToEnd(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		stdin      string
		service    *fakeService
		wantCode   int
		wantOut    []string
		wantErr    []string
		wantNotOut []string
		wantReqs   int
	}{
		{
			name: "一次性记录打印记录时间",
			args: []string{"我喜欢跑步"},
			service: &fakeService{
				statuses:  []int{http.StatusCreated},
				responses: []string{`{"kind":"record","time":"2026-11-04T15:00:00.123456789Z","version":1}`},
			},
			wantCode: 0,
			wantOut:  []string{"记录时间", "2026-11-04T15:00:00.123456789Z"},
			wantReqs: 1,
		},
		{
			name: "一次性查询打印回答状态与引用原文",
			args: []string{"我什么时候跑步"},
			service: &fakeService{
				statuses:  []int{http.StatusOK},
				responses: []string{`{"kind":"query","answer":"你每周三跑步","citations":[{"time":"2026-11-04T15:00:00.123456789Z","content":"我每周三去公园跑步"}],"status":"ok"}`},
			},
			wantCode: 0,
			wantOut:  []string{"你每周三跑步", "状态", "ok", "2026-11-04T15:00:00.123456789Z", "我每周三去公园跑步"},
			wantReqs: 1,
		},
		{
			name: "一次性无法识别说明没有保存",
			args: []string{"嗯"},
			service: &fakeService{
				statuses:  []int{http.StatusOK},
				responses: []string{`{"kind":"unrecognized"}`},
			},
			wantCode: 0,
			wantOut:  []string{"无法识别，这条没有被保存"},
			wantReqs: 1,
		},
		{
			name: "服务返回错误时写 stderr 并以非零退出码结束",
			args: []string{"你好"},
			service: &fakeService{
				statuses:  []int{http.StatusServiceUnavailable},
				responses: []string{`{"error":{"code":"model_unavailable","message":"模型暂时不可用"}}`},
			},
			wantCode: 1,
			wantErr:  []string{"model_unavailable", "模型暂时不可用"},
			wantReqs: 1,
		},
		{
			name:  "交互式会话逐行独立发送",
			args:  nil,
			stdin: "第一句\n\n第二句\n/quit\n不该发送\n",
			service: &fakeService{
				statuses: []int{http.StatusCreated, http.StatusOK},
				responses: []string{
					`{"kind":"record","time":"2026-11-04T15:00:00.123456789Z","version":1}`,
					`{"kind":"query","answer":"回答","citations":[],"status":"ok"}`,
				},
			},
			wantCode: 0,
			wantOut:  []string{"2026-11-04T15:00:00.123456789Z", "回答"},
			wantReqs: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.service.handler())
			defer srv.Close()

			var out, errOut bytes.Buffer
			code := run(
				context.Background(),
				append(tt.args, "--server", srv.URL),
				newSender,
				strings.NewReader(tt.stdin),
				&out,
				&errOut,
			)

			if code != tt.wantCode {
				t.Errorf("退出码 = %d，期望 %d（stderr: %q）", code, tt.wantCode, errOut.String())
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("标准输出 %q 缺少 %q", out.String(), want)
				}
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(errOut.String(), want) {
					t.Errorf("标准错误 %q 缺少 %q", errOut.String(), want)
				}
			}
			for _, notWant := range tt.wantNotOut {
				if strings.Contains(out.String(), notWant) {
					t.Errorf("标准输出 %q 不应包含 %q", out.String(), notWant)
				}
			}
			if len(tt.service.requests) != tt.wantReqs {
				t.Errorf("服务收到 %d 个请求 %q，期望 %d 个", len(tt.service.requests), tt.service.requests, tt.wantReqs)
			}
		})
	}
}

func TestCLIEndToEndSendsEachLineSeparately(t *testing.T) {
	service := &fakeService{
		statuses: []int{http.StatusCreated, http.StatusCreated},
		responses: []string{
			`{"kind":"record","time":"2026-11-04T15:00:00.123456789Z","version":1}`,
			`{"kind":"record","time":"2026-11-04T16:00:00.123456789Z","version":1}`,
		},
	}
	srv := httptest.NewServer(service.handler())
	defer srv.Close()

	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"--server", srv.URL}, newSender, strings.NewReader("第一句\n第二句\n/quit\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("退出码 = %d，stderr: %q", code, errOut.String())
	}

	if len(service.requests) != 2 {
		t.Fatalf("服务收到 %d 个请求，期望 2", len(service.requests))
	}
	if !strings.Contains(service.requests[0], "第一句") || strings.Contains(service.requests[0], "第二句") {
		t.Errorf("第一个请求 %q 应只含第一句", service.requests[0])
	}
	if !strings.Contains(service.requests[1], "第二句") || strings.Contains(service.requests[1], "第一句") {
		t.Errorf("第二个请求 %q 应只含第二句", service.requests[1])
	}
}

func TestCLIEndToEndSendsBearerToken(t *testing.T) {
	t.Setenv(cliTokenEnv, "s3cret")

	service := &fakeService{statuses: []int{http.StatusOK}, responses: []string{`{"kind":"unrecognized"}`}}
	srv := httptest.NewServer(service.handler())
	defer srv.Close()

	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"你好", "--server", srv.URL}, newSender, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("退出码 = %d，stderr: %q", code, errOut.String())
	}

	if len(service.auth) != 1 {
		t.Fatalf("服务收到 %d 个请求", len(service.auth))
	}
	if service.auth[0] != "Bearer s3cret" {
		t.Errorf("Authorization = %q，期望 %q", service.auth[0], "Bearer s3cret")
	}
}
