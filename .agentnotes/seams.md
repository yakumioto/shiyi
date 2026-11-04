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

## 构造器签名（冻结，下游票按此依赖）

```go
// internal/config
func Load() (Config, error)                 // 读 SHIYI_CONFIG 或默认路径；套用环境变量覆盖与默认值
func LoadFile(path string) (Config, error)
func (c Config) Validate() error             // 必填项、shard 枚举、目录可写、rg 可执行、api_key_env 存在
func (c Config) APIKey() (string, error)     // 从 Answer.APIKeyEnv 指向的环境变量读
func (c Config) AuthToken() string           // 从 AuthTokenEnv 指向的环境变量读；未设置返回 ""

// internal/store
// 分片粒度用字符串传入，避免 internal/store 依赖 internal/config。
// 取值必须是 "monthly" / "weekly" / "daily"。
func New(dataDir string, shard string) (*Store, error)

// internal/classify
func New(client llm.Client, model string, temperature float64) Judge

// internal/llm
type Options struct {
    BaseURL     string
    Model       string
    APIKey      string
    Timeout     time.Duration
    Temperature float64
    HTTPClient  *http.Client   // 可为 nil
    Logger      *slog.Logger   // 可为 nil
}
func New(opts Options) *Client
func NewFakeClient(turns ...*Response) *FakeClient
func (f *FakeClient) SetError(err error)
func (f *FakeClient) Requests() []Request
func TextResponse(content string) *Response
func ToolCallResponse(id, name, args string) *Response

// internal/search
func New(dataDir string) *Searcher

// internal/answer
type Budget struct {
    MaxSearchCalls int
    MaxRecords     int
    AnswerTimeout  time.Duration
}
func New(client llm.Client, searcher search.Searcher, model string, temperature float64, budget Budget) Answerer

// internal/httpapi
type Deps struct {
    Config   config.Config
    Judge    classify.Judge
    Store    store.Store
    Answerer answer.Answerer
    Logger   *slog.Logger
}
func New(d Deps) (*Server, error)
func (s *Server) Handler() http.Handler
func (s *Server) HandlerWithWeb(web http.Handler) http.Handler

// internal/web
func Handler() http.Handler
```

`Request` 上新增（对冻结接缝的增补，Lead 已批准）：
```go
type ResponseFormat struct{ Type string `json:"type"` }
// Request.ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
```

## 包依赖方向（禁止循环）

```
config  →  (无内部依赖)
llm     →  (无内部依赖)
api     →  (无内部依赖)
store   →  (无内部依赖)
classify → llm
search  →  store
answer  →  llm, search, store
httpapi →  api, config, classify, store, answer, llm
web     →  (无内部依赖)
cmd/shiyi → 全部
```

- `internal/search` **不** import `internal/config`（只收 `dataDir` 字符串）。
- `internal/store` **不** import `internal/config`。
- `internal/answer` **不** import `internal/httpapi`。
- `internal/web` **不** import `internal/httpapi`。

## 分片粒度字符串

`internal/store` 不依赖 `internal/config`，所以分片粒度以字符串传递。
合法值：`"monthly"`、`"weekly"`、`"daily"`。非法值 → `store.New` 返回错误。

## 已知坑：TOML 时长字段（票 01a 必须处理）

`github.com/pelletier/go-toml/v2` **不能**把 `"20s"` 直接解码到 `time.Duration` 字段：

```
toml: cannot decode TOML string into struct field ModelConfig.Timeout of type time.Duration
```

而 design.md §8 的配置契约写的是字符串：`timeout = "20s"`、`answer_timeout = "30s"`、
`request_timeout = "60s"`。整数会被解析成纳秒，语义错误。

**规定的修法**（保持 `config.Config` 的字段类型不变，不要改冻结签名的公开类型）：
在 `internal/config` 内部用一个私有 raw 结构体承接 TOML，时长字段声明为 `string`，
解析后再用 `time.ParseDuration` 转换成 `time.Duration` 赋给 `Config`。即
`Load` / `LoadFile` 的**实现细节**，不影响 `Config` 的公开字段类型。

已实测：`encoding.TextUnmarshaler` 路径与 `UnmarshalTOML` 路径都不如私有 raw 结构可靠，
请直接用 raw 结构方案。

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
