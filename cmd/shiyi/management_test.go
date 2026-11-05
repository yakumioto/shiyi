package main

import (
	"context"
	"strings"
	"testing"
)

func TestRemovedMemoryClientFailsWithoutEchoingInput(t *testing.T) {
	const secret = "private-legacy-input"
	for _, args := range [][]string{nil, {secret}, {"--server", secret}, {"--server=" + secret}, {"--", secret}} {
		var out, errOut strings.Builder
		code := run(context.Background(), args, strings.NewReader(secret), &out, &errOut)
		if code != exitUsage {
			t.Fatalf("removed memory client exit = %d, want usage failure", code)
		}
		if strings.Contains(out.String()+errOut.String(), secret) {
			t.Fatal("usage error echoed private input")
		}
	}
}

func TestHashPasswordRejectsArgumentsWithoutEchoingSecrets(t *testing.T) {
	const secret = "--misplaced-password-secret"
	for _, arg := range []string{secret, "password-marker", "--password=password-marker"} {
		var out, errOut strings.Builder
		code := run(context.Background(), []string{"hash-password", arg}, strings.NewReader(""), &out, &errOut)
		if code != exitUsage {
			t.Fatalf("exit = %d, want usage failure", code)
		}
		if strings.Contains(out.String()+errOut.String(), arg) {
			t.Fatal("argument rejection echoed a possible password")
		}
	}
}

func TestHashPasswordRejectsNonTerminalWithoutReadingServeConfig(t *testing.T) {
	t.Setenv("SHIYI_CONFIG", "/missing-shiyi-management-config")
	var out, errOut strings.Builder
	code := run(context.Background(), []string{"hash-password"}, strings.NewReader("pipe-password-marker\n"), &out, &errOut)
	if code != exitUsage || out.Len() != 0 || !strings.Contains(errOut.String(), "TTY") {
		t.Fatal("non-TTY hash input was not safely refused")
	}
	if strings.Contains(errOut.String(), "pipe-password-marker") || strings.Contains(errOut.String(), "missing-shiyi") {
		t.Fatal("hash management leaked input or loaded serve config")
	}
}

func TestRunHelp(t *testing.T) {
	t.Setenv("SHIYI_CONFIG", "/missing-shiyi-management-config")
	var out, errOut strings.Builder
	code := run(context.Background(), []string{"--help"}, strings.NewReader(""), &out, &errOut)
	if code != exitOK || errOut.Len() != 0 {
		t.Fatal("help failed")
	}
	for _, want := range []string{"serve", "hash-password", "--version"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("help lacks %s", want)
		}
	}
	for _, removed := range []string{"--server", "/quit"} {
		if strings.Contains(out.String(), removed) {
			t.Fatal("help advertises removed client")
		}
	}
}

func TestRunUsageError(t *testing.T) {
	var out, errOut strings.Builder
	if run(context.Background(), []string{"--unknown"}, strings.NewReader(""), &out, &errOut) != exitUsage || errOut.Len() == 0 {
		t.Fatal("usage error must fail with service usage")
	}
}

func TestRunVersion(t *testing.T) {
	t.Setenv("SHIYI_CONFIG", "/missing-shiyi-management-config")
	var out, errOut strings.Builder
	if run(context.Background(), []string{"--version"}, strings.NewReader(""), &out, &errOut) != exitOK || !strings.Contains(out.String(), "shiyi") || errOut.Len() != 0 {
		t.Fatal("version failed")
	}
}
