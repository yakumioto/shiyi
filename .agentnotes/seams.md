# 冻结接缝契约（Seam Contract）

本文件由 Lead 维护。**实施子代理一律按此契约编码，不要自行改签名。**
如果确实需要改，先在报告里说明理由，由 Lead 统一改。

已冻结的接口位于 `internal/api`、`internal/store/record.go`、`internal/store/store.go`、
`internal/llm/llm.go`、`internal/classify/classify.go`、`internal/search/search.go`、
`internal/answer/answer.go`、`internal/config/config.go`。

## 分工与所有权

| 包 | 归谁写 | 说明 |
| --- | --- | --- |
| `internal/api` | Lead（只读引用） | HTTP 线格式；**任何人都不改** |
| `internal/store` | 01a | 存储实现 |
| `internal/llm` | 01b | 模型客户端实现 |
| `internal/config` | 01a | 配置加载与校验 |
| `internal/classify` | 01a | 分类器实现（提示词 `classify_v1`） |
| `internal/search` | 02a | 检索实现 |
| `internal/answer` | 02a | 回答编排实现 |
| `internal/httpapi` | 02b | HTTP 处理器实现 |
| `cmd/shiyi` | 03 | CLI |
| `internal/web` | 04 | Web 单页 |

## 关键签名

```go
// internal/store
type Store interface {
    Append(ctx context.Context, content string) (Record, error)
    Records(ctx context.Context) ([]Record, error)
}
type Record struct {
    Version int    `json:"version"`
    Content string `json:"content"`
    Time    Time   `json:"time"`
}
type Time struct{ time.Time }  // 零值 JSON 用法
```

`store.Record` 与 `api.RecordResponse` 是**不同**的东西：
存储层记录带原文，回答层引用只带 `time` + `content` 原文。

```go
// internal/llm
type Client interface {
    Complete(ctx context.Context, req Request) (*Response, error)
}
var ErrUnavailable, ErrInvalidResponse error
```

`ErrUnavailable` → 503 `model_unavailable`；`ErrInvalidResponse` → 502 `model_invalid_response`。

```go
// internal/classify
type Judge interface {
    Classify(ctx context.Context, content string) (Kind, error)
}
```

`mixed` 必须映射为 `KindQuery`。

```go
// internal/search
type Searcher interface {
    Search(ctx context.Context, keywords []string, limit int) ([]store.Record, error)
}
```

```go
// internal/answer
type Answerer interface {
    Answer(ctx context.Context, query string) (Result, error)
}
var ErrInvalidCitation error   // → 502 invalid_citation
const NoMatchAnswer = "没有找到相关记忆"
```

## 错误映射表（唯一权威）

| 底层错误 | HTTP | code |
| --- | --- | --- |
| JSON 非法 / content 为空 | 400 | `invalid_request` |
| body 超过 `max_body_bytes` | 413 | `request_too_large` |
| Bearer token 缺失或错误 | 401 | `unauthorized` |
| `store.ErrStorageFailed` | 500 | `storage_failed` |
| 检索失败（rg 不可用/执行失败） | 500 | `search_failed` |
| `llm.ErrUnavailable` | 503 | `model_unavailable` |
| `llm.ErrInvalidResponse` | 502 | `model_invalid_response` |
| `answer.ErrInvalidCitation` | 502 | `invalid_citation` |
| 其它 | 500 | `internal_error` |

错误消息**不得**包含原始模型错误体、密钥或完整原文。

## 提示词版本

- 分类：`internal/classify` 中的 `classify_v1` 常量。
- 回答：`internal/answer` 中的 `answer_v1` 常量。

提示词改动视为行为变更，必须同步更新冻结测试集（票 07）。

## HTTP 响应体（照抄 `internal/api`）

```json
// 201
{"kind":"record","time":"2026-11-04T15:00:00.123456789Z","version":1}
// 200 查询
{"kind":"query","answer":"...","citations":[{"time":"...","content":"..."}],"status":"ok|budget_exhausted|no_match"}
// 200 无法识别
{"kind":"unrecognized"}
// 错误
{"error":{"code":"...","message":"..."}}
```
