package main

import "testing"

func TestParseArgs(t *testing.T) {
	for _, tt := range []struct {
		name    string
		args    []string
		want    Options
		wantErr bool
	}{
		{"serve 子命令", []string{"serve"}, Options{ModeServe}, false},
		{"hash-password", []string{"hash-password"}, Options{ModeHashPassword}, false},
		{"help", []string{"--help"}, Options{ModeHelp}, false},
		{"h", []string{"-h"}, Options{ModeHelp}, false},
		{"version", []string{"--version"}, Options{ModeVersion}, false},
		{"serve 之后的 help", []string{"serve", "--help"}, Options{ModeHelp}, false},
		{"serve 不接受额外参数", []string{"serve", "多余"}, Options{}, true},
		{"serve 不接受 --server", []string{"serve", "--server", "http://localhost"}, Options{}, true},
		{"未知选项", []string{"--unknown"}, Options{}, true},
		{"无参数不进入REPL", nil, Options{}, true},
		{"一次性发送已移除", []string{"我的原文"}, Options{}, true},
		{"旧服务地址已移除", []string{"--server=http://localhost"}, Options{}, true},
		{"双横线不发送原文", []string{"--", "原文"}, Options{}, true},
		{"hash-password 不接受密码参数", []string{"hash-password", "secret"}, Options{}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseArgs(tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, want error %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("options = %+v, want %+v", got, tt.want)
			}
		})
	}
}
