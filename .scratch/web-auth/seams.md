# Web 认证冻结接缝（01）

本清单与 [规格](<spec.md>)、[实施图](<implementation.md>) 配套。**01 只提交可编译契约，不启用登录，不提供 `auth.New` 占位实现，也不改变旧 Bearer/匿名行为。** 下列运行要求由 02 实现，03/04 消费；声明存在不代表安全功能已交付。更改冻结接口须先由 Lead 协调下游。

## 归属、依赖与组装

| 包/文件 | 归属与责任 |
| --- | --- |
| [config 契约](<../../internal/config/auth_contract.go>)、[Config.Auth](<../../internal/config/config.go>) | 01 冻结名称/类型；02 加载、环境覆盖、校验及迁移拒绝 |
| [auth 契约](<../../internal/auth/contract.go>) | 01 冻结；02 封装密码验证、持久状态、撤销、限速、来源、CSRF、Cookie 和认证 HTTP 响应 |
| [HTTP 线格式](<../../internal/api/auth.go>)、[错误码映射](<../../internal/api/api.go>) | 01 冻结；02/03 共用，不另造 JSON 形状 |
| [HTTP 入口](<../../internal/httpapi/server.go>) | 02 挂载认证 handler，以 `Protect` 包住记忆业务；不重复实现认证判断 |
| Web 静态资源及 Web 测试 | 03；只消费 HTTP 线格式，不读取 Cookie、不依赖认证内部结构 |
| cmd / 部署 / 文档 / 示例 / CI | 04；构造并关闭认证模块，迁移旧 CLI；01 的示例说明仅为接缝兼容 |
| 真实浏览器验收设施 | 05；测试 HTTPS Cookie、草稿、退出和页面恢复 |

依赖方向：`config` 与 `api` 不依赖 `auth/httpapi`；`auth → config, api`；`httpapi → auth, api`；组合入口构造这些模块。`auth` 不依赖分类、模型、检索或记忆存储。`config.Validate` 负责配置存在性、布尔值、路径隔离、来源语法和旧配置迁移检查；bcrypt 编码/成本和状态安全检查由 `auth.New` 负责，不能为了校验从 `config` 反向 import `auth`。两阶段均在监听前完成，`auth.New` 也独立拒绝非法配置，便于直接构造的 HTTP 测试。持久格式、密码比较、随机源、限速内部协作者不形成额外公共 seam。

02 实现 `func auth.New(config.AuthConfig, auth.Options) (auth.Module, error)`，其类型由 `auth.NewFunc` 固定；可用 `var _ auth.NewFunc = auth.New` 检查。`Module` 只有 `ServeHTTP`、`Protect(next)` 和生命周期 `Close()`。`Close` 幂等、释放单写者锁，必须在 HTTP 停止后调用；关闭后的模块不得放行。构造失败不启动 HTTP。

02 在 `httpapi.Deps` 增加必填的 `Auth auth.Module`；nil 是构造错误，不能由 HTTP 层猜测匿名模式。保持 `httpapi.New(Deps) (*Server, error)`、`Handler()` 和 `HandlerWithWeb(http.Handler)` 形状。将 `/auth/` 交给 `Auth`，`POST /messages` 交给 `Auth.Protect`；健康检查与静态资源不包入 Protect。02 新增 `httpapi.Server.Close() error`，幂等关闭其持有的认证模块；调用者先完成 HTTP Shutdown 再 Close。04 在组合入口调用真实 `auth.New`，若 `httpapi.New` 失败须自行关闭模块，成功后由 Server.Close 释放；HTTP 测试也注册 Close，重构前关闭旧 server/module。HTTP 时间测试注入真实模块而非模拟认证结果。

## 配置与密码

