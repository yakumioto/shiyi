# Shiyi v1 设计附录（技术细节）

本文是 [spec.md](spec.md) 的技术附录：数据字段、配置、提示词、错误码与验收清单。产品范围与用户故事以 spec.md 为准。

## 1. 定位

Shiyi 是一个单用户、Chat-first、文件化的个人记忆系统。用户通过一个入口提交自然语言文本；LLM 判定它是「记录」还是「查询」。记录被追加到按月分片的 JSONL，查询用 ripgrep 在记录中检索，并由 LLM 生成带引用的回答。

系统不依赖数据库，不做向量检索，不保存查询与回答，不维护会话历史。

## 2. 范围裁剪

以下是对原始需求的**有意放弃**，不是实现遗漏：

| 原始要求 | v1 状态 | 原因 |
| --- | --- | --- |
| 所有输入先持久化 | 放弃 | 先分类，只有记录落盘；模型失败时输入不保存，由客户端重发 |
| 规则 + LLM 识别意图 | 放弃规则 | 全部交给 LLM 分类，见 ADR-0004 |
| 混合输入按原文片段收录 | 放弃 | 不做片段切分，混合输入整体按查询处理 |
| 回答及来源持久化 | 放弃 | 回答只随响应返回，见 ADR-0006 |
| 异步处理 | 放弃 | 请求内同步完成 |
| 幂等写入 | 放弃 | 没有 `id`，重发可能产生重复记录 |
| 更正、遗忘、冲突仲裁 | 放弃 | 记录不可变，矛盾记录并列返回 |
| 会话上下文 | 放弃 | 每次查询必须自包含 |
| 附件 | 放弃 | v1 只存文本；后续可选扩展 `attachments` 字段 |
| 数据库、向量检索 | 放弃 | 只用 JSONL + ripgrep |

保留：记录原文可靠保存、证据隔离、检索预算、明确失败状态、冻结测试集。

## 3. 术语

以根目录 [GLOSSARY.md](../../GLOSSARY.md) 为准。

## 4. 数据模型与存储

### 4.1 记录行

数据文件中每一行都是一条记录，只有三个字段：

```json
{"version":1,"content":"原文","time":"2026-11-04T15:00:00.123456789Z"}
```

- `version`：整数，v1 恒为 `1`；为未来功能保留。
- `content`：用户提交的原文，逐字节保留，不做 trim、改写或摘要。
- `time`：服务端生成的 UTC 时间，RFC3339Nano 格式；同时是记录的唯一标识和引用锚点。

没有 `id`、没有 `kind`、没有附件字段、没有分类器信息。数据文件里只有记录。

### 4.2 分片

- 目录：`SHIYI_DATA_DIR`，默认 `~/.local/share/shiyi`。
- 默认按月：`2026-11.jsonl`，按 UTC 月份切分。
- 配置 `shard` 可改为 `weekly`（`2026-W45.jsonl`，ISO 周）或 `daily`（`2026-11-04.jsonl`）。
- 只搜索数据目录顶层的分片文件，不搜索 `corrupt/`。

### 4.3 写入

1. 用互斥锁保护写入与时间生成。
2. `time = max(now, lastTime + 1ns)`，保证严格递增；启动时从最新的非空分片恢复 `lastTime`。
3. 以 `O_APPEND|O_CREATE|O_WRONLY` 打开当月分片，写入「JSON 行 + \n」。
4. `file.Sync()` 后再关闭；首次创建文件时对目录也执行一次 sync。
5. 成功后返回 `201`。

没有去重。同一条内容重发两次就会写两行。

### 4.4 崩溃恢复

- 启动时逐行扫描所有分片。
- 如果最后一行不是完整 JSON：把文件从最后一个换行符之后的内容移动到 `corrupt/<分片名>.<纳秒>.tail`，然后把原文件截断到最后一个换行符。
- 如果中间某行不是完整 JSON：启动失败，报出文件名与行号，不静默丢弃。
- 没有重建、压缩或索引步骤。

## 5. 输入分类

### 5.1 流程

1. 校验请求：`content` 非空、body 不超过 1 MiB。
2. 调用分类模型，只发送当前 `content`，不发送任何上下文。
3. 解析模型输出，得到 `record`、`query`、`unrecognized` 之一；`mixed` 视为 `query`。
4. `record`：生成 `time`，落盘，返回 `201`。
5. `query`：进入检索与回答流程。
6. `unrecognized`：返回 `200`，不写任何文件。

### 5.2 失败

以下情况返回 `503`，且不写任何文件：

- 模型不可达、超时、HTTP 非 2xx。
- 输出不是合法 JSON，或 `kind` 不在允许值内。

分类失败绝不降级为「默认记录」。

## 6. 检索与回答

