package main

import (
	"testing"
)

func TestNewSender(t *testing.T) {
	t.Setenv(cliTokenEnv, "s3cret")

	snd, err := newSender(Options{Mode: ModeSend, Server: "http://127.0.0.1:8787", Text: "你好"})
	if err != nil {
		t.Fatalf("newSender 返回错误: %v", err)
	}
	client, ok := snd.(*Client)
	if !ok {
		t.Fatalf("newSender 返回 %T，期望 *Client", snd)
	}
	if client.BaseURL != "http://127.0.0.1:8787" {
		t.Errorf("BaseURL = %q", client.BaseURL)
	}
	if client.Token != "s3cret" {
		t.Errorf("Token = %q", client.Token)
	}
}

func TestNewSenderWithoutToken(t *testing.T) {
	t.Setenv(cliTokenEnv, "")

	snd, err := newSender(Options{Mode: ModeSend, Server: "http://127.0.0.1:8787"})
	if err != nil {
		t.Fatalf("newSender 返回错误: %v", err)
	}
	client := snd.(*Client)
	if client.Token != "" {
		t.Errorf("未设置 token 时 Token = %q，期望空", client.Token)
	}
}

func TestNewSenderUsesConfiguredServer(t *testing.T) {
	snd, err := newSender(Options{Mode: ModeREPL, Server: "http://example.com:9000"})
	if err != nil {
		t.Fatalf("newSender 返回错误: %v", err)
	}
	if snd.(*Client).BaseURL != "http://example.com:9000" {
		t.Errorf("BaseURL = %q", snd.(*Client).BaseURL)
	}
}

func TestIsQuitCommand(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"/quit", true},
		{"/exit", true},
		{"/QUIT", true},
		{"/quit ", true},
		{" /exit", true},
		{"/quitnow", false},
		{"退出", false},
		{"quit", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isQuitCommand(tt.line); got != tt.want {
			t.Errorf("isQuitCommand(%q) = %v，期望 %v", tt.line, got, tt.want)
		}
	}
}
