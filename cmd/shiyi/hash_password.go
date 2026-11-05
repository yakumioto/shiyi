package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

// runHashPassword is an offline management command. Only the resulting hash
// goes to stdout; neither command arguments nor serve configuration are read.
func runHashPassword(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) int {
	input, ok := stdin.(*os.File)
	if !ok || !term.IsTerminal(int(input.Fd())) {
		fmt.Fprintln(stderr, "shiyi: hash-password 需要交互式 TTY；不接受管道输入")
		return exitUsage
	}
	fd := int(input.Fd())
	state, err := term.GetState(fd)
	if err != nil {
		fmt.Fprintln(stderr, "shiyi: 无法读取终端状态")
		return exitFailure
	}
	// Also restore echo when the signal context cancels a blocked read.
	defer func() { _ = term.Restore(fd, state) }()
	readPassword := func(prompt string) ([]byte, error) {
		fmt.Fprint(stderr, prompt)
		type result struct {
			password []byte
			err      error
		}
		ch := make(chan result)
		go func() {
			password, err := term.ReadPassword(fd)
			select {
			case ch <- result{password, err}:
			case <-ctx.Done():
				clear(password)
			}
		}()
		select {
		case r := <-ch:
			fmt.Fprintln(stderr)
			return r.password, r.err
		case <-ctx.Done():
			fmt.Fprintln(stderr)
			return nil, ctx.Err()
		}
	}
	password, err := readPassword("密码: ")
	defer clear(password)
	if err != nil {
		fmt.Fprintln(stderr, "shiyi: 密码读取中止")
		return exitFailure
	}
	if len(password) == 0 || len(password) > 72 {
		fmt.Fprintln(stderr, "shiyi: 密码必须为 1–72 个 UTF-8 字节")
		return exitFailure
	}
	confirmation, err := readPassword("再次输入密码: ")
	defer clear(confirmation)
	if err != nil || !bytes.Equal(password, confirmation) {
		fmt.Fprintln(stderr, "shiyi: 密码确认失败")
		return exitFailure
	}
	hash, err := bcrypt.GenerateFromPassword(password, 12)
	if err != nil {
		fmt.Fprintln(stderr, "shiyi: 密码哈希生成失败")
		return exitFailure
	}
	if _, err := fmt.Fprintln(stdout, string(hash)); err != nil {
		fmt.Fprintln(stderr, "shiyi: 无法写出密码哈希")
		return exitFailure
	}
	return exitOK
}
