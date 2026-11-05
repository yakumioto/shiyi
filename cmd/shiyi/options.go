package main

import "fmt"

// Mode selects a service management operation, never a memory client.
type Mode int

const (
	ModeServe Mode = iota
	ModeHelp
	ModeVersion
	ModeHashPassword
)

type Options struct {
	Mode Mode
}

const usageText = `shiyi — 单用户 Web 个人记忆系统

用法:
  shiyi serve              启动服务，监听地址取自配置文件
  shiyi hash-password      在 TTY 中不回显输入并确认密码，输出 bcrypt 哈希
  shiyi --help             显示本帮助
  shiyi --version          显示版本号

记忆客户端已移除；请通过配置的 HTTPS Web 页面登录、记录和查询。
hash-password 不接受任何参数或管道输入，不加载服务配置。
`

func parseArgs(args []string) (Options, error) {
	if len(args) == 1 {
		switch args[0] {
		case "serve":
			return Options{Mode: ModeServe}, nil
		case "hash-password":
			return Options{Mode: ModeHashPassword}, nil
		case "help", "-h", "-help", "--help":
			return Options{Mode: ModeHelp}, nil
		case "version", "-v", "-version", "--version":
			return Options{Mode: ModeVersion}, nil
		}
	}
	if len(args) == 2 && args[0] == "serve" && (args[1] == "--help" || args[1] == "-h") {
		return Options{Mode: ModeHelp}, nil
	}
	if len(args) > 0 && args[0] == "hash-password" {
		return Options{}, fmt.Errorf("hash-password 不接受参数；请在终端交互输入密码")
	}
	// Unknown arguments may contain a password or private original text.
	return Options{}, fmt.Errorf("无效的服务管理命令；记忆客户端已移除，请使用 Web")
}
