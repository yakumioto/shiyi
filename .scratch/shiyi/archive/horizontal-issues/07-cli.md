# 07 - CLI

Status: ready-for-agent
Depends on: 06

## 目标

提供 `shiyi` 命令行客户端，覆盖启动服务、单次发送与交互式 REPL。

## 范围

- `shiyi serve`：加载配置并启动 HTTP 服务。
- `shiyi "文本"`：发送一次 `POST /messages`，按响应类型打印：
  - record：记录时间。
  - query：回答、状态、引用列表（时间 + 内容）。
  - unrecognized：无法识别的提示。
  - 错误：HTTP 状态、错误码、错误信息。
- `shiyi`：交互式 REPL，逐行读取；空行忽略；`/quit` 退出；每行一次请求。
- `--server`，默认 `http://127.0.0.1:8787`。
- 不做本地历史、不做缓存、不重试。
- 输出为纯文本，便于脚本使用；错误写 stderr，退出码非零。

## 验收

- 能对本地服务完成一次记录、一次查询、一次无法识别。
- 服务返回 503 时打印明确错误并以非零退出。
- REPL 中每条输入独立发送，不携带上下文。
- `go test ./cmd/...` 覆盖参数解析与响应格式化。

## 参考

- [design.md](../design.md)。
