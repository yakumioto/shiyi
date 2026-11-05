# 03: Web 登录与私人界面生命周期

Status: ready-for-agent
Execution: completed
Blocked by: 02

## 范围

按 ../spec.md §Web 交互与 ../seams.md 实现。写范围为 internal/web/、internal/httpapi/web_test.go（若需适配）以及 tests/browser/ 下最小可复用的真实 HTTPS 浏览器测试夹具/驱动；不能改认证接缝、生产 cmd、Makefile 或部署文档。与 04 并行。浏览器测试夹具由本票随 UI 行为先落地并实际运行，05 继承扩展全量验收并接入门禁/CI，避免新增 UI 只由源码字符串断言驱动。

页面区分未登录/登录/过期/临时服务错误；密码登录、当前退出、再次验证密码退出全部设备；未提交原文仅页面内存保存，过期或模型错误后保留，重登不自动重发。主动退出清除原文/回答/引用/草稿；失效清除旧私人视图；多标签页和页面恢复需检查状态并处理异步响应竞态，禁止旧响应复活退出前视图。无凭据浏览器存储，无历史持久化；保持安全 textContent 渲染和无第三方脚本。

## 验收

- [x] 登录/退出交互完整，Cookie/CSRF 严格遵守冻结契约。
- [x] 失效保留草稿但不自动提交，主动退出清屏；页面恢复与异步竞态安全。
- [x] 保留既有安全渲染护栏，新增并实际运行最小真实 HTTPS 浏览器交互回归；提供可复用夹具与命令，05 扩展完整浏览器矩阵并最终接入验证流程。
- [x] `make check` 通过，合并最新集成 tip，无冻结接缝漂移。

## Comments

已完成。功能提交 `79007d8`，票 tip `101d82a` 已同步 `7736eb0`，集成提交 `e1d8461`；分支 `feat/web-auth`。

合并后 `flock /tmp/shiyi-web-auth-make-check.lock make check` exit 0；`CHROME_NO_SANDBOX=1 CHROME_BIN=/bin/google-chrome node --test tests/browser/web-auth.test.mjs` 15/15 通过、0 skip（Node 24.22.2、Chrome 153，测试 profile/HOME/XDG 全隔离，只有 fixture SPKI 证书例外）。用例包括真实 Cookie 收发、过期/模型失败草稿保留与重登不重放、当前/全部设备退出、跨标签页、实际 BFCacheRestore+persisted、延迟业务/认证响应不能复活旧私人视图、错误分层、限速恢复、XSS 文本化和原生密码表单不进 URL。

浏览器驱动及真实组件 HTTPS 夹具位于 `tests/browser/`，没有生产测试路由、npm 依赖或真实模型前置。页面隐藏和同页过期保留草稿；实际页面离开（含进入 BFCache）额外保守清草稿，刷新不恢复历史。冻结认证接口未改；完整验证流程/CI 接入由 05 继续，容器验收仍归 04/05。
