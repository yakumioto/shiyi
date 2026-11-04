# 05 - 多轮检索与回答循环

Status: ready-for-agent
Depends on: 03, 04

## 目标

实现查询回答：让模型通过 `search_memory` 多轮检索，最终返回受引用校验的回答。

## 范围

- `internal/answer` 包，暴露 `Answer(ctx, query string) (Result, error)`。
- `Result`：`Answer`、`Citations []Citation`、`Status`（`ok` / `budget_exhausted` / `no_match`）。
- 工具定义与 JSON schema：`search_memory(keywords, limit)`。
- 循环：
  1. 发送系统提示、用户查询、工具定义。
  2. 执行模型返回的工具调用，累计证据；每次调用计入预算。
  3. 把工具结果以 `tool` 消息追加。
  4. 模型给出最终内容时，解析 `{"answer","citations"}`。
- 预算：最多 3 次搜索、20 条不同记录、回答阶段 30 秒；超限后做一次不带工具的「立即回答」调用，状态 `budget_exhausted`。
- 无证据时直接返回 `no_match`，固定回答「没有找到相关记忆」，引用为空，丢弃模型文本。
- 引用校验：每个 `time` 必须在本次查询返回过的集合中；否则返回 `ErrInvalidCitation`。
- 模型输出非法 JSON、缺字段、引用格式错误 → `ErrInvalidResponse`。
- 测试使用假 LLM 与假检索器，覆盖：一次命中、多轮、预算耗尽、无结果、非法引用、非法 JSON、工具参数错误。

## 验收

- 模型只能看到 `search_memory` 返回的记录。
- 引用无法指向未检索到的记录。
- `no_match` 不会出现模型自由发挥的内容。
- `budget_exhausted` 仍返回合法引用。
- `go test ./internal/answer -race` 通过。

## 参考

- [design.md](../design.md)。
- [ADR 0004](../../../docs/adr/0004-llm-classification-and-mixed-as-query.md)。
