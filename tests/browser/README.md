# 真实 HTTPS Web 回归

真实浏览器验收接缝：由 `make browser-check` 执行，并纳入唯一的本地 `make check` 和 CI 的同一目标。本目录不启动生产服务、不修改生产路由、不连接真实用户 Chrome，也不需要模型网关或真实凭据。

## 执行

从仓库根目录执行（Node 24、Go、ripgrep、已有 Chrome/Chromium 必须可执行；缺失会失败，不跳过）：

```sh
CHROME_BIN=/bin/google-chrome make browser-check
# 同一套用例的直接入口：
CHROME_BIN=/bin/google-chrome node --test tests/browser/web-auth.test.mjs
```

在禁止 Linux namespace/setuid sandbox、不能写 `/dev/shm` 的受限测试环境中，明确选择：

```sh
CHROME_BIN=/bin/google-chrome CHROME_NO_SANDBOX=1 node --test tests/browser/web-auth.test.mjs
```

只有后一命令向隔离的测试 Chrome 传 `--no-sandbox`。这是测试环境限制，不是生产部署建议；测试只加载本地 fixture。驱动始终使用 `--disable-dev-shm-usage`，不依赖可写 `/dev/shm`。不用 npm，不下载浏览器，无需额外 Node 包。

`make browser-check` 只用于定位，不替代完整 `make check`。后者已包含这 17 条用例、Go race/冻结集、真实 TTY/进程冒烟和文档检查：

```sh
CHROME_BIN=/bin/google-chrome flock /tmp/shiyi-web-auth-make-check.lock make check
# 受限测试环境必须显式选择；仅测试 Chrome 关闭 sandbox：
CHROME_NO_SANDBOX=1 CHROME_BIN=/bin/google-chrome flock /tmp/shiyi-web-auth-make-check.lock make check
```

`flock` 串行化同机并行工作树门禁；fixture、Chrome 调试端口和进程 smoke 端口均动态分配。
CI 安装 Node 24、Chrome stable 及运行依赖，以安装结果的 `CHROME_BIN` 运行同一 `make check`，另有独立容器门禁。
缺工具/Node 主版本不符会明确失败，无 skip。PTY 权限拒绝也会让真实终端 smoke 失败，不能绕过或把局部浏览器通过称作完整门禁通过。
本地验证版本为 Node 24.22.2 / Chrome 153.0.8010.47；提交 CI 配置不表示远端 CI 已实际运行。

## 设施与边界

- [fixture/main.go](fixture/main.go)：真实 `auth.New`（bcrypt 测试密码）、`httpapi.New`、`web.Handler`、分类、回答、JSONL store 和 ripgrep search；只有外部模型 `llm.Client` 及 `auth.Options.Now` 为可控替身。
- [driver.mjs](<driver.mjs>)：Node 原生 WebSocket/CDP；每个用例独占 `mkdtemp` 根目录、认证目录、记忆目录、Chrome profile、HOME/XDG 目录与进程。独立 browser context 表示设备，同 context 的多个 target 表示共享 Cookie 的标签页。关闭重开用例使用默认持久 context（不是无痕 context），等待原 Chrome 正常退出、核对不同进程，再以原临时 profile 启动新 Chrome；不导出/注入 Cookie。测试专用 `--password-store=basic` 避免访问用户桌面密钥环；该 profile 只含测试凭据，结束后删除，不用于生产。
- HTTPS 通过 Go `httptest` 临时证书提供，Chrome 证书例外只匹配 fixture 证书公钥 SPKI，不全局忽略证书错误。真实 UI 登录后由 Chrome 接收/发送 Secure、HttpOnly、SameSite=Strict Cookie；不预塞 Cookie、不改 Cookie 安全属性。
- fixture 控制使用 stdin/stdout JSON 行，**没有测试 HTTP 路由**。`advance` 推进认证时钟（秒）；`model` 设置分类种类、持续错误及回答脚本；`stats` 返回模型分类调用数；`close-auth` 通过公开生命周期使认证不可用。`restart-service` 先停 HTTPS、再公开 `Close` 释放 auth 锁，随后以相同认证/记忆目录和密码哈希重新构造真实组件并监听原 HTTPS 来源；不是 auth mock 或测试 HTTP 路由。
- CDP Fetch 只延迟实际服务已经生成的响应，或模拟网络不可达；正常授权、错误码、Cookie、CSRF 都来自真实后端。模型计数仅用于验证未授权操作不进入模型以及登录不重放输入，测试不读取认证私有持久格式。
- 按服务器就绪行、DOM MutationObserver、网络/CDP 事件和浏览器绘制帧等待，不使用固定 sleep。BFCache 用例必须观察实际 `BackForwardCacheRestore` 和 `pageshow.persisted`；若当前 Chrome 不支持，不会用合成事件或 reload 冒充通过。
- 用例结束先优雅关闭 Chrome、停止 fixture，再校验并删除本次创建的临时目录；不扫描或清理真实用户 profile。

## 当前用例

[web-auth.test.mjs](web-auth.test.mjs) 覆盖：

- HTTPS 登录 Cookie 的实际接收与业务发送；原文空白保留。
- Chrome 实际关闭重开后有效 Cookie 仍保留；同目录/哈希的服务与 auth 重构后浏览器仍可提交。两者均验证新页面不恢复私人原文、查询、回答、引用或草稿，不自动重放；不读取私有持久格式。
- 7 天闲置到期后清除私人展示，草稿只留本页内存，重登不重放，主动再提交及刷新不恢复历史。
- 模型失败保留原文；错误密码不注销；再次验证正确密码后撤销全部设备，模型故障不阻碍认证。
- 当前退出清除原文、草稿，另一独立设备仍可使用。
- 共享 Cookie 标签页的退出/登录同步；页面离开前清除私人 DOM/草稿；真实 BFCache 返回时重新认证。
- 已生成业务响应在退出/重登后不能复活旧视图；旧认证检查不能覆盖较新的退出；另一个设备撤销已获准输入时，旧结果不再展示。
- 密码限速与错误密码/过期区分，受控时间恢复；真实 CSRF 拒绝保留草稿且不重试；网络断开不谎报退出成功；认证状态故障不显示为模型故障。
- 显式开发匿名标识及密码/全部退出入口隐藏。
- 原文、模型回答、真实检索引用的 HTML/XSS 文本化；无浏览器持久化存储、无脚本可读 Cookie、无外部资源。
- 密码表单的原生提交后备路径只能 POST，不能把密码放入 URL 或原生表单 body。

说明：页面隐藏保留草稿在内存，页面离开（含进入 BFCache）与主动退出会清除草稿；这避免冻结页面错过退出通知后重新显示私人输入。正在执行的服务端业务不能由 UI 撤销，本测试只保证失效后的结果不重新显示；网络结果不确定时明确提示手动重试可能重复记录，不声称已有提交幂等。

后端的 30 天绝对期限、撤销/密码变更/损坏/丢失状态、完整 CSRF 矩阵等由既有 HTTP 验收承担，不在浏览器重复每个排列。真实进程和容器启停由 `make check` 的 smoke 与独立 `make image-check` 承担；本目录的服务重构不冒充操作系统进程或容器重启。完整规格 1–13 的证据与执行限制见[验收记录](<../../.scratch/web-auth/validation.md>)。
