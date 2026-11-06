# 部署 Shiyi

日常使用见 [README](<../README.md>)。本篇是部署者需要执行的指引，
**不是对实际域名、证书、代理、防火墙或备份的已完成声明**。此次实现不会操作用户真实部署。

## 安全接入前提

- 正常模式仅接受显式配置的唯一 HTTPS 来源，例如 `https://shiyi.example.com`。没有尾斜杠、路径或多个域名。
- 应用内部可监听 HTTP，外部必须经你维护的 HTTPS 反向代理；生产 Cookie 始终带 Secure，不按转发头降级。
- 将后端绑定 loopback 或隔离网络，仅允许可信代理访问。Origin/CSRF 是浏览器防护，不是网络访问控制。
- 不静态发布记忆、认证状态、凭据文件或备份。代理不得记录请求体、Cookie、Authorization、X-CSRF-Token 或密码。
- 不缓存认证和私人业务响应；保留应用的 `Cache-Control: no-store`，不向公共缓存泄露原文。
- 服务器及管理员属于信任边界；模型网关及上游模型可接触发送的原文。登录、TLS、自管网关都不等于端到端加密。

## 初始化密码与忘记密码恢复

先构建服务二进制或镜像：

```bash
make build
./shiyi hash-password
# 或使用镜像中的同一管理入口
docker build -t shiyi:latest .
docker run --rm -it --network none shiyi:latest hash-password
```

必须在 TTY 中交互输入、双确认，输入不回显；密码原样处理，不 trim、不归一化，为 1–72 UTF-8 字节。
使用成熟 bcrypt，生成成本 12，服务接受成本 10–14 的完整编码。不接受参数或 stdin 管道，误传参数不会回显。
该入口不加载 serve 配置，不依赖模型；提示写 stderr，哈希写 stdout。Docker `-t` 会合并终端显示，复制时只取哈希行。

不要把明文密码放在 shell 参数、环境文件、配置、日志或工单，不要启用 shell tracing。
把哈希保存到权限 0600 的服务环境文件（`SHIYI_AUTH_PASSWORD_HASH=完整编码`），或受保护配置的 `[auth].password_hash`。
环境文件一般不作 shell 展开；若从 shell 赋值，请用单引号保护 bcrypt 中的 `$`，或直接捕获管理入口的 stdout。
哈希、模型密钥、会话凭据也须保密，不应复用。

**恢复流程**：服务器管理员生成新哈希 → 替换实际服务使用的配置/环境变量 → 优雅停止旧进程 → 带新配置重启 → 在 HTTPS 页面重新登录。
即使只是对同一密码重新哈希，编码变化也会保守作废全部旧登录会话。忘记密码不需要删除记忆。
不要回滚旧认证状态来“恢复”已撤销设备；状态丢失只允许创建新空状态，所有旧 Cookie 失效。

## 容器

准备一个新建、权限为 0600 的环境文件，通过编辑器填写真实值；不要把示例占位符直接用于部署：

```dotenv
SHIYI_API_KEY=模型密钥
SHIYI_AUTH_PASSWORD_HASH=完整bcrypt编码
SHIYI_AUTH_PUBLIC_ORIGIN=https://shiyi.example.com
```

```bash
docker run -d --name shiyi --restart unless-stopped \
  -p 127.0.0.1:8787:8787 \
  -v shiyi-data:/data \
  -v shiyi-auth:/auth \
  --env-file ./shiyi.env \
  shiyi:latest
```

默认配置来自[容器配置](<config.docker.toml>)，镜像内路径 `/etc/shiyi/config.toml`；可按需只读挂载自己的配置替换。
模型密钥变量名默认 `SHIYI_API_KEY`；认证哈希及来源默认为空，漏配会退出，绝不匿名。

| 路径 | 用途 | 权限与所有权 |
| --- | --- | --- |
| `/data` | 明文记忆 JSONL | 镜像目录 0700，uid/gid 1000 |
| `/auth` | 必需的独立认证持久状态 | 目录 0700，文件 0600，uid/gid 1000 |
| `/etc/shiyi/config.toml` | 配置；镜像默认不含秘密 | 自定义含哈希时应限制宿主访问并只读挂载 |

