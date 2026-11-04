package main

import (
	"fmt"
	"net/url"
	"strings"
)

// defaultServer is the address the CLI talks to when --server is not given.
const defaultServer = "http://127.0.0.1:8787"

// Mode is what the CLI was asked to do.
type Mode int

const (
	// ModeSend sends a single message and exits.
	ModeSend Mode = iota
	// ModeREPL reads messages line by line until the user quits.
	ModeREPL
	// ModeServe starts the local service.
	ModeServe
	// ModeHelp prints the usage text and exits.
	ModeHelp
)

// Options is the parsed command line.
type Options struct {
	// Mode is what the CLI was asked to do.
	Mode Mode
	// Server is the base address of the Shiyi service.
	Server string
	// Text is the single message to send; only meaningful for ModeSend.
	Text string
}

// usageText is printed for --help and -h.
const usageText = `shiyi — Chat-first 的个人记忆系统

用法:
  shiyi                    进入交互式会话，逐行发送消息，用 /quit 退出
  shiyi <文本>             发送一条消息并打印结果
  shiyi serve              启动本地服务
  shiyi --help             显示本帮助

选项:
  --server <地址>          服务地址，默认 ` + defaultServer + `

交互式会话:
  每行单独发送，不携带任何上下文；空行忽略；/quit 或 /exit 退出。
`

// parseArgs turns a command line into Options.
//
// Flags may appear anywhere. Everything that is not a flag is either the
// "serve" sub-command or the text of a single message.
func parseArgs(args []string) (Options, error) {
	opts := Options{Mode: ModeREPL, Server: defaultServer}

	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			positional = append(positional, args[i+1:]...)
			i = len(args)
		case arg == "-h" || arg == "-help" || arg == "--help":
			opts.Mode = ModeHelp
		case arg == "--server" || arg == "-server":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s 缺少取值", arg)
			}
			i++
			opts.Server = args[i]
		case strings.HasPrefix(arg, "--server=") || strings.HasPrefix(arg, "-server="):
			opts.Server = strings.SplitN(arg, "=", 2)[1]
		case strings.HasPrefix(arg, "-"):
			return opts, fmt.Errorf("未知选项 %q", arg)
		default:
			positional = append(positional, arg)
		}
	}

	if err := validateServer(opts.Server); err != nil {
		return opts, err
	}

	switch {
	case len(positional) == 0:
		if opts.Mode != ModeHelp {
			opts.Mode = ModeREPL
		}
	case positional[0] == "serve":
		if len(positional) > 1 {
			return opts, fmt.Errorf("serve 不接受额外参数: %q", strings.Join(positional[1:], " "))
		}
		if opts.Mode != ModeHelp {
			opts.Mode = ModeServe
		}
	default:
		if opts.Mode != ModeHelp {
			opts.Mode = ModeSend
		}
		opts.Text = strings.Join(positional, " ")
	}

	return opts, nil
}

// validateServer rejects a --server value that is not a usable base address.
func validateServer(server string) error {
	if strings.TrimSpace(server) == "" {
		return fmt.Errorf("服务地址不能为空")
	}
	parsed, err := url.Parse(server)
	if err != nil {
		return fmt.Errorf("服务地址 %q 无效: %w", server, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("服务地址 %q 必须以 http:// 或 https:// 开头", server)
	}
	if parsed.Host == "" {
		return fmt.Errorf("服务地址 %q 缺少主机名", server)
	}
	return nil
}
