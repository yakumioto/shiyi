# Web 认证双轴代码审查

用户确认的固定点：`660df6d2579586e0d74d0867fdc7411a4a24d707`。审查 HEAD：`0323cc1cfa844e242c542cfaa3355ad320f32c05`。命令：`git diff 660df6d...HEAD`，diff 非空；两位全新上下文只读代理并行审查。两轴独立保留，不合并排序。

## Standards

共 1 条：硬性规范违反 0 条；判断性 smell 1 条，严重性 P3（低）。

### S1：可能的 Duplicated Code——重新加锁后的认证复核整段重复

[auth.go:313–325](<../../internal/auth/auth.go#L313-L325>) 与 [auth.go:368–380](<../../internal/auth/auth.go#L368-L380>) 重复相同检查及错误响应：

```go
if m.closed || m.failed || !m.stateSafe()
id, s, logged = m.session(r)
if r.URL.Path == api.PathAuthLogoutAll && !logged
if !m.sourceOK(r, true) || !m.csrfOK(r, s, logged)
```

两次复核分别位于读取密码 body、bcrypt 比较之后，检查时机都应保留；问题只是同一安全规则维护两份，后续修改可能遗漏。最小建议：提取要求已持锁的私有复核函数，保留两个调用点和原有顺序，不扩展公共接缝。此项为可维护性判断，不是已证实的鉴权漏洞或硬性违规。

审查已按根 AGENTS、工程约定、领域文档与接缝清单，第三方规则只用于各自 vendor 范围；没有将生成的上游代码风格计为项目问题。审查者未重跑门禁。

## Spec

发现 1 项：P2 实现错误；独立缺失项 0，范围蔓延 0。

### P1：开发匿名模式的 CSRF 正常轮换会丢失未提交原文

规格要求：“未成功提交的原文仅在当前页面内存中保留，包括登录过期及模型失败”（[spec.md:116](<spec.md#L116>)）。

[app.js:124–128](<../../internal/web/static/app.js#L124-L128>) 将任何已认证响应的 CSRF token 变化视为身份切换，调用 `clearPrivate(false)` 清草稿；但开发匿名模式 bootstrap 到期时会更新 token，仍返回 `authenticated:true`（[auth.go:260–272](<../../internal/auth/auth.go#L260-L272>)）。开发页填草稿，20 分钟后发送时，前置检查会清空原文并取消提交，虽然使用者没有退出或切换身份。

此发现来自静态路径核对，审查者未实跑该场景。最小建议：区分密码登录会话变化和开发模式 challenge 轮换；后者保留草稿。以可控时间扩展[真实开发模式浏览器验收](<../../tests/browser/web-auth.test.mjs#L235-L247>)覆盖 20 分钟边界。

真实授权、持久撤销、来源/CSRF、限速、CLI/配置/容器及隐私边界未发现其他规格偏离。未将远端 CI 或实际部署未执行列为缺陷。

## 汇总与整改

Standards：1 条，最高 P3；Spec：1 条，最高 P2。

由一位全新上下文实施者在独立工作树统一修复 S1/P1；见 [06](<issues/06-review-fixes.md>)。当前尚未修复，不能将此审查记录视为通过结论。