| TOML `[auth]` 键 | 环境覆盖 | 默认/约束 |
| --- | --- | --- |
| `password_hash` | `SHIYI_AUTH_PASSWORD_HASH` | 空；正常模式必填。bcrypt 完整编码，接受成本 10–14，生成推荐成本 12 |
| `state_dir` | `SHIYI_AUTH_STATE_DIR` | `~/.local/state/shiyi/auth`；加载时展开 `~`，与记忆目录分离，不放在记忆目录内 |
| `public_origin` | `SHIYI_AUTH_PUBLIC_ORIGIN` | 空；所有模式都必须显式配置唯一来源。正常模式只允许 HTTPS |
| `dev_anonymous` | `SHIYI_DEV_ANONYMOUS` | `false`；只有显式 true 开启开发匿名模式，允许 HTTP 来源 |

环境已设置即覆盖文件，空白值不能回落到文件凭据；开发布尔值只接受明确的 `true`/`false`，其他值报错。正常模式缺失、空白、无效哈希/来源均拒绝启动。开发模式不要求哈希或认证状态，但仍要求明确来源和 CSRF；不得根据漏配、请求地址、Host 或转发头切换模式。

选用 `golang.org/x/crypto/bcrypt`，02 加入固定版本并提交 vendor。密码原样验证，不 trim、不归一化；空密码或超过 72 UTF-8 字节均不能认证。明文密码不进入配置、日志、URL 或持久状态。原始哈希也不得出现在日志/错误里。初始化和恢复说明由 04 提供，不恢复记忆 CLI。

02 拒绝 TOML 中出现旧 `auth_token_env`（包括空值），也拒绝设置旧默认 `SHIYI_AUTH_TOKEN`（包括空值）；错误明确要求迁移，不回显值。请求中的旧 Bearer token 在新后端不授予访问资格。01 暂留旧字段和行为，不能提前声称已移除。

## HTTP 路由与线格式

所有请求/响应为同源交互，不提供跨源 CORS 授权。认证与受保护业务的成功/失败响应均 `Cache-Control: no-store`。

| 路由 | 请求 | 正常响应 | 登录要求 / 是否推进闲置期限 |
| --- | --- | --- | --- |
| `GET /auth/session` | 无 body | 200 `SessionResponse` | 无；**不推进**。失效凭据返回 `authenticated:false` 而非 401 |
| `POST /auth/login` | `PasswordRequest` | 200 `SessionResponse`，新 Cookie | 无；正确密码创建全新登录会话，不沿用客户端标识 |
| `POST /auth/logout` | 空 body | 204，无 body，清 Cookie | 有效登录会话；持久撤销当前设备，其他设备不变 |
| `POST /auth/logout-all` | `PasswordRequest` | 204，无 body，清 Cookie | 有效登录会话及正确密码；持久撤销含当前设备的全部登录会话 |
| `POST /messages` | 现有 `MessageRequest` | 现有记录/查询/无法识别响应不变 | 有效登录会话 + 来源/CSRF/JSON；通过保护后才进入原业务 |
| `GET /healthz` | 无 body | 现有最小存活响应 | 公开；不推进、不调用模型 |
| 页面与必要静态资源 | 无私人数据 | 现有 Web 资源 | 公开；不推进、不嵌入凭据或私人原文 |

`SessionResponse` 示例：

```json
{"authenticated":false,"mode":"password","csrf_token":"opaque-browser-bound-token"}
```

成功登录/已登录检查为 `authenticated:true`，`mode:"password"`，返回当前登录会话绑定的 CSRF token。无任何登录凭据出现在 JSON 中。开发匿名检查为 `authenticated:true, mode:"development_anonymous"`，同样提供 CSRF token；UI 必须明确标识开发模式，不展示密码/退出所有设备动作。开发模式登录与退出所有设备返回 400 `invalid_request`，不能假装验证了密码；退出当前设备通过来源/CSRF 后仅清理 bootstrap Cookie，返回 204。

`PasswordRequest` 为 `{"password":"原样密码"}`。密码端点请求体上限 4096 字节；解码错误/不支持的 Content-Type 返回 400，超过上限返回 413。`POST /messages` 保留既有上限与语义。未知认证路径/不支持方法分别 404/405，仍用现有 `ErrorResponse`，方法错误附 `Allow`。

## 明确来源 + CSRF 协议

