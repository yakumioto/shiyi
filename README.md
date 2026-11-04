# Shiyi

**单用户、Chat-first 的本地个人记忆系统：说一句话就记住，之后用自然语言问出来，答案能指回你的原话。**

Shiyi 是一个本地 HTTP 服务。你把一段自然语言交给它，它先问 LLM 这是「要记住的东西」还是「在提问」：
只有被判定为**记录**的输入，才会以**原文**追加到按月分片的 JSONL 文件里；**查询**则触发一次字面检索，
由 LLM 生成带**引用**的回答。

- 数据就是普通 JSONL 文件，不用数据库，不用向量检索，你直接看、直接备份。
- 每条记录只有三个字段：`version`、`content`、`time`。原文逐字保留，一个字都不改。
- `time` 是记录的唯一标识，同时也是回答引用它的锚点。

---

## 目录

- [范围裁剪](#范围裁剪)
- [快速开始：容器](#快速开始容器)
- [快速开始：本地构建](#快速开始本地构建)
- [配置](#配置)
- [HTTP API](#http-api)
- [健康检查](#健康检查)
- [命令行用法](#命令行用法)
- [数据格式](#数据格式)
- [已知限制](#已知限制)
- [开发](#开发)

---

## 范围裁剪

下面这些是 **v1 有意放弃** 的能力，不是实现遗漏。文档不会把它们说成已实现：

| 能力 | v1 状态 | 说明 |
| --- | --- | --- |
| 保存查询与回答 | **不做** | 查询和回答只随响应返回，不落盘。重启后没有任何会话状态，也查不到「上次问过什么」。 |
| 幂等写入 | **不做** | 没有幂等键、没有去重。网络抖动后重发同一条内容，会产生两条独立记录。 |
| 会话上下文 | **不做** | 每次查询必须自包含，不支持多轮追问，模型看不到历史对话。 |
| 附件 | **不支持** | v1 只存文本。没有图片、PDF、音频、OCR、视觉模型。 |
| 向量检索 / embedding / 数据库 | **不做** | 检索只用 ripgrep 做字面匹配，没有语义召回，没有索引。 |

其它同样不在范围内的：多用户与账号体系、记录更正与删除、审计日志、异步任务与队列、
Markdown 渲染、流式输出、多设备同步、加密、自动更新、多副本。

保留的核心：记录原文可靠保存、证据隔离、检索预算、明确的失败状态、冻结测试集。

---

## 快速开始：容器

需要：Docker、一个大模型 API key（DeepSeek、OpenAI 或任何 OpenAI 兼容服务）。

### 1. 构建镜像

```bash
docker build -t shiyi:latest .
```

镜像里的 `rg`（ripgrep）是检索的硬依赖，已经装好。可以自己确认：

```bash
docker run --rm --entrypoint rg shiyi:latest --version
```

### 2. 运行

```bash
docker run -d \
  --name shiyi \
  -p 8787:8787 \
  -v shiyi-data:/data \
  -e SHIYI_API_KEY="$DEEPSEEK_API_KEY" \
  shiyi:latest
```

- `-v shiyi-data:/data`：数据卷。**记录全部写在这里**，换镜像、重启容器都不丢。
- `-e SHIYI_API_KEY=...`：模型密钥。**只经环境变量传入**，不写进配置文件，也不进日志。
- `-p 8787:8787`：把容器端口映射到本机。

### 3. 可选：给服务加 token

容器默认监听 `0.0.0.0:8787`。要在局域网上给别人用，请开启 Bearer 鉴权：

```bash
docker run -d \
  --name shiyi \
  -p 8787:8787 \
  -v shiyi-data:/data \
  -e SHIYI_API_KEY="$DEEPSEEK_API_KEY" \
  -e SHIYI_AUTH_TOKEN="$(openssl rand -hex 24)" \
  shiyi:latest
```

设了 `SHIYI_AUTH_TOKEN` 之后，`POST /messages` 必须带 `Authorization: Bearer <token>`，
否则返回 `401 unauthorized`。`GET /healthz` 永远不鉴权。

### 4. 记录一条、查一次

```bash
# 记录
curl -sS -X POST http://127.0.0.1:8787/messages \
  -H 'Content-Type: application/json' \
  -d '{"content":"我下周一开始新的作息，每天 7 点起床。"}'
# => {"kind":"record","time":"2026-11-04T09:12:33.481902417Z","version":1}

# 查询
curl -sS -X POST http://127.0.0.1:8787/messages \
  -H 'Content-Type: application/json' \
  -d '{"content":"我的作息是怎样的？"}'
```

带上 token 时加一行 `-H 'Authorization: Bearer <token>'`。

### 5. 确认数据确实落盘了

```bash
docker run --rm -v shiyi-data:/data alpine ls -l /data
# 2026-11.jsonl

docker run --rm -v shiyi-data:/data alpine cat /data/2026-11.jsonl
# {"version":1,"content":"我下周一开始新的作息，每天 7 点起床。","time":"2026-11-04T09:12:33.481902417Z"}
```

容器重建（`docker rm -f shiyi && docker run ...`）后记录仍在，因为数据在卷里，不在容器里。

### 容器里用了什么

| 项 | 值 |
| --- | --- |
| 基础镜像 | `alpine:3.22`（运行段）/ `golang:1.26-alpine`（构建段） |
| 运行用户 | `shiyi`（非 root，uid 1000） |
| 数据目录 | `/data`（数据卷，`SHIYI_DATA_DIR=/data`） |
| 监听地址 | `0.0.0.0:8787`（`SHIYI_LISTEN`） |
| 容器内配置 | `/etc/shiyi/config.toml`（`SHIYI_CONFIG`） |
| 健康检查 | `HEALTHCHECK` 调 `GET /healthz` |

镜像内置的配置以 `SHIYI_API_KEY` 作为 `api_key_env`。想换成别的变量名，
挂一份自己的配置进去即可（见 [配置](#配置)）。

---

## 快速开始：本地构建

需要：Go 1.26、系统里有 `rg`（ripgrep）、一个大模型 API key。

### 1. 构建

```bash
go build -o shiyi ./cmd/shiyi
# 或者
make build
```

### 2. 准备配置

```bash
mkdir -p ~/.config/shiyi
cp config.example.toml ~/.config/shiyi/config.toml
```

配置默认从 `~/.config/shiyi/config.toml` 读取。放在别处就用 `SHIYI_CONFIG` 指过去：

```bash
export SHIYI_CONFIG=/path/to/config.toml
```

### 3. 准备密钥

```bash
export DEEPSEEK_API_KEY=sk-...      # 对应配置里的 api_key_env
export SHIYI_AUTH_TOKEN=...          # 可选；不设置则不鉴权
```

密钥只从环境变量读。配置文件里写的是**变量名**，不是密钥本身。

### 4. 跑起来

```bash
./shiyi serve
# 或者
make run
```

服务监听 `127.0.0.1:8787`。开另一个终端验证：

```bash
curl http://127.0.0.1:8787/healthz
# {"status":"ok"}
```

然后用[命令行用法](#命令行用法)或 [HTTP API](#http-api)完成第一次记录和第一次查询。

---

## 配置

配置文件是 TOML，路径由 `SHIYI_CONFIG` 决定，默认 `~/.config/shiyi/config.toml`。
完整示例见 [`config.example.toml`](config.example.toml)，
容器内置的那份见 [`docs/config.docker.toml`](docs/config.docker.toml)。

### 顶层

| 键 | 含义 | 默认值 |
| --- | --- | --- |
| `listen` | 监听地址。本地默认只听本机；容器内必须改成 `0.0.0.0:8787` 才能被端口映射访问。 | `127.0.0.1:8787` |
| `data_dir` | 数据目录，JSONL 分片文件放这里。`~` 会展开成主目录。 | `~/.local/share/shiyi` |
| `shard` | 分片粒度：`monthly` / `weekly` / `daily`，边界按 UTC 算。 | `monthly` |
| `auth_token_env` | 鉴权 token 所在的环境变量名。该变量非空时 `POST /messages` 需要 Bearer token；为空或未设置则不鉴权。 | `SHIYI_AUTH_TOKEN` |
| `max_body_bytes` | 请求体上限（字节），超过返回 `413 request_too_large`。 | `1048576`（1 MiB） |

### `[classifier]` —— 意图分类模型

| 键 | 含义 | 默认值 |
| --- | --- | --- |
| `base_url` | OpenAI 兼容端点的根地址，例如 `https://api.deepseek.com`。 | — |
| `model` | 模型名，例如 `deepseek-chat`。 | — |
| `api_key_env` | 从这个环境变量读取 API key。 | — |
| `timeout` | 单次分类请求的超时。 | `20s` |
| `temperature` | 取 0 让分类结果稳定。 | `0` |

省略整个 `[classifier]` 段时，继承 `[answer]` 的设置。

### `[answer]` —— 回答模型

| 键 | 含义 | 默认值 |
| --- | --- | --- |
| `base_url` | OpenAI 兼容端点的根地址。 | — |
| `model` | 模型名。 | — |
| `api_key_env` | 从这个环境变量读取 API key。 | — |
| `timeout` | 单次回答请求的超时。 | `30s` |
| `temperature` | 取 0。 | `0` |

### `[budget]` —— 单次查询的检索预算

| 键 | 含义 | 默认值 |
| --- | --- | --- |
| `max_search_calls` | 最多调用多少次检索工具。 | `3` |
| `max_records` | 最多返回多少条不同的记忆条目。 | `20` |
| `answer_timeout` | 回答阶段的总时限。 | `30s` |
| `request_timeout` | 单次请求的总时限。 | `60s` |

### 环境变量覆盖

配置里的这三项可以直接用环境变量覆盖，环境变量优先：

| 环境变量 | 覆盖的配置项 |
| --- | --- |
| `SHIYI_CONFIG` | 配置文件路径本身 |
| `SHIYI_LISTEN` | `listen` |
| `SHIYI_DATA_DIR` | `data_dir` |
| `SHIYI_SHARD` | `shard` |

### 密钥只从环境变量读

这是硬规则：**配置文件里永远只写变量名，不写密钥**。

| 变量名 | 用途 |
| --- | --- |
| `DEEPSEEK_API_KEY` | `config.example.toml` 里 `api_key_env` 的默认值。 |
| `SHIYI_API_KEY` | 容器内置配置 `docs/config.docker.toml` 里 `api_key_env` 的值。 |
| `SHIYI_AUTH_TOKEN` | 可选的 Bearer token，用来保护 `POST /messages`。 |

换成别的提供商，只要改 `base_url`、`model` 和 `api_key_env` 三个键即可；
密钥依然只放在环境变量里。密钥不会被写进配置文件，也不会出现在日志中。

### 启动校验

进程启动时会检查：必填项是否齐全、分片枚举是否合法、数据目录是否可写、
`rg` 是否在 PATH 里、API key 对应的环境变量是否非空。
任何一项不满足都会直接退出并打印可操作的错误信息，而不是等到第一次请求才失败。

> **注意**：`timeout`、`answer_timeout`、`request_timeout` 是**字符串**（如 `"20s"`），
> 不是整数秒。配置文件里请照示例写成 `"20s"`，不要写成 `20`。

---

## HTTP API

只有一个业务端点。请求体固定是 `{"content":"..."}`。

### `POST /messages`

```bash
curl -sS -X POST http://127.0.0.1:8787/messages \
  -H 'Content-Type: application/json' \
  -d '{"content":"我下周一开始新的作息，每天 7 点起床。"}'
```

#### 判定为记录 → `201`

```json
{"kind":"record","time":"2026-11-04T09:12:33.481902417Z","version":1}
```

`time` 就是这条记录的标识，可以直接拿去引用。

#### 判定为查询 → `200`

```json
{
  "kind": "query",
  "answer": "你下周一开始新的作息，每天 7 点起床。",
  "citations": [
    {
      "time": "2026-11-04T09:12:33.481902417Z",
      "content": "我下周一开始新的作息，每天 7 点起床。"
    }
  ],
  "status": "ok"
}
```

`status` 有三种取值：

| status | 含义 |
| --- | --- |
| `ok` | 正常回答。 |
| `budget_exhausted` | 检索预算用完了，用已有证据做了「立即回答」。回答可能不完整，引用仍然合法。 |
| `no_match` | 本次检索没有任何命中。固定回答「没有找到相关记忆」，`citations` 为空。 |

检索不到时：

```json
{"kind":"query","answer":"没有找到相关记忆","citations":[],"status":"no_match"}
```

#### 无法识别 → `200`

```json
{"kind":"unrecognized"}
```

这条输入没有被保存，也没有获得证据资格。

#### 出错

所有非 2xx 响应的形状统一：

```json
{"error":{"code":"invalid_request","message":"content 不能为空"}}
```

| HTTP | code | 含义 |
| --- | --- | --- |
| 400 | `invalid_request` | JSON 非法，或 `content` 为空。 |
| 401 | `unauthorized` | 需要 token 但缺失或不匹配。 |
| 413 | `request_too_large` | 请求体超过 `max_body_bytes`。 |
| 500 | `storage_failed` | 写入或 fsync 失败。 |
| 500 | `search_failed` | ripgrep 不可用或执行失败。 |
| 500 | `internal_error` | 其它内部错误。 |
| 502 | `invalid_citation` | 回答引用了本次检索没有返回过的记录。 |
| 502 | `model_invalid_response` | 模型输出无法解析。 |
| 503 | `model_unavailable` | 模型不可达、超时或返回非 2xx。 |

错误消息里不会出现原始模型响应体、密钥或完整原文。

### `GET /healthz`

进程存活就返回 `200`，**不**检查模型连通性。

```bash
curl http://127.0.0.1:8787/healthz
# {"status":"ok"}
```

### `GET /`

一个极简单页：输入框、当前消息列表、回答与引用。只调用 `POST /messages`，
不往浏览器里存任何状态。

---

## 命令行用法

CLI 是 HTTP 服务的客户端，默认连 `http://127.0.0.1:8787`。

```bash
# 一次性发送一条，然后退出。适合接脚本、快捷键。
./shiyi "我下周一开始新的作息，每天 7 点起床。"

# 连到别的服务地址
./shiyi --server http://192.168.1.10:8787 "我的作息是怎样的？"

# 交互式 REPL：逐行发送，/quit 退出
./shiyi
> 我下周一开始新的作息，每天 7 点起床。
> /quit

# 启动服务
./shiyi serve
```

一次性模式会打印记录时间，或者打印回答和它引用的记录。REPL 里同样是这两样输出。

服务开了 Bearer 鉴权时，CLI 会自动从 `SHIYI_AUTH_TOKEN` 读 token 并带上
`Authorization: Bearer <token>` 头，不用额外操作。

浏览器里的极简页面**不支持** Bearer 鉴权：它用同源 `fetch` 调 `POST /messages`，
不附加 `Authorization` 头，也不把 token 存到浏览器里。所以启用 `auth_token_env`
之后，Web 页面会收到 `401 unauthorized`，此时请改用 CLI。这是有意的取舍——
spec 明确不做登录页、账号体系和浏览器端持久化。

CLI **不做**本地缓存、不做历史记录、不做多轮上下文。

---

## 数据格式

数据目录里是一堆 JSONL 文件，一行一条记忆条目，每行**恰好三个字段**：

```json
{"version":1,"content":"我下周一开始新的作息，每天 7 点起床。","time":"2026-11-04T09:12:33.481902417Z"}
```

| 字段 | 含义 |
| --- | --- |
| `version` | 记录格式版本，v1 恒为 `1`。 |
| `content` | 你提交的原文，**逐字节保留**，不做 trim、改写或摘要。 |
| `time` | 服务端生成的 UTC 时间，RFC3339Nano 格式（固定九位小数）。 |

`time` 严格递增且唯一，既是这条记录的**唯一标识**，也是回答**引用**它的锚点。
没有 `id`、没有 `kind`、没有附件字段。

### 分片

按 `shard` 配置切分文件，边界一律按 **UTC** 计算：

| `shard` | 文件名 | 例 |
| --- | --- | --- |
| `monthly` | `YYYY-MM.jsonl` | `2026-11.jsonl` |
| `weekly` | `YYYY-Www.jsonl`（ISO 周） | `2026-W45.jsonl` |
| `daily` | `YYYY-MM-DD.jsonl` | `2026-11-04.jsonl` |

### 崩溃恢复

启动时会扫描所有分片：

- 最后一行不是完整 JSON —— 把它挪到 `corrupt/<分片名>.<纳秒>.tail`，再把原文件截断到最后一个换行符。
- 中间某行损坏 —— **启动失败**，报出文件名和行号，不静默丢数据。

没有重建、压缩或索引步骤。备份就是拷贝数据目录。

---

## 已知限制

请认真读这一节 —— 下面每一条都是当前版本的**真实边界**，不是待办事项。

1. **无回答持久化**。查询和回答不落盘：没有查询历史、没有回答档案、没有审计日志。重启服务后没有任何会话状态。
2. **无幂等**。没有幂等键也没有去重。同一条内容发两次就写两行。网络抖动导致响应丢失时重发，可能产生重复记录。
3. **无会话上下文**。每次查询必须自包含，模型看不到之前的对话，不支持多轮追问。
4. **不支持附件**。v1 只存文本。图片、PDF、音频、OCR、视觉模型都不在范围内。
5. **跨换行短语检索不到**。JSONL 会把换行转义成 `\n`，所以「先写这一句」和「再写这一句」这种跨行的短语无法命中。普通词语不受影响。
6. **记录不可更正、不可删除**。记录是只追加的，没有编辑、没有删除、没有墓碑。写错了就再记一条，矛盾的内容会并列返回，系统不替你仲裁哪个是真的。
7. **数据是明文 JSONL，没有加密**。谁能读到数据目录，谁就能读到你的全部记录。放在受控的机器上，别放在会同步到公共云盘的目录里。
8. **模型不可用即服务不可用**。分类和回答都依赖 LLM，模型不可达、超时或返回非法结果时，服务返回 `503 model_unavailable`，**不会**降级成规则分类或假装回答。这次输入也不会被保存，你需要自己保留原文重发。
9. **引用校验只能保证「引用存在」**。服务端会检查每个引用的 `time` 确实来自本次检索到的记录，但不保证回答里的**每一句话**都被引用支撑。模型仍可能说过头，请自己核对引用里的原文。
10. **不做向量检索**。检索是字面匹配：关键词大小写不敏感地作为子串出现在原文里才算命中。换一种说法可能就查不到。
11. **启用鉴权时 Web 页面不可用**。页面用同源 `fetch`，不附加 `Authorization` 头，也不把 token 存进浏览器。开了 `auth_token_env` 后请用 CLI。

---

## 开发

### 依赖

- Go 1.26
- 系统里有 `rg`（ripgrep）。检索靠它预筛文件，**没有它服务无法启动**。
  装不上就用容器：`docker run --rm --entrypoint rg shiyi:latest --version`。
- 依赖已经 vendor 进仓库，构建和测试**不需要网络**，也不需要 API key。

### Makefile

```bash
make build         # 编译出 ./shiyi
make test          # go test ./... -race
make vet           # go vet ./...
make test-frozen   # 只跑冻结测试集
make fmt           # go fmt ./...
make run           # 编译并启动服务
make clean         # 删掉 ./shiyi
```

### 门禁

```bash
go build ./...
go vet ./...
go test ./... -race
```

冻结测试集（`testdata/frozen/`）是 `go test ./... -race` 的一部分，也可以单独跑：

```bash
go test ./... -race -run Frozen
```

### 端到端冒烟

用假模型 + 真实二进制 + 真实 `rg` + 临时数据目录，跑一遍记录、无法识别、
带引用的查询、错误路径和 Web 页面。不需要网络，也不需要 API key：

```bash
bash scripts/smoke.sh
```

### 文档一致性检查

`README.md`、`Dockerfile` 和配置文件里的路径、环境变量名、CLI 子命令、错误码
很容易互相漂移。有一个脚本专门交叉检查它们：

```bash
bash docs/check-readme.sh
```

它不依赖网络，也不依赖 Go 工具链；任何一处漂移都会打印出 `[FAIL]` 行并以非 0 退出。
改了上面三个文件中的任何一个之后，记得跑一遍。

### 部署相关的进一步说明

容器部署、反向代理、systemd、备份等展开在 [`docs/deployment.md`](docs/deployment.md)。

---

## 许可

见仓库。
