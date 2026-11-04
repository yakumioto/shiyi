# 04 - ripgrep 检索

Status: ready-for-agent
Depends on: 01, 02

## 目标

实现 `search_memory` 的底层检索：在月份分片中做字面子串匹配，返回记录并按时间排序。

## 范围

- `internal/search` 包，暴露 `Search(ctx, keywords []string, limit int) ([]store.Record, error)`。
- 参数校验：关键词 1..8 个，去空白、去重、单个最长 128 字符；`limit` 默认 10，最大 20。
- 文件枚举：数据目录顶层 `*.jsonl`，排除 `corrupt/`。
- 预筛：每个关键词执行 `rg -l -F -i --no-messages --max-depth 1 -g '*.jsonl' -e <keyword> <data_dir>`。
  - 退出码 1（无匹配）不是错误；退出码 2 是错误。
- 精确判定：读取候选文件，逐行解析 JSON，仅当 `content` 大小写不敏感包含任一关键词时命中。
- 去重：按 `time` 去重；按 `time` 降序；截断到 `limit`。
- 错误：`rg` 不存在、执行失败、候选文件含非法 JSON 时返回错误，由上层映射为 `search_failed`。
- 测试使用临时目录与真实 `rg`（测试前 `exec.LookPath("rg")`，缺失时给出明确失败信息）。

## 验收

- 多关键词按 OR 命中。
- 英文大小写不敏感；中文子串命中。
- 不会因为 `time` 或 JSON 字段名而误命中。
- 返回顺序为时间降序，无重复。
- 无匹配返回空切片，不报错。
- `go test ./internal/search -race` 通过。

## 参考

- [design.md](../design.md)。
