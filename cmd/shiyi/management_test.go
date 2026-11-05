package main

import (
	"context"
	"strings"
	"testing"
)

func TestHashPasswordRejectsArgumentsWithoutEchoingSecrets(t *testing.T) {
	const secret = "--misplaced-password-secret"
	var out, errOut strings.Builder
	code := run(context.Background(), []string{"hash-password", secret}, nil, strings.NewReader(""), &out, &errOut)
	if code != exitUsage {
		t.Fatalf("exit = %d, want usage failure", code)
	}
	if strings.Contains(out.String()+errOut.String(), secret) {
		t.Fatal("argument rejection echoed a possible password")
	}
}