1. `public_origin` 是规范化的 `scheme://host[:port]`，不得包含用户信息、路径（包括尾 `/`）、query 或 fragment；不得配置多个来源。比较按解析后的 scheme、规范化 hostname 和有效端口，不能用字符串前缀/后缀匹配。生产必须 HTTPS，不能从请求 Host、Referer、`Forwarded`、`X-Forwarded-*` 推导可信来源。
2. 每个状态变更（登录、退出、退出所有设备、记忆提交，包括开发模式）必须有且仅有一个合法 `Origin`，与配置来源相同。缺失、`null`、多值、欺骗性子域、不同端口或 scheme 均拒绝；没有头也不能放行。SameSite 不是唯一防护。
3. 页面先以同源 `fetch`、`credentials:"same-origin"` 调用 `GET /auth/session`。GET 无 Origin 可用于同源 bootstrap；若带 Origin 则同样严格校验，`Sec-Fetch-Site: cross-site` 拒绝。不得启用 CORS/JSONP。此 GET 不建立登录资格、不 touch。
4. 未登录时 GET 签发/复用一个**浏览器绑定且有过期时间**的 bootstrap CSRF challenge：一个 HttpOnly bootstrap Cookie + JSON `csrf_token`。必须校验 Cookie 的真实性与 token 绑定，不能只比较两个攻击者可任意选择的字符串；允许内部采用持久密钥认证的无状态 challenge，不要求冻结存储格式。无效、缺失、过期均拒绝变更。GET 复用仍有效 challenge，避免不同标签页互相失效。
5. 所有 POST 将 JSON token 放入 `X-CSRF-Token`；缺失/空/多值/错误 token 一律 403。密码 POST 和记忆提交必须为 `application/json`（允许合法 charset 参数）；退出虽无 body，仍发送该 Content-Type，不能以表单/文本/省略头绕过来源和 CSRF。
6. 正确登录创建新的高熵不透明登录凭据（至少 256 位随机性），以及与该登录会话绑定的 CSRF token，返回 JSON，清掉 bootstrap Cookie；不能以登录前 challenge 继续调用受保护业务。已有有效登录会话发起重新登录时使用该会话 CSRF，成功后撤销当前旧会话并签发新会话；失败不改变旧会话。
7. 已登录 GET 返回当前会话 token，不轮换、不延长闲置期限。受保护 POST 必须使用此 token 与登录 Cookie；仅 bootstrap challenge 不能授权业务。失效会话检查清旧 Cookie 并重新 bootstrap，主动注销之后由页面重新 GET 获取 challenge；不得自动重发草稿。
8. Protect 先验证登录会话，失效统一 401，再验证来源、CSRF 和 Content-Type，且以上均在读业务 body、模型调用和记忆副作用之前。登录端点先来源/CSRF、限体积/解码，再限速验证密码；再次验证还须先有有效登录会话。

## Cookie、时间与持久状态

- 正常登录 Cookie：`__Host-shiyi-session`；bootstrap Cookie：`__Host-shiyi-csrf`。均 `Path=/`、无 Domain、HttpOnly、Secure、SameSite=Strict。认证不信任转发头来决定是否加 Secure；外部 HTTPS、内部 HTTP 代理部署仍签发 Secure Cookie。
- 登录 Cookie 有 Expires/Max-Age，最迟为创建后 30 天；服务端独立实施 7 天闲置上限，浏览器保有 Cookie 不代表它仍有效。清理使用相同名称/作用域/安全属性及过去 Expires、`Max-Age=0`（Go Cookie 的负 MaxAge）。
- bootstrap 最长 20 分钟，无需持久保留页面 token。开发模式只使用 `shiyi-dev-csrf`，HttpOnly、SameSite=Strict、Path=/、无 Domain，可不加 Secure；不签发生产登录 Cookie。
- 所有时间取 `Options.Now`，nil 用 `time.Now`。判定 `now >= lastUsed+7d` 或 `now >= created+30d` 即过期；先判定再续期，绝对期限不滑动。时钟注入同样覆盖 CSRF、限速、Cookie 与 Retry-After，不引入 sleep。
- **有效使用**在本协议精确定义为：通过有效登录会话、来源、CSRF 与 JSON Content-Type 检查而获准进入受保护记忆 POST。此时先持久推进 lastUsed 再调用下游，即使之后输入 JSON 无效或模型失败也属于主动业务尝试；不是模型成功才续期。健康检查、页面加载、会话检查、认证失败/来源失败/CSRF 失败不续期，UI 不后台提交记忆来保活。
- 认证目录 0700、状态文件 0600；拒绝不安全权限、损坏或不可读状态，不回退匿名。新目录/状态丢失允许创建全新空状态，旧 Cookie 均失效；这不是恢复旧资格。仅支持单进程写者，构造时获得目录锁，重构测试先 Close 再 New。
- 正常重启复用独立状态；创建、续期、撤销均持久成功才报告成功，失败 503，Protect 续期失败不执行下游。密码哈希编码变化即保守作废所有旧登录会话；不支持以回滚旧认证状态恢复已撤销访问。
- 会话标识/密码/CSRF token/哈希不进入日志；认证状态不进入记忆分片、检索、引用。认证状态格式、持久化原子实现与随机实现由 02 隐藏，不作为测试断言对象。