新命名卷继承镜像目录权限。绑定宿主目录需要部署者预先正确设置 uid/gid 1000 及目录 0700；
不要用 `chmod 777` 或长期以 root 运行救启动。认证目录不能放进记忆目录，不能让多个副本并发写同一认证卷。
两个卷均要保留；重建容器而保留同一认证状态和密码哈希时，仍有效登录会话保持。

升级应先 `docker stop shiyi` 等待优雅退出，再替换容器并复用原来两个卷和凭据配置。
不要用删除/回滚认证卷代替正常升级，也不要在旧进程仍写入时启动第二个写者。

## 本地二进制与 systemd

```bash
make build
./shiyi serve
```

配置默认 `~/.config/shiyi/config.toml`，可用 `SHIYI_CONFIG` 指向别处；示例见[配置示例](<../config.example.toml>)。
本地记忆默认 `~/.local/share/shiyi`，认证默认 `~/.local/state/shiyi/auth`，两者分离。

以下仅为 unit 模板，用户自行安装专用服务账户、二进制、配置和环境文件：

```ini
[Unit]
Description=Shiyi personal memory service
After=network-online.target

[Service]
Type=simple
User=shiyi
Group=shiyi
UMask=0077
Environment=SHIYI_CONFIG=/etc/shiyi/config.toml
Environment=SHIYI_DATA_DIR=/var/lib/shiyi-memory
Environment=SHIYI_AUTH_STATE_DIR=/var/lib/shiyi-auth
EnvironmentFile=/etc/shiyi/shiyi.env
StateDirectory=shiyi-memory shiyi-auth
StateDirectoryMode=0700
ExecStart=/usr/local/bin/shiyi serve
Restart=on-failure
RestartSec=5
TimeoutStopSec=15
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
```

环境文件由 root 管理，权限 0600，内容包含配置所选的模型密钥变量（本地示例为 `DEEPSEEK_API_KEY`）、
`SHIYI_AUTH_PASSWORD_HASH`、`SHIYI_AUTH_PUBLIC_ORIGIN`。不要把明文密码放入 unit 的 Environment。
环境变量并非针对受信服务器管理员的秘密隔离；管理员可读取进程/容器配置，哈希也应限制访问。
服务 SIGINT/SIGTERM 后先 HTTP Shutdown，再关闭认证模块释放单写者锁；不要把强制杀进程作为日常重启流程。

## 反向代理责任

你自行维护域名、证书、TLS 和网络策略。必须保持浏览器看到的来源与 `public_origin` 完全一致。
示例展示必要的请求传递，不是一份可直接替代现有生产代理的完整配置：

```nginx
server {
    listen 443 ssl;
    server_name shiyi.example.com;
    # ssl_certificate / ssl_certificate_key 由部署者配置
    location / {
        proxy_pass http://127.0.0.1:8787;
        proxy_set_header Host $host;
        # 不改写 Origin、Cookie、X-CSRF-Token、Content-Type 或 Set-Cookie。
        # 认证与私人业务禁止缓存；日志不得包含这些敏感头和请求体。
        proxy_cache off;
    }
}
```

应用不从 Host、Referer、Forwarded 或 X-Forwarded-* 推导可信来源/安全模式，不依赖代理转发 Bearer 授权。
外部 HTTPS + 内部 HTTP 时仍使用 `__Host-` Secure Cookie；直接访问内部 HTTP 的网页登录不能作为生产验收。
可用内部 HTTP `GET /healthz` 探活，但这不证明浏览器登录成功，更不证明模型业务可用。

## 登录生命周期与故障排查

- 连续 7 天没有有效记忆 POST 使用即过期；登录后绝对最长 30 天；页面/健康/session 检查不保活。
- 当前设备退出只撤销当前登录；退出所有设备要求再次验证密码。撤销持久化后才成功响应。
- 无效来源/CSRF 返回 403，失效/伪造登录与旧 Bearer 返回 401；密码尝试每 5 分钟最多 10 次，429 带 Retry-After。
- 模型不可用不影响登录/检查/注销/撤销。业务失败不绕过分类保存原文，页面草稿只留内存，主动重试而非自动重发。
- 配置缺哈希/来源、非法哈希、旧 `auth_token_env` 键或 `SHIYI_AUTH_TOKEN` 环境变量（即使空）均拒绝启动。
- 认证状态损坏、权限过宽、无法读取或单写者锁被占用：拒绝启动。不要回退匿名、改成全员可写或移入记忆目录。
- 模型密钥缺失、`rg` 不在 PATH、数据目录不可用、分片/时长非法也会启动失败。