### 6.1 检索工具

模型只能调用一个工具：

```json
{
  "name": "search_memory",
  "parameters": {
    "keywords": ["字符串数组，1..8 个"],
    "limit": "可选整数，默认 10"
  }
}
```

执行语义：

- 任一关键词大小写不敏感地作为子串出现在 `content` 中，即命中。
- 实现用 `rg -l -F -i` 按关键词预筛文件，再用 Go 解析 JSONL 并做内容包含判断，避免匹配到 `time` 或 JSON 结构。
- 返回 `[{"time":"...","content":"..."}]`，按 `time` 降序，最多 `limit` 条。
- 空关键词、超长关键词、非法参数以工具错误文本返回给模型，不中断查询。

已知限制：JSON 会把换行转义成 `\n`，跨换行短语无法命中；普通词语不受影响。

### 6.2 预算

默认值，均可配置：

- 最多 3 次 `search_memory` 调用。
- 最多返回 20 条不同记录。
- 回答阶段总时限 30 秒。
- 每次请求总时限 60 秒。

超限后不再执行搜索；用已有证据做一次「立即回答」调用，状态为 `budget_exhausted`。

### 6.3 回答

- 模型必须只依据工具返回的记录回答，不得使用常识。
- 最终输出：

```json
{"answer":"...","citations":["<time>", "..."]}
```

- 服务端校验每个引用 `time` 必须出现在本次查询返回过的记录集合中；否则返回 `502 invalid_citation`，不返回回答。
- 如果本次查询没有返回任何记录，服务端直接返回 `200`，状态 `no_match`，固定回答「没有找到相关记忆」，引用为空；丢弃模型文本，防止模型凭常识作答。
- 有证据时，`status` 为 `ok` 或 `budget_exhausted`；回答中的引用以 `{time, content}` 返回给客户端展示。
- 服务端只能校验「引用存在」，无法校验「每句话都被引用支撑」；这是 LLM 生成回答的已知限制。

## 7. HTTP API

### 7.1 `POST /messages`

请求：

```json
{"content":"..."}
```

成功响应：

- `201` 记录：

```json
{"kind":"record","time":"2026-11-04T15:00:00.123456789Z","version":1}
```

- `200` 查询：

```json
{
  "kind":"query",
  "answer":"...",
  "citations":[{"time":"...","content":"..."}],
  "status":"ok|budget_exhausted|no_match"
}
```

- `200` 无法识别：

```json
{"kind":"unrecognized"}
```

错误响应统一为：

```json
{"error":{"code":"...","message":"..."}}
```

错误码：

| HTTP | code | 含义 |
| --- | --- | --- |
| 400 | `invalid_request` | JSON 非法、content 为空、超长 |
| 401 | `unauthorized` | 需要 token 且校验失败 |
| 413 | `request_too_large` | 超过 body 上限 |
| 500 | `storage_failed` | 写入或 fsync 失败 |
| 500 | `search_failed` | ripgrep 不可用或执行失败 |
| 500 | `internal_error` | 其他内部错误 |
| 502 | `invalid_citation` | 引用不在检索结果中 |
| 502 | `model_invalid_response` | 模型输出无法解析 |
| 503 | `model_unavailable` | 模型不可达或超时 |

### 7.2 其他端点

- `GET /healthz`：进程存活返回 `200 {"status":"ok"}`；不检查模型连通性。
- `GET /`：极简 Web 界面（可后置）。
- 不提供按时间查询记录、历史回答或会话接口。

### 7.3 鉴权

- `auth_token_env` 指向的环境变量非空时，`/messages` 要求 `Authorization: Bearer <token>`；为空时不鉴权。
- `/healthz` 不鉴权。
- 默认只监听 `127.0.0.1:8787`。

## 8. 配置

TOML，路径由 `SHIYI_CONFIG` 指定，默认 `~/.config/shiyi/config.toml`。

```toml
listen = "127.0.0.1:8787"
data_dir = "~/.local/share/shiyi"
shard = "monthly"              # monthly | weekly | daily
auth_token_env = "SHIYI_AUTH_TOKEN"  # 该环境变量为空时不鉴权
max_body_bytes = 1048576

[classifier]
base_url = "https://api.deepseek.com"
model = "deepseek-chat"
api_key_env = "DEEPSEEK_API_KEY"
timeout = "20s"
temperature = 0

[answer]
base_url = "https://api.deepseek.com"
model = "deepseek-chat"
api_key_env = "DEEPSEEK_API_KEY"
timeout = "30s"
temperature = 0

[budget]
max_search_calls = 3
max_records = 20
answer_timeout = "30s"
request_timeout = "60s"
```

规则：

