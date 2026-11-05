# 真实 HTTPS Web 回归

03 的可复用浏览器接缝；05 可扩展此入口并接入 Makefile/CI。本目录不启动生产服务、不修改生产路由、不连接真实用户 Chrome，也不需要模型网关或真实凭据。

## 执行

从仓库根目录执行（Node 24、Go、ripgrep、已有 Chrome/Chromium 必须可执行；缺失会失败，不跳过）：

```sh
CHROME_BIN=/bin/google-chrome node --test tests/browser/web-auth.test.mjs
```

在禁止 Linux namespace/setuid sandbox、不能写 `/dev/shm` 的受限测试环境中，明确选择：

```sh
CHROME_BIN=/bin/google-chrome CHROME_NO_SANDBOX=1 node --test tests/browser/web-auth.test.mjs
```

只有后一命令向隔离的测试 Chrome 传 `--no-sandbox`。这是测试环境限制，不是生产部署建议；测试只加载本地 fixture。驱动始终使用 `--disable-dev-shm-usage`，不依赖可写 `/dev/shm`。不用 npm，不下载浏览器，无需额外 Node 包。

这不是 `make check` 的替代。03 当前还需独立运行唯一仓库门禁：

```sh
flock /tmp/shiyi-web-auth-make-check.lock make check
```

`flock` 仅串行化同机并行工作树的既有固定端口 smoke；fixture 与 Chrome 调试端口均由系统分配。本目录没有改 Makefile/CI；完整浏览器矩阵及门禁接入由 05 负责。

## 设施与边界

- [fixture/main.go](fixture/main.go)：真实 `auth.New`（bcrypt 测试密码）、`httpapi.New`、`web.Handler`、分类、回答、JSONL store 和 ripgrep search；只有外部模型 `llm.Client` 及 `auth.Options.Now` 为可控替身。
- [driver.mjs](driver.mjs)：Node 原生 WebSocket/CDP；每个用例独占 `mkdtemp` 根目录、认证目录、记忆目录、Chrome profile、HOME/XDG 目录与进程。独立 browser context 表示设备，同 context 的多个 target 表示共享 Cookie 的标签页。
- HTTPS 通过 Go `httptest` 临时证书提供，Chrome 证书例外只匹配 fixture 证书公钥 SPKI，不全局忽略证书错误。真实 UI 登录后由 Chrome 接收/发送 Secure、HttpOnly、SameSite=Strict Cookie；不预塞 Cookie、不改 Cookie 安全属性。
- fixture 控制使用 stdin/stdout JSON 行，**没有测试 HTTP 路由**。`advance` 推进认证时钟（秒）；`model` 设置分类种类、持续错误及回答脚本；`stats` 返回模型分类调用数；`close-auth` 通过公开生命周期使认证不可用。需要模拟多设备、状态重构或新模型脚本时可扩展控制协议，不必碰生产 handler。
- CDP Fetch 只延迟实际服务已经生成的响应，或模拟网络不可达；正常授权、错误码、Cookie、CSRF 都来自真实后端。模型计数仅用于验证未授权操作不进入模型以及登录不重放输入，测试不读取认证私有持久格式。
- 按服务器就绪行、DOM MutationObserver、网络/CDP 事件和浏览器绘制帧等待，不使用固定 sleep。BFCache 用例必须观察实际 `BackForwardCacheRestore` 和 `pageshow.persisted`；若当前 Chrome 不支持，不会用合成事件或 reload 冒充通过。
- 用例结束先优雅关闭 Chrome、停止 fixture，再校验并删除本次创建的临时目录；不扫描或清理真实用户 profile。

## 当前用例

[web-auth.test.mjs](web-auth.test.mjs) 覆盖：

- HTTPS 登录 Cookie 的实际接收与业务发送；原文空白保留。
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

后端的 30 天绝对期限、认证状态重构/密码变更、完整 CSRF 矩阵等由已有 HTTP 验收承担，05 可再补浏览器端更完整的组合。容器验证不属于本目录。
