package main

import "testing"

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    Options
		wantErr bool
	}{
		{
			name: "无参数进入交互式会话",
			args: nil,
			want: Options{Mode: ModeREPL, Server: defaultServer},
		},
		{
			name: "serve 子命令",
			args: []string{"serve"},
			want: Options{Mode: ModeServe, Server: defaultServer},
		},
		{
			name: "一次性发送文本",
			args: []string{"我喜欢跑步"},
			want: Options{Mode: ModeSend, Server: defaultServer, Text: "我喜欢跑步"},
		},
		{
			name: "多个参数拼成一条文本",
			args: []string{"我", "喜欢", "跑步"},
			want: Options{Mode: ModeSend, Server: defaultServer, Text: "我 喜欢 跑步"},
		},
		{
			name: "指定服务地址",
			args: []string{"--server", "http://example.com:9000", "你好"},
			want: Options{Mode: ModeSend, Server: "http://example.com:9000", Text: "你好"},
		},
		{
			name: "等号形式指定服务地址",
			args: []string{"--server=http://example.com:9000", "你好"},
			want: Options{Mode: ModeSend, Server: "http://example.com:9000", Text: "你好"},
		},
		{
			name: "交互式会话可指定服务地址",
			args: []string{"--server", "http://example.com:9000"},
			want: Options{Mode: ModeREPL, Server: "http://example.com:9000"},
		},
		{
			name: "help",
			args: []string{"--help"},
			want: Options{Mode: ModeHelp, Server: defaultServer},
		},
		{
			name: "h",
			args: []string{"-h"},
			want: Options{Mode: ModeHelp, Server: defaultServer},
		},
		{
			name: "serve 之后的 help",
			args: []string{"serve", "--help"},
			want: Options{Mode: ModeHelp, Server: defaultServer},
		},
		{
			name:    "serve 不接受额外参数",
			args:    []string{"serve", "多余"},
			wantErr: true,
		},
		{
			name: "双横线之后的文本当作消息",
			args: []string{"--", "-5度"},
			want: Options{Mode: ModeSend, Server: defaultServer, Text: "-5度"},
		},
		{
			name:    "未知选项",
			args:    []string{"--unknown"},
			wantErr: true,
		},
		{
			name:    "服务地址缺少 scheme",
			args:    []string{"--server", "example.com:9000", "你好"},
			wantErr: true,
		},
		{
			name:    "服务地址缺少主机名",
			args:    []string{"--server", "http://", "你好"},
			wantErr: true,
		},
		{
			name:    "服务地址不是 URL",
			args:    []string{"--server", "://坏地址", "你好"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseArgs(%q) = %+v，期望返回错误", tt.args, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseArgs(%q) 返回错误: %v", tt.args, err)
			}
			if got != tt.want {
				t.Errorf("parseArgs(%q) = %+v，期望 %+v", tt.args, got, tt.want)
			}
		})
	}
}

func TestUsageMentionsServerAndQuit(t *testing.T) {
	for _, want := range []string{"--server", "/quit", "serve", defaultServer} {
		if !contains(usageText, want) {
			t.Errorf("帮助文本缺少 %q", want)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