```bash
docker logs shiyi
docker inspect --format '{{.State.Health.Status}}' shiyi
docker run --rm --entrypoint rg shiyi:latest --version
curl -fsS http://127.0.0.1:8787/healthz
```

应用日志不包含密码、哈希、Cookie/会话标识、CSRF token、模型密钥或记忆原文。排障时不要开启代理正文/敏感头日志；
发布日志前仍应核对用户自己配置的路径、主机名及其他基础设施日志的敏感信息。

## 隔离验证与真实部署验收

仓库唯一的本地门禁是 `make check`，包含 `make browser-check`；CI 安装 Node 24 与 Chrome 后运行同一门禁，另跑 `make image-check`。
需要 Go 1.26、ripgrep、Python 3、OpenSSL、Node 24、Chrome/Chromium 和可分配 PTY 的环境；镜像验证还需 Docker。
浏览器默认 `/bin/google-chrome`，可以用 `CHROME_BIN` 指定；缺工具会失败，不能跳过或视为通过。

```bash
CHROME_BIN=/bin/google-chrome flock /tmp/shiyi-web-auth-make-check.lock make check
CHROME_BIN=/bin/google-chrome make browser-check
flock /tmp/shiyi-web-auth-image-check.lock make image-check IMAGE=shiyi:test
```

若隔离测试环境禁止 Chrome sandbox，显式给前两条命令增加 `CHROME_NO_SANDBOX=1`。
这只影响本地临时测试 Chrome，绝不是生产配置；PTY 被拒绝时真实终端冒烟会失败，不能改设备权限或跳过来获得通过。
浏览器夹具使用正常密码模式、临时 HTTPS、隔离 profile、仅限夹具公钥的 SPKI 证书例外；不全局忽略证书错误。
测试实际关闭/重开 Chrome 并复用同一临时 profile；服务/auth 重构复用同一临时认证目录/哈希。
它验证有效 Cookie 持续可用、原文/回答/引用/草稿不会随新页面恢复，不预塞 Cookie、不读取认证或 Chrome 私有序列化。
其余过期、退出、跨标签页、BFCache、竞态、XSS 用例见[浏览器验收说明](<../tests/browser/README.md>)。

这些自动化测试不使用实际模型或部署凭据，也不能证明你的公网域名、TLS、代理或网络策略已安全配置。
部署者仍需在实际 HTTPS 来源通过 Web 登录、记录/查询、关闭重开浏览器、正常重启服务、退出并确认旧页面失效；
检查公开健康接口与模型业务可用性的区别，并确认内部端口不对公网开放。
不要把本地结果或仅提交的 CI 配置当成远端 CI 已运行的证据。

## 从旧版本迁移

1. 停止旧服务；备份记忆并保护备份密钥。
2. 删除 TOML 中 `auth_token_env`，取消服务环境中的 `SHIYI_AUTH_TOKEN`，包括空值设置。
3. 准备密码哈希、唯一 HTTPS 来源、独立认证持久卷及正确权限。
4. 用 `shiyi serve` 启动；通过 HTTPS Web 登录并记录/查询。旧 CLI 文本发送、REPL、客户端地址/token 功能均已移除。
5. 验证公网访问没有后端绕过入口；本次实现不替你检查真实代理、证书或防火墙。

## 备份、恢复与隐私

记忆 JSONL 是明文，读取目录的人就能读取原文；认证状态不是记忆，也不参与检索。
为一致性备份，先优雅停止写者，再分别备份记忆和必要配置；对认证状态的备份及恢复需特别谨慎：
恢复旧认证快照可能复活已撤销访问，**不能**将其当正常恢复路径。灾难恢复可以使用全新认证目录，要求所有设备重新登录。

不要在公开路径产生明文 tar 包。使用部署者选定的成熟备份加密工具，将解密密钥与备份分离保管，并演练恢复。
压缩不是加密；宿主机加密磁盘只保护关机/离线介质，不保护已解锁运行中的服务器免受管理员或完全失陷攻击。
本版本不提供应用层/客户端端到端加密、隐私字段或脱敏；发送给网关及其上游模型的原文仍由你承担提供方信任决策。
查询、回答和对话上下文不持久化，不借登录功能新增查询/回答审计档案。
