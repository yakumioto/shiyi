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

由一位全新上下文实施者在独立工作树统一修复 S1/P1；见 [06](<issues/06-review-fixes.md>)。原始发现与严重性保留，实际整改及 Lead 补验结果见下文。

## 06 实际整改记录（2026-11-06，Asia/Shanghai）

本节追加整改证据，不改写上面独立 Standards / Spec 审查的原始结论、严重性或静态发现来源。“当前尚未修复”是上一节记录时的历史状态。06 由同一全新上下文实施者独自在指定工作树完成；未重新声称执行两位独立审查。

### Standards / S1（P3）整改

- 实现提交：`019b2066fe49085c3b3859d5b49dfc9076c513b5`。
- [auth.go](<../../internal/auth/auth.go>) 只提取私有 `revalidatePasswordRequestLocked`，调用者必须持有 `m.mu`；不增加公共接口或测试私有实现。
- 保留密码 body IO 后和 bcrypt 比较后重新加锁的两次调用。每次仍依次检查 closed / failed / stateSafe、重新解析登录会话及期限、logout-all 的有效登录要求、来源与 CSRF；分别保持 503 / 401 / 403 响应。body 错误处理、尝试额度和密码错误处理的相对位置不变。
- 这是可维护性整改，不把原 P3 smell 重新描述成已确认的鉴权漏洞；最终 HTTP/race、慢 body 撤销、期限及限速回归执行结果见下方门禁记录。

### Spec / P1（P2）整改

- 在修改应用前，扩展[真实开发匿名浏览器用例](<../../tests/browser/web-auth.test.mjs>)并连续两次实际复现：19:59 时模型失败后草稿保留；推进至 20:00、主动点击发送后，草稿实际为 `''`。两次均 exit 1，失败为 `challenge rotation must not discard an unsubmitted draft`，不是超时或环境失败。
- 红灯命令：`CHROME_NO_SANDBOX=1 CHROME_BIN=/bin/google-chrome node --test --test-name-pattern='development anonymous mode' tests/browser/web-auth.test.mjs`；两次耗时约 1.915s / 1.336s。只修改测试时复现，没有先改应用或人为破坏后端。
- [app.js](<../../internal/web/static/app.js>) 区分密码登录会话 token 变化与开发匿名 challenge 正常轮换；模式变化或密码身份变化仍递增视图 generation 并清屏/清草稿，开发匿名轮换只接纳新 CSRF token。没有延长 challenge 期限、改 Cookie 或绕过真实来源/CSRF。
- 相同命令修后 exit 0（1/1、0 skip，约 1.422s）。20:00 的用户主动提交确实到达模型；失败后原文空白及草稿保留，模型恢复不自动重发，下一次主动点击才成功。开发主动退出仍清草稿。
- 原 17 条浏览器用例和断言全部保留，另扩展原共享 Cookie 用例：另一标签通过真实 HTTPS 登录替换 Cookie、不广播通知，原页再次交互仍清旧私人视图/草稿且不提交旧草稿。该定向测试 exit 0（1/1、0 skip，约 1.718s）。
- 两项均无公共契约、冻结测试集或生产配置变更；没有顺带重构其他业务。验证环境保持隔离 Chrome、临时认证/记忆目录、fixture SPKI 例外、测试密码和模型替身；未验证远端 CI 或实际公网部署。

### Lead 独立补验与关闭结论

修复代理在提交 `019b206` 并写入上述红绿记录后异常结束，未留下最终工具报告；Lead 保留其未提交审查记录，未将报告缺失等同于通过。Lead 检查了完整修复 diff、确认集成 tip 为祖先，并在同一固定代码提交的工作树实际运行并收集：

```sh
CHROME_NO_SANDBOX=1 CHROME_BIN=/bin/google-chrome flock /tmp/shiyi-web-auth-make-check.lock make check
flock /tmp/shiyi-web-auth-image-check.lock make image-check IMAGE=shiyi:web-auth-review
```

两项均 exit 0。完整门禁包括 Go/race/冻结集、扩展后的原 17 条真实 HTTPS 浏览器（17 pass / 0 fail / 0 skip，包含开发 challenge 19:59/20:00 与真实密码身份轮换保护）、TTY/认证进程 smoke 和 270 项文档检查；镜像完整安全认证 smoke 通过，包括非 root/独立卷权限、重启/撤销/重哈希与秘密日志。

**Standards：S1 已整改，未解决 0 条；Spec：P1 已整改并由真实浏览器回归验证，未解决 0 条。** 此结论为原发现的修复核对及 Lead 实测，不冒充重新进行一次全量独立审查。
