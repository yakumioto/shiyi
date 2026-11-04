package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// errSendFailed is returned when a message could not be delivered at all.
var errSendFailed = errors.New("发送失败")

// Exit codes the CLI returns.
const (
	// exitOK means everything worked.
	exitOK = 0
	// exitFailure means the service refused the request or it failed.
	exitFailure = 1
	// exitUsage means the command line could not be understood.
	exitUsage = 2
)

// sender sends one message to the service and returns the answer.
//
// The CLI talks to the service through this seam so that the command
// dispatch can be tested without a network.
type sender interface {
	Send(ctx context.Context, content string) (*Response, error)
}

// senderFactory builds the client that talks to the service.
//
// The factory is given the parsed options so that the server address and the
// credentials can be derived from the command line.
type senderFactory func(Options) (sender, error)

// run dispatches a command line and returns the process exit code.
//
// The factory is only called for the modes that talk to the service.
func run(ctx context.Context, args []string, factory senderFactory, stdin io.Reader, stdout, stderr io.Writer) int {
	opts, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "shiyi: %v\n\n%s", err, usageText)
		return exitUsage
	}

	switch opts.Mode {
	case ModeHelp:
		fmt.Fprint(stdout, usageText)
		return exitOK
	case ModeServe:
		return runServe(ctx, opts, stdout, stderr)
	case ModeSend:
		snd, err := factory(opts)
		if err != nil {
			fmt.Fprintf(stderr, "shiyi: %v\n", err)
			return exitFailure
		}
		return runSend(ctx, snd, opts.Text, stdout, stderr)
	case ModeREPL:
		snd, err := factory(opts)
		if err != nil {
			fmt.Fprintf(stderr, "shiyi: %v\n", err)
			return exitFailure
		}
		return runREPL(ctx, snd, stdin, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "shiyi: 未知模式\n")
		return exitUsage
	}
}

// runSend sends one message and prints the answer.
//
// A message the service refused is reported on the standard error together
// with the error code the service gave, and the process ends with a non-zero
// exit code so that a script can tell success from failure.
func runSend(ctx context.Context, snd sender, text string, stdout, stderr io.Writer) int {
	if snd == nil {
		fmt.Fprintf(stderr, "shiyi: 没有可用的服务连接\n")
		return exitFailure
	}

	resp, err := snd.Send(ctx, text)
	if err != nil {
		fmt.Fprintf(stderr, "shiyi: %v\n", err)
		return exitFailure
	}
	if resp != nil && resp.Err != nil {
		fmt.Fprintf(stderr, "错误 %s: %s\n", resp.Err.Code, resp.Err.Message)
		return exitFailure
	}

	fmt.Fprintln(stdout, formatResponse(resp))
	return exitOK
}

// runREPL reads messages line by line and sends each one on its own.
//
// Every line is a separate request that carries no context of its own: the
// service sees exactly what the user typed. Blank lines are ignored and
// /quit (or /exit) ends the session.
func runREPL(ctx context.Context, snd sender, stdin io.Reader, stdout, stderr io.Writer) int {
	if snd == nil {
		fmt.Fprintf(stderr, "shiyi: 没有可用的服务连接\n")
		return exitFailure
	}

	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	failed := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if isQuitCommand(line) {
			break
		}

		resp, err := snd.Send(ctx, line)
		if err != nil {
			fmt.Fprintf(stderr, "shiyi: %v\n", err)
			failed = true
			continue
		}
		if resp != nil && resp.Err != nil {
			fmt.Fprintf(stderr, "错误 %s: %s\n", resp.Err.Code, resp.Err.Message)
			failed = true
			continue
		}

		fmt.Fprintln(stdout, formatResponse(resp))
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(stderr, "shiyi: 读取输入失败: %v\n", err)
		return exitFailure
	}

	if failed {
		return exitFailure
	}
	return exitOK
}

// isQuitCommand reports whether a line ends the session.
func isQuitCommand(line string) bool {
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "/quit", "/exit":
		return true
	}
	return false
}
