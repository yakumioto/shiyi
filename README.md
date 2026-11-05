# Shiyi

**单用户、Chat-first 的个人记忆系统：通过 HTTPS Web 登录，保存原文，用自然语言查询并核对引用。**

输入先由 LLM 判定用途：只有判定为**记录**的输入，才以**原文**追加到 JSONL；
**查询**触发字面检索并即时返回带**引用**的回答。每条记忆只有 `version`、`content`、`time` 三个字段，原文不改写。

登录是访问控制，不是数据加密。敏感原文可以保存，也可以发送到你配置的模型网关及其上游模型。
当前版本信任服务器及管理员，不承诺服务器完全失陷后保密，也不承诺模型提供方看不到原文。

## 目录

- [快速开始：容器](#快速开始容器)
- [快速开始：本地构建](#快速开始本地构建)
- [配置](#配置)
- [登录、恢复与迁移](#登录恢复与迁移)
- [HTTP API](#http-api)
- [服务管理命令](#服务管理命令)
- [数据格式与隐私](#数据格式与隐私)
- [已知限制与后续方向](#已知限制与后续方向)
- [开发与门禁](#开发与门禁)

## 快速开始：容器

需要 Docker、模型 API key，以及由你管理的域名、HTTPS 证书和反向代理。
以下是部署指引，**不表示你的实际部署已经配置或验证**。接入责任详见[部署说明](<docs/deployment.md>)。

```bash
docker build -t shiyi:latest .
docker run --rm --entrypoint rg shiyi:latest --version
# 只生成哈希；不需要模型配置，不联网。终端输入两次，不回显密码。
docker run --rm -it --network none shiyi:latest hash-password
```

关闭 shell tracing，不把密码放在参数、管道或命令历史里。将输出的**哈希那一行**复制到权限为 0600 的部署环境文件；
不要复制终端提示。哈希也应当保密，不把整个终端输出上传到日志或工单。

```bash
umask 077
# 新建文件，不覆盖你已有的部署凭据文件
( set -o noclobber; : > shiyi.env )
${EDITOR:-vi} shiyi.env
```

文件内容（把占位符换成实际值；环境文件不进行 shell 变量展开）：

```dotenv
SHIYI_API_KEY=实际模型密钥
SHIYI_AUTH_PASSWORD_HASH=刚刚生成的完整bcrypt编码
SHIYI_AUTH_PUBLIC_ORIGIN=https://shiyi.example.com
```

```bash
docker run -d --name shiyi --restart unless-stopped \
  -p 127.0.0.1:8787:8787 \
  -v shiyi-data:/data \
  -v shiyi-auth:/auth \
  --env-file ./shiyi.env \
  shiyi:latest
curl -fsS http://127.0.0.1:8787/healthz
# {"status":"ok"}
```

将自管的 HTTPS 反向代理指向本机 8787，再通过配置的 HTTPS 来源打开网页、输入密码登录、提交原文。
**不要直接用内部 HTTP 地址登录**：生产 Cookie 始终是 Secure。
只对可信反向代理开放后端端口；固定来源校验不代替防火墙，也不能阻止非浏览器客户端伪造请求头。

| 镜像约定 | 值 |
| --- | --- |
| 基础镜像 | 构建 `golang:1.26-alpine`；运行 `alpine:3.22` |
| 运行身份 | 非 root `shiyi`，uid/gid 1000 |
| 记忆卷 | `/data`，`SHIYI_DATA_DIR=/data` |
| 独立认证卷 | `/auth`，`SHIYI_AUTH_STATE_DIR=/auth`；目录 0700，文件 0600 |
| 容器内监听 | `SHIYI_LISTEN=0.0.0.0:8787` |
| 容器内配置 | `/etc/shiyi/config.toml`，由 `SHIYI_CONFIG` 指定 |
| 存活检查 | 公开 `GET /healthz`，不调用模型 |

正常模式缺少哈希或来源会拒绝启动。镜像不含可用密码/哈希，不自动进入匿名模式。
重建容器要保留两个命名卷：记忆卷保存记录，认证卷保存登录及撤销状态，二者不可混放。
绑定宿主目录时须由部署者预先设置 uid/gid 1000 和安全权限，不使用长期 root 或全员可写来绕过错误。

## 快速开始：本地构建

需要 Go 1.26、ripgrep（`rg`）和模型 API key。

```bash
make build
mkdir -p ~/.config/shiyi
cp config.example.toml ~/.config/shiyi/config.toml
chmod 600 ~/.config/shiyi/config.toml
# 可选：配置位于其他路径
export SHIYI_CONFIG="$HOME/.config/shiyi/config.toml"
# 不把明文登录密码放入命令；只捕获哈希 stdout，提示仍显示在终端
export SHIYI_AUTH_PASSWORD_HASH="$(./shiyi hash-password)"
export SHIYI_AUTH_PUBLIC_ORIGIN=https://shiyi.example.com
# 通过你的秘密管理方式提供 DEEPSEEK_API_KEY，再启动
./shiyi serve
```

本地默认只监听 `127.0.0.1:8787`；同样需要自管 HTTPS 代理。模型密钥只从 `api_key_env` 指定的环境变量读取。
启动不探测模型连通性；**模型故障不影响认证**和存活检查，但分类/回答会明确报错。

## 配置

完整配置见[配置示例](<config.example.toml>)；镜像默认配置见[容器配置](<docs/config.docker.toml>)。
默认配置文件路径为 `~/.config/shiyi/config.toml`，可用 `SHIYI_CONFIG` 覆盖。

### 顶层与模型

| 键 | 含义 / 默认 |
| --- | --- |
| `listen` | 本地默认 `127.0.0.1:8787` |
| `data_dir` | 记忆目录，默认 `~/.local/share/shiyi`，展开 `~` |
| `shard` | `monthly` / `weekly` / `daily`，默认 monthly，UTC 分界 |
| `max_body_bytes` | 记忆请求体上限，默认 1048576（1 MiB） |

`[classifier]` 是意图分类模型，省略整段时继承 `[answer]`。二者的键：

| 键 | 含义 / 默认 |
| --- | --- |
| `base_url` | OpenAI 兼容端点根地址，例如 `https://api.deepseek.com` |
| `model` | 模型名，例如 `deepseek-chat` |
| `api_key_env` | 模型密钥环境变量名，不是密钥本身 |
| `timeout` | 分类默认 `"20s"`，回答默认 `"30s"` |
| `temperature` | 默认 0 |

示例使用 `DEEPSEEK_API_KEY`，容器使用 `SHIYI_API_KEY`。更换提供商时修改 `base_url`、`model`、`api_key_env`。
模型密钥不写入配置文件；登录密码哈希可以放在受保护配置或环境变量中。密码、哈希与模型密钥不得复用。

`[budget]` 的默认值：`max_search_calls = 3`、`max_records = 20`、`answer_timeout = "30s"`、`request_timeout = "60s"`。
所有时长必须是字符串，不能写成整数秒。

### `[auth]` 与环境覆盖

| 键 | 覆盖变量 | 默认与要求 |
| --- | --- | --- |
| `password_hash` | `SHIYI_AUTH_PASSWORD_HASH` | 空；正常模式必填，完整 bcrypt 编码，成本 10–14，生成使用 12 |
| `state_dir` | `SHIYI_AUTH_STATE_DIR` | `~/.local/state/shiyi/auth`；容器 `/auth`；必须独立于记忆目录 |
| `public_origin` | `SHIYI_AUTH_PUBLIC_ORIGIN` | 空；必须显式配置唯一 `scheme://host[:port]`，正常模式只允许 HTTPS |
| `dev_anonymous` | `SHIYI_DEV_ANONYMOUS` | `false`；只有明确 `true` 开启开发匿名模式 |

来源不能包含路径（包括尾斜杠）、用户信息、query、fragment 或多个来源，不从 Host、Referer 或转发头推导。
认证环境变量**已设置即覆盖**配置文件；空值也覆盖，不能回落到旧凭据。开发布尔覆盖只接受 `true`/`false`。
其他覆盖变量：`SHIYI_LISTEN` → listen、`SHIYI_DATA_DIR` → data_dir、`SHIYI_SHARD` → shard。

开发匿名模式仅用于隔离的本地开发，仍要求明确来源并执行 Origin + CSRF 校验；允许 HTTP，页面明确标识开发模式。
它不验证密码、不签发生产登录 Cookie。生产及容器验收不能以该模式代替安全登录。

启动检查配置存在性、模型配置与密钥是否齐备、`rg`、数据目录、来源、哈希和认证目录权限/状态/单写者锁。
任何错误在监听前拒绝启动；不因漏配、坏状态或权限不足自动匿名。一个认证目录只允许一个服务进程写入。

## 登录、恢复与迁移

- 单用户、无用户名/注册/邮箱找回；同一密码可以在多台设备建立独立登录会话。
- 连续 **7 天**无有效使用过期，每次登录最长 **30 天**。正常服务重启复用认证卷，保留仍有效登录。
- 有效使用是通过登录、来源、CSRF 与 JSON 类型校验的记忆 POST；之后即使业务 JSON 或模型失败也会推进闲置期限。
  页面加载、健康检查、会话检查和失败鉴权不续期。绝对 30 天期限不滑动。
- “退出当前设备”持久撤销当前登录；“退出所有设备”要求再次验证密码并撤销全部登录。
- 忘记密码时，在服务器通过 `shiyi hash-password` 生成新哈希，替换配置/环境变量，停止旧服务后用新配置重启。
  **即使同一密码重新哈希，编码变化也会作废全部旧登录会话**；不要将旧认证状态回滚作为恢复已撤销访问的方法。
- 认证状态丢失会建立全新空状态，旧 Cookie 失效；状态损坏、权限不安全或不可读则启动失败，不开放访问。
- 未成功提交的草稿只留当前页面内存；认证过期后重新登录不会自动重发，须主动提交。主动退出清空草稿和私人视图。
  刷新不会恢复历史查询/回答；登录 Cookie 持续存在不表示保存了对话上下文。

### 从旧 Bearer / CLI 迁移

删除配置里的旧 `auth_token_env` 键，并从服务环境中**彻底取消** `SHIYI_AUTH_TOKEN`，不是仅置空。
两者存在（包括空值）都会给出迁移错误。旧 Bearer token 不再授予访问资格。
准备密码哈希、唯一 HTTPS 来源、独立认证目录，再用 Web 登录。
一次性文本发送、REPL、客户端地址选项和客户端 token 读取均已移除；旧调用明确失败，不会静默发送原文。

## HTTP API

以下是 Web 所依赖的协议，不提供新的脚本客户端或长期 API token 产品。
生产登录 Cookie 为 `__Host-shiyi-session`，bootstrap Cookie 为 `__Host-shiyi-csrf`：
`Secure; HttpOnly; SameSite=Strict; Path=/`，无 Domain。浏览器脚本不读 Cookie，不把凭据存入 localStorage/sessionStorage。

| 路由 | 行为 |
| --- | --- |
| `GET /auth/session` | 返回 `authenticated`、`mode`、`csrf_token`，未登录也可 bootstrap，不续期 |
| `POST /auth/login` | JSON `{"password":"原样密码"}`，成功签发全新登录资格 |
| `POST /auth/logout` | 空 body，204，撤销当前登录 |
| `POST /auth/logout-all` | 同上 PasswordRequest，再次验证密码后撤销全部，204 |
| `POST /messages` | JSON `{"content":"原文"}`，先认证再读业务 body/分类/存储 |
| `GET /healthz` | 公开最小 `{"status":"ok"}`，不检查模型 |
| `GET /` 与必要静态资源 | 公开登录界面，不嵌入私人数据 |

页面先同源 GET session 获取浏览器绑定的 bootstrap CSRF challenge，再携带 Cookie、唯一且与配置匹配的 `Origin`、
`X-CSRF-Token` 和 `Content-Type: application/json` 发起 POST（包括退出）。登录成功后使用新会话的 CSRF token。
缺失/错误/跨站来源或 CSRF 均拒绝；没有 CORS 授权，不盲信转发 IP 或转发来源。
所有认证及私人业务响应为 `Cache-Control: no-store`；反向代理也不得缓存这些响应。
密码不 trim、不归一化；空密码或超过 72 UTF-8 字节无效。密码端点 body 上限 4096 字节。
登录与退出所有设备共享每进程全局限速：每 5 分钟最多 10 次实际密码尝试（成功也计数），429 带 `Retry-After`，不会永久锁定账号。

### 记忆业务响应不变

记录 → 201：

```json
{"kind":"record","time":"2026-11-04T09:12:33.481902417Z","version":1}
```

查询 → 200：

```json
{"kind":"query","answer":"你提到过茶。","citations":[{"time":"2026-11-04T09:12:33.481902417Z","content":"我喜欢喝茶"}],"status":"ok"}
```

查询 `status` 为 `ok`、`budget_exhausted`（预算耗尽，引用仍合法）或 `no_match`：

```json
{"kind":"query","answer":"没有找到相关记忆","citations":[],"status":"no_match"}
```

无法识别 → 200 `{"kind":"unrecognized"}`，不保存、不授予证据资格。查询和回答也不持久化。
统一错误形状：

```json
{"error":{"code":"invalid_request","message":"content 不能为空"}}
```

| HTTP | code | 含义 |
| --- | --- | --- |
| 400 | `invalid_request` | 非法 JSON/参数/Content-Type 或空原文 |
| 401 | `unauthorized` | 缺失、过期、撤销、伪造登录或错误密码（仅 Bearer 也无效） |
| 403 | `forbidden` | Origin / CSRF 拒绝 |
| 413 | `request_too_large` | body 超限 |
| 429 | `rate_limited` | 密码尝试限速，按 Retry-After 再试 |
| 500 | `storage_failed` | 记忆写入或 fsync 失败 |
| 500 | `search_failed` | 检索失败 |
| 500 | `internal_error` | 其他内部错误 |
| 502 | `invalid_citation` | 引用不在本次检索证据中 |
| 502 | `model_invalid_response` | 模型输出非法 |
| 503 | `model_unavailable` | 模型不可达/超时/非 2xx |
| 503 | `auth_unavailable` | 认证状态无法安全读写；不得继续业务 |

错误和日志不包含密码、哈希、Cookie/会话凭据、CSRF token、模型密钥、完整原文或模型原始响应。

## 服务管理命令

```bash
./shiyi serve
./shiyi hash-password
./shiyi --help
./shiyi --version
```

仅保留服务管理。哈希命令要求 TTY、不回显、双确认、拒绝参数及 non-TTY；误传参数也不回显可能的密码。
它不加载 serve 配置、不调用模型；stdout 仅输出成本 12 的 bcrypt 哈希，提示和固定错误写 stderr。
服务收到 SIGINT/SIGTERM 后先停止 HTTP，再释放认证状态资源。

## 数据格式与隐私

一行一条记忆，**恰好三个字段**：

```json
{"version":1,"content":"我喜欢喝茶","time":"2026-11-04T09:12:33.481902417Z"}
```

- `version` 当前为 1；`content` 是逐字节保留、不 trim、不摘要的原文。
- `time` 为服务端 UTC RFC3339Nano（固定九位小数），严格递增且唯一，是标识也是引用锚点。
- monthly：`2026-11.jsonl`；weekly：`2026-W45.jsonl`；daily：`2026-11-04.jsonl`。UTC 分界。
- 尾部不完整行移至 corrupt 目录再截断；中间损坏会拒绝启动，不静默丢失。

JSONL 是唯一记忆真相源。认证状态是获准独立持久化的访问控制状态，不是记忆，不进入检索或引用。
记忆和认证目录都不要静态发布；权限应仅允许运行身份和受信管理员访问。

**JSONL 仍是明文**，没有应用层或端到端加密。宿主机加密磁盘保护关机/离线磁盘，不防运行中的受信管理员；
备份应加密，解密密钥与备份分离保管。明文压缩包不是加密备份。登录和 HTTPS 都不能替代静态数据保护。

## 已知限制与后续方向

1. **无回答持久化**：查询、回答、模型原始响应与对话历史不保存；只有登录会话及必要认证状态独立持久化。
2. **无幂等**：主动重复与网络重试都可能产生新记录，没有去重或幂等键。
3. **无对话上下文**：查询必须自包含，不支持多轮追问。多设备登录不等于同步历史视图。
4. **不支持附件**：没有图片、PDF、音频、OCR 或视觉模型。
5. **跨换行检索仍有限制**：JSONL 的物理行和原文内部换行不同；本次不交付空白归一化修复，不宣称语义召回完整。
6. **记录不可更正、不可删除**：当前只追加；矛盾记忆并列，不由模型裁决覆盖。
7. **明文与外发边界**：允许敏感原文发送到配置的网关及上游模型；自定义网关不等于数据完全不外发。
8. **模型故障不影响认证**：登录、注销、撤销、会话检查及存活仍可用；模型业务明确失败，不绕过分类保存原文，不离线回答或后台排队。
9. **引用校验只能保证引用存在**：不能保证回答每句话都有依据或零幻觉，仍须核对原文。
10. **不做向量检索**：现有字面匹配不能保证同义表达召回。
11. 单用户、单进程写者；不做注册、角色、MFA、设备列表、多副本或多租户隔离。

后续独立交付方向是检索正确性、提交幂等、逐项回答证据校验，以及删除/更正及备份遗忘语义；这些均非本次已实现能力。

## 开发与门禁

依赖 Go 1.26、ripgrep、Python 3、OpenSSL 和可分配 PTY 的环境。Go 依赖固定版本并提交 vendor，模型使用本地替身，不依赖真实密钥。
构建容器还需 Docker 与基础镜像/系统软件包来源；测试不会修改实际部署。

**`make check` 是唯一的本地门禁**，CI 执行同一目标；并行工作树期间按锁串行运行：

```bash
flock /tmp/shiyi-web-auth-make-check.lock make check
make image-check
```

| check 步骤 | 内容 |
| --- | --- |
| prereqs | rg / python3 / openssl |
| verify-vendor | go mod verify、go mod vendor、vendor 无漂移 |
| vet | `go vet ./...` |
| compile | `go build ./...` |
| test | `go test ./... -race -count=1` |
| test-frozen | 版本固定、不可就地修改的冻结测试集 |
| smoke | 真实二进制、TTY、临时 HTTPS 代理、Cookie jar、模型替身，安全配置/登录/业务/重启/撤销 |
| readme-check | 配置、文档、镜像、错误码和服务管理命令交叉检查 |

`make image-check` 实际构建镜像并运行非 root、独立卷、默认安全拒绝/登录/重启/撤销冒烟，不用匿名模式救绿。
冒烟动态分配端口、按真实 HTTP 就绪继续，不使用固定启动延时；所有容器与数据仅为临时测试资源。
这些检查不等于真实浏览器验收，也不证明你的公网代理配置安全。

其他目标：`make build`、`make run`、`make test`、`make vet`、`make test-frozen`、`make smoke`、`make readme-check`、`make image`、`make fmt`、`make clean`。
单独子目标只用于定位，不替代完整门禁。[冒烟脚本](<scripts/smoke.sh>)和[文档检查](<docs/check-readme.sh>)可直接运行；
[冻结测试实现](<internal/frozen/frozen_test.go>)仍属于常规测试，冻结用例不能为迎合认证而修改。

更多容器、systemd、HTTPS、恢复和备份细节见[部署说明](<docs/deployment.md>)。