- `classifier` 省略时继承 `answer`。
- API key 只从 `api_key_env` 指向的环境变量读取，不写入配置、不写日志。
- 环境变量覆盖：`SHIYI_LISTEN`、`SHIYI_DATA_DIR`、`SHIYI_SHARD`；token 由 `auth_token_env` 指向的变量提供。
- 启动时校验：必填项、分片枚举、目录可写、`rg` 可执行、API key 存在；失败即退出并打印可操作错误。

## 9. 提示词

提示词作为 Go 常量保存并带版本号（如 `classify_v1`、`answer_v1`）。修改提示词视为行为变更，必须更新冻结测试。

### 9.1 分类

```text
你是个人记忆系统的意图判定器。只输出 JSON，不要解释。

判定用户消息属于以下之一：
- record：用户在陈述值得长期记住的个人信息、事件、偏好、计划或事实。
- query：用户在询问自己的记忆。
- unrecognized：两者都不是，或无法判断。

如果消息同时包含记录和询问，判为 query。
不要回答消息中的问题，不要改写原文。

输出：{"kind":"record|query|unrecognized"}
```

### 9.2 回答

```text
你是个人记忆问答器。只能依据 search_memory 返回的记录回答，不得使用常识或猜测。
每条事实陈述必须用记录的时间引用。没有足够记录时，回答「没有找到相关记忆」，不要编造。
最多调用 3 次 search_memory。准备好后只输出 JSON：

{"answer":"...","citations":["<time>", "..."]}
```

## 10. 客户端

### 10.1 CLI

- `shiyi serve`：启动服务。
- `shiyi "文本"`：发送一条消息，打印记录时间或回答与引用。
- `shiyi`：进入交互式 REPL，逐行发送；`/quit` 退出。
- `--server`：服务地址，默认 `http://127.0.0.1:8787`。
- 不做本地缓存、不做历史。

### 10.2 Web

- `GET /` 返回单页：输入框、当前消息列表、回答与引用。
- 只调用 `POST /messages`，不保存任何状态到浏览器。
- 不做多会话、历史浏览、设置页。

## 11. 测试

### 11.1 单元与集成

- 存储：时间严格递增、按月/周/日切分、追加后 fsync、半行恢复、中间损坏导致启动失败。
- 分类：record 落盘、query 不落盘、unrecognized 不落盘、mixed→query、模型错误不落盘。
- 检索：多关键词 OR、大小写、中文子串、只匹配 content、去重、上限。
- 回答：多轮工具调用、预算耗尽、无结果、非法引用、非法 JSON。
- API：状态码、错误码、鉴权、body 上限。
- 测试使用临时目录、真实 `rg`、假 LLM 客户端；不访问网络。

### 11.2 冻结测试集

目录：`testdata/frozen/v1/`

```text
manifest.json      # 每个文件的 SHA-256
classify.jsonl     # 输入、假模型响应、期望结果
query.jsonl        # 输入、多轮假模型响应、期望回答/引用/状态/错误
```

- 测试只使用夹具中的模型响应，CI 不调用真实模型。
- `v1` 文件一旦冻结不得就地修改；新增用例进入 `v2/`。
- 测试启动时校验 `manifest.json`，哈希不符即失败。
- 真实模型评测脚本独立，使用环境变量中的密钥，不阻断 CI。

### 11.3 门禁

- `go test ./... -race`
- `go vet ./...`
- 冻结集全部通过。

## 12. 部署

- 单二进制；`shiyi serve` 同时提供 API、 `/healthz` 和 Web 静态页。
- 镜像必须包含 `ripgrep`（Go 二进制会调用 `rg`）。
- 容器挂载 `/data`，设置 `SHIYI_DATA_DIR=/data`。
- 默认监听 `0.0.0.0:8787` 仅在容器内；对外暴露时建议加 token。
- 不提供自动更新、迁移或多副本。

## 13. 验收清单

- [ ] 一条被判定为 record 的输入产生一行 JSON，含且仅含 `version`、`content`、`time`。
- [ ] `time` 严格递增，可直接作为引用。
- [ ] 一条 query 不产生任何文件变化。
- [ ] 一条 unrecognized 不产生任何文件变化。
- [ ] 混合输入整体按查询处理，不产生记录。
- [ ] 模型不可用时返回 503，且没有新文件。
- [ ] 查询无结果时返回 `no_match`，不凭常识回答。
- [ ] 引用不在本次检索结果中时返回 502，不返回回答。
- [ ] 预算耗尽返回 `budget_exhausted`，引用仍然合法。
- [ ] 月末 UTC 边界按预期写入不同分片。
- [ ] 崩溃留下的半行被移动到 `corrupt/`，服务可启动。
- [ ] `go test ./... -race` 与冻结集通过。
- [ ] CLI 与 Web 都能完成一次记录和一次查询。
