# 06: 修复双轴审查发现

Status: ready-for-agent
Execution: claimed
Blocked by: 05

## 范围

按 [双轴审查](<../review.md>) 统一处理 S1/P1。写范围：internal/auth/auth.go 的私有复核逻辑、internal/web/static/app.js 的开发匿名 challenge 轮换判断、tests/browser/web-auth.test.mjs 的真实回归、必要的现有 HTTP 并发安全测试；以及 tests/browser/README.md、../validation.md、../review.md 的修复证据。不要修改公共认证接缝、生产配置语义或冻结测试集。

必须由同一全新上下文实施者完成，独立 `.worktrees/web-auth-06` / `tkt/web-auth-06`，基于最新 `feat/web-auth`；加载 tdd。先以真实 HTTPS 浏览器和可控时钟复现开发匿名 20 分钟 challenge 轮换后草稿丢失，再修复。仍须保留密码登录身份真正变化时的私人视图清理，不能自动重发原文。

认证重构只提取已持锁的私有复核 helper：密码 body IO 之后和 bcrypt 比较之后的两次检查都必须保留，检查顺序/撤销/过期/关闭行为不变；不是删除重复的安全检查。

## 验收

- [ ] 开发匿名 20 分钟 challenge 边界的草稿保留/用户主动提交回归先红后绿，既有浏览器测试不削弱。
- [ ] S1 重复逻辑合并且两次复核时机保留，现有慢 body/撤销/期限/限速 HTTP 测试通过。
- [ ] 最新集成 tip 已 merge；完整 `CHROME_NO_SANDBOX=1 CHROME_BIN=/bin/google-chrome flock /tmp/shiyi-web-auth-make-check.lock make check` 通过。
- [ ] `flock /tmp/shiyi-web-auth-image-check.lock make image-check IMAGE=shiyi:web-auth-review` 实际通过。
- [ ] 审查/验收记录写明实际修复与测试证据，不宣称远端 CI 或实际部署已验证。

## Comments

当前文件策略已是 danger-full-access，审批关闭，不请求升级。所有测试仍使用临时目录、隔离浏览器、测试凭据与模型替身。Lead 负责最终关闭票据和集成；实施者不得修改主树或提前清理工作树。
