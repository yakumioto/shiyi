# 03 - OpenAI-compatible LLM 客户端

Status: ready-for-agent
Depends on: 01

## 目标

实现可配置的 OpenAI-compatible Chat Completions 客户端，支持分类调用、带工具的答案调用与错误分类。

## 范围

- `internal/llm` 包，定义 `Client` 接口、消息/工具/响应类型，便于测试注入假客户端。
- HTTP 实现：
  - `POST {base_url}/chat/completions`
  - `Authorization: Bearer <api_key>`
  - `model`、`messages`、`temperature`、`tools`、`tool_choice`
  - 分类调用使用 JSON response format（供应商支持时）。
  - 解析 `choices[0].message.content` 与 `tool_calls`。
- 错误映射：
  - 网络错误、超时、非 2xx、5xx → `ErrUnavailable`
  - 2xx 但响应结构非法 → `ErrInvalidResponse`
  - 4xx 鉴权错误 → `ErrUnavailable`，并在日志中给出可操作提示
- 超时来自配置；context 取消可中断。
- 额外：一个脚本化的假客户端，供上层测试使用。
- 单测使用 `httptest.Server`，校验请求头、路径、JSON 字段、错误映射。

## 验收

- 能正确构造分类请求与工具请求。
- 能解析工具调用与普通文本。
- 超时、非 2xx、坏 JSON 分别映射到正确错误类型。
- 密钥不出现在日志与错误信息中。
- `go test ./internal/llm -race` 通过。

## 参考

- [design.md](../design.md)。
- [ADR 0005](../../../docs/adr/0005-service-availability-follows-model.md)。
