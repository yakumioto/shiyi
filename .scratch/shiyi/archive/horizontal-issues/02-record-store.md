# 02 - 记录存储与崩溃恢复

Status: ready-for-agent
Depends on: 01

## 目标

实现只追加记录 JSONL 的存储层：时间生成、按月/周/日分片、fsync、半行恢复。

## 范围

- `internal/store` 包，暴露类似 `Open(cfg) (*Store, error)` 与 `Append(content string) (Record, error)`。
- `Record` 结构只含 `Version`、`Content`、`Time`，JSON 字段名与规格一致。
- 时间：UTC、`RFC3339Nano`、进程内严格递增；冲突时 `+1ns`。
- 启动时从最新非空分片恢复 `lastTime`。
- 分片：`monthly`、`weekly`（ISO 周）、`daily`，按 UTC。
- 写入：`O_APPEND`、写一行、`Sync`、关闭；首次创建文件时同步目录。
- 启动扫描所有分片：
  - 最后一行不完整：移动到 `corrupt/<分片>.<纳秒>.tail`，截断到最后一个换行符。
  - 中间行损坏：返回错误，启动失败，包含文件名与行号。
- 写入互斥；读取不受影响。
- 单测覆盖：单调时间、跨月/跨周/跨日边界、并发写入、半行恢复、中间损坏。

## 验收

- 连续写入 1000 条，`time` 严格递增且无重复。
- 跨 UTC 月边界的记录进入正确分片。
- 进程崩溃模拟（截断最后一行）后启动成功，坏尾进入 `corrupt/`，之前的记录仍可读。
- 中间行损坏时启动返回明确错误，不静默丢数据。
- `go test ./internal/store -race` 通过。

## 参考

- [design.md](../design.md)。
- [ADR 0001](../../../docs/adr/0001-jsonl-as-sole-source-of-truth.md)。