## 密码尝试限制与错误映射

登录与退出所有设备的密码校验共享每进程全局固定窗口：每 5 分钟最多 10 次实际密码尝试，成功也计数、不清空窗口；窗口届满自然恢复，不设永久锁定。请求先经来源/CSRF 和体积校验，额度在昂贵密码比较前原子获取；不得盲信转发 IP 拆分额度。限速状态可随进程重启重置，登录会话/撤销状态不可。测试用注入时钟验证恢复；429 的 `Retry-After` 为至少 1 的向上取整秒数。

| `errors.Is` 分类/情形 | 错误码 | HTTP |
| --- | --- | --- |
| `auth.ErrInvalidConfig` | 启动失败，不映射 HTTP | — |
| `auth.ErrUnauthenticated`（缺失/过期/撤销/伪造 Cookie、错误或空密码、仅 Bearer） | `unauthorized` | 401 |
| `auth.ErrForbidden`（来源/CSRF 无效） | `forbidden` | 403 |
| `auth.ErrRateLimited` | `rate_limited` + `Retry-After` | 429 |
| `auth.ErrUnavailable`（认证状态读写或安全性故障） | `auth_unavailable` | 503 |
| 非法 JSON/Content-Type/认证操作参数 | `invalid_request` | 400 |
| 请求体超限 | `request_too_large` | 413 |
| 其他未知错误 | `internal_error` | 500 |

继续使用 `{"error":{"code":"...","message":"固定公开文本"}}`。错误密码不泄露验证细节；所有错误禁止拼接底层错误、原文或任何凭据。业务原有错误码及映射不变；认证不以模型探测成功为前提。

## 已确认测试 seam 与验收分工

用户已确认 HTTP 主 seam、真实浏览器和服务/容器冒烟，Lead 另确认 01 的真实 JSON 契约测试。

- **01：真实 JSON 编解码和公开错误码映射**：捕获 UI 字段/状态漂移；不声称证明授权、CSRF 或持久化。既有 HTTP 回归经 `make check` 保证本票没有替换授权运行路径。
- **02：真实 `auth.Module` + 应用 HTTP 入口**：配临时目录、可控时钟和现有模型替身，验证授权前无业务副作用、登录/撤销/重构/密码变更、来源+CSRF、限速恢复、敏感日志与模型故障解耦；不测私有函数或序列化布局。
- **03/05：真实 HTTPS 浏览器**：验证 Cookie 实际收发、登录 UI、过期草稿仅留内存且不重发、主动退出清屏、跨标签/页面恢复失效检查；HTTP 测试不能替代浏览器 Cookie 规则。
- **04/05：进程与容器冒烟**：验证安全配置启动/拒绝、CLI 移除、服务启停与探活。此票不修改容器，不运行 `make image-check`，不能将其列为已通过。

本票门禁只使用 `make check`；后续各票保留同一门禁，涉及容器的票再运行 `make image-check`。构造器实现、端点挂载与完整认证验收均留给后端票。
