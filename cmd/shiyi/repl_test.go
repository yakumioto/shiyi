package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// fakeSender records every message it is given and answers with a canned
// response, so the REPL can be tested without a network.
type fakeSender struct {
	requests []string
	answers  []*Response
	err      error
}

func (f *fakeSender) Send(ctx context.Context, content string) (*Response, error) {
	f.requests = append(f.requests, content)
	if f.err != nil {
		return nil, f.err
	}
	if len(f.answers) == 0 {
		return &Response{Kind: "unrecognized", Unrecognized: &apiUnrecognized{Kind: "unrecognized"}}, nil
	}
	answer := f.answers[0]
	if len(f.answers) > 1 {
		f.answers = f.answers[1:]
	}
	return answer, nil
}

func TestREPL(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		answers     []*Response
		wantReqs    []string
		wantOutput  []string
		wantErrs    []string
		wantExit    int
		wantErrFail bool
	}{
		{
			name:  "每行独立发送且不携带上下文",
			input: "第一句\n第二句\n",
			answers: []*Response{
				{Kind: "record", Record: &apiRecord{Kind: "record", Time: "2026-11-04T15:00:00.123456789Z", Version: 1}},
				{Kind: "query", Query: &apiQuery{Kind: "query", Answer: "回答", Status: "ok"}},
			},
			wantReqs:   []string{"第一句", "第二句"},
			wantOutput: []string{"2026-11-04T15:00:00.123456789Z", "回答"},
		},
		{
			name:       "空行被忽略",
			input:      "\n\n   \n只有一句\n",
			wantReqs:   []string{"只有一句"},
			wantOutput: []string{"无法识别"},
		},
		{
			name:     "quit 退出",
			input:    "一句话\n/quit\n不该发送\n",
			wantReqs: []string{"一句话"},
		},
		{
			name:     "exit 退出",
			input:    "一句话\n/exit\n不该发送\n",
			wantReqs: []string{"一句话"},
		},
		{
			name:     "quit 前后的空行不影响退出",
			input:    "/quit\n",
			wantReqs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sender := &fakeSender{answers: tt.answers}
			var out, errOut bytes.Buffer

			code := runREPL(context.Background(), sender, strings.NewReader(tt.input), &out, &errOut)

			if code != tt.wantExit {
				t.Errorf("退出码 = %d，期望 %d", code, tt.wantExit)
			}
			if len(sender.requests) != len(tt.wantReqs) {
				t.Fatalf("发送了 %d 条请求 %q，期望 %d 条 %q", len(sender.requests), sender.requests, len(tt.wantReqs), tt.wantReqs)
			}
			for i, want := range tt.wantReqs {
				if sender.requests[i] != want {
					t.Errorf("第 %d 条请求 = %q，期望 %q", i, sender.requests[i], want)
				}
			}
			for _, want := range tt.wantOutput {
				if !strings.Contains(out.String(), want) {
					t.Errorf("输出 %q 缺少 %q", out.String(), want)
				}
			}
			for _, want := range tt.wantErrs {
				if !strings.Contains(errOut.String(), want) {
					t.Errorf("错误输出 %q 缺少 %q", errOut.String(), want)
				}
			}
		})
	}
}

func TestREPLReportsServiceErrorToStderr(t *testing.T) {
	sender := &fakeSender{answers: []*Response{
		{Err: &apiError{Code: "model_unavailable", Message: "模型暂时不可用"}},
	}}
	var out, errOut bytes.Buffer

	code := runREPL(context.Background(), sender, strings.NewReader("你好\n/quit\n"), &out, &errOut)

	if code == 0 {
		t.Errorf("服务出错时退出码应非零，得到 %d", code)
	}
	if !strings.Contains(errOut.String(), "model_unavailable") {
		t.Errorf("错误输出 %q 缺少错误码", errOut.String())
	}
	if !strings.Contains(errOut.String(), "模型暂时不可用") {
		t.Errorf("错误输出 %q 缺少错误信息", errOut.String())
	}
	if strings.Contains(out.String(), "model_unavailable") {
		t.Errorf("错误码不应出现在标准输出: %q", out.String())
	}
}

func TestREPLContinuesAfterError(t *testing.T) {
	sender := &fakeSender{}
	sender.err = errors.New("连接被拒绝")

	var out, errOut bytes.Buffer
	_ = runREPL(context.Background(), sender, strings.NewReader("一\n二\n"), &out, &errOut)

	if len(sender.requests) != 2 {
		t.Errorf("发送了 %d 条请求，期望会话在错误后继续", len(sender.requests))
	}
}

// fakeFactory hands out the fake sender to the command dispatch.
func fakeFactory(snd sender) senderFactory {
	return func(Options) (sender, error) { return snd, nil }
}

func TestRunSendMode(t *testing.T) {
	sender := &fakeSender{answers: []*Response{
		{Kind: "record", Record: &apiRecord{Kind: "record", Time: "2026-11-04T15:00:00.123456789Z", Version: 1}},
	}}
	var out, errOut bytes.Buffer

	code := run(context.Background(), []string{"我喜欢跑步"}, fakeFactory(sender), strings.NewReader(""), &out, &errOut)

	if code != 0 {
		t.Errorf("退出码 = %d，期望 0", code)
	}
	if len(sender.requests) != 1 || sender.requests[0] != "我喜欢跑步" {
		t.Errorf("请求 = %q", sender.requests)
	}
	if !strings.Contains(out.String(), "2026-11-04T15:00:00.123456789Z") {
		t.Errorf("输出 %q 缺少记录时间", out.String())
	}
}

func TestRunSendModeReportsServiceError(t *testing.T) {
	sender := &fakeSender{answers: []*Response{
		{Err: &apiError{Code: "model_unavailable", Message: "模型暂时不可用"}},
	}}
	var out, errOut bytes.Buffer

	code := run(context.Background(), []string{"你好"}, fakeFactory(sender), strings.NewReader(""), &out, &errOut)

	if code == 0 {
		t.Error("服务出错时退出码应非零")
	}
	if !strings.Contains(errOut.String(), "model_unavailable") {
		t.Errorf("错误输出 %q 缺少错误码", errOut.String())
	}
}

func TestRunHelp(t *testing.T) {
	sender := &fakeSender{}
	var out, errOut bytes.Buffer

	code := run(context.Background(), []string{"--help"}, fakeFactory(sender), strings.NewReader(""), &out, &errOut)

	if code != 0 {
		t.Errorf("退出码 = %d，期望 0", code)
	}
	if !strings.Contains(out.String(), "--server") {
		t.Errorf("帮助输出 %q 缺少 --server", out.String())
	}
	if len(sender.requests) != 0 {
		t.Errorf("帮助不应发送请求，得到 %q", sender.requests)
	}
}

func TestRunUsageError(t *testing.T) {
	sender := &fakeSender{}
	var out, errOut bytes.Buffer

	code := run(context.Background(), []string{"--unknown"}, fakeFactory(sender), strings.NewReader(""), &out, &errOut)

	if code == 0 {
		t.Error("用法错误时退出码应非零")
	}
	if errOut.Len() == 0 {
		t.Error("用法错误应写入标准错误")
	}
}

func TestRunREPLMode(t *testing.T) {
	sender := &fakeSender{}
	var out, errOut bytes.Buffer

	code := run(context.Background(), nil, fakeFactory(sender), strings.NewReader("/quit\n"), &out, &errOut)

	if code != 0 {
		t.Errorf("退出码 = %d，期望 0", code)
	}
}

var _ io.Reader = strings.NewReader("")
