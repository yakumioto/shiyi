# Web 认证验收记录（05）

## 最终补验结论（Lead，2026-11-06）

**本地完整验收已通过。** Lead 在固定候验提交 `1f62f130df6d054167c0ec7946a3944e3b789c96` 的原工作树，按工具规则获准对下文 A/C 原命令作单次更宽权限重试；未改变设备权限、跳过测试或启用匿名模式。之后以 `f0226ca3518fcb2e1028a9e028cc85880d74563a` 合入 `feat/web-auth`，结果树与该候验提交完全相同，并在主树再次完整执行 A。

| 最终执行 | 实际结果 |
| --- | --- |
| A：票工作树完整 `make check` | exit 0，ALL GREEN；含全部 Go/race/冻结集、真实 HTTPS 浏览器 17/17（0 skip）、真实 TTY 与认证进程 smoke、270 项文档检查 |
| C：`make image-check IMAGE=shiyi:web-auth-05`（独占锁） | exit 0；真实 TTY、TLS 登录、Cookie、非 root、独立认证卷与 0700/0600、存活、重启/撤销、全部退出、同密码重哈希失效、秘密日志全部通过 |
| A：集成提交 `f0226ca` 主树复验 | exit 0，ALL GREEN；17 条浏览器、TTY/进程与文档检查再次通过 |

Testing Decisions 1–13 的本地要求均已有实际执行证据；真实 OS 进程和容器验收不再待补。浏览器的 `CHROME_NO_SANDBOX=1` 仅为隔离测试环境选项，不是生产建议。用户随后将本会话改为 danger-full-access，主树复验不再受先前 PTY 限制。

**未推送、远端 CI 未运行；真实公网代理、证书、防火墙、模型供应商与备份/宿主加密仍属部署者责任，未操作或验证。**

下文保留实施者初验记录以追踪环境失败。其“未通过/待 Lead 补验”描述均为历史状态，已由本节补验关闭；远端 CI 和实际部署的未验证边界仍有效。

## 初验结论与验证版本（历史记录）

- 日期：2026-11-06（Asia/Shanghai）。工作树 `web-auth-05`，分支 `tkt/web-auth-05`；开工时 HEAD / `feat/web-auth` 均为 `807844b11771ab7b30fdb84953484c9a3a2aa437`，工作树干净。
- 最终门禁前已执行 `git merge --no-edit feat/web-auth`，结果 Already up to date，集成 tip 仍为 `807844b`。本票实现及测试快照为 `9e85c52db04067c9f792f2490a7a6a65f3f88fa5`；本记录与[验收追踪](<acceptance-map.md>)的后续提交只补证据，不改变该已执行实现。
- **浏览器验收 17/17 通过、0 skip；完整 `make check` 和 `make image-check` 本票均实际运行但未通过**。两者都在真实 TTY 冒烟的 `os.openpty()` 被 workspace-write 沙箱拒绝，exit 2。不能用浏览器/Go 子阶段通过、04 的历史通过或镜像构建成功冒充本票完整门禁通过。
- 没有修改应用实现、生产路由、冻结接口、vendor 或冻结测试集；未启动替代 GUI，未接触真实部署、用户 profile、真实模型或真实凭据。没有发现需越界修补的应用缺陷。
- CI 已接入，**未推送、远端 CI 未运行**；没有验证用户实际公网部署。

## 环境与可重复命令

实测 Linux/amd64、Go 1.26.8、Node 24.22.2、Google Chrome 153.0.8010.47（`CHROME_BIN=/bin/google-chrome`）；Go/ripgrep/Python 3/OpenSSL 可用，Docker 构建及镜像内 ripgrep 14.1.1 命令实际成功。浏览器无需 npm 包。

本环境必须显式使用 `CHROME_NO_SANDBOX=1`；它仅向本次隔离测试 Chrome 添加 `--no-sandbox`，**不是生产配置**。每例使用新临时根目录、认证/记忆目录、profile、HOME/XDG 和测试密码；正常密码模式通过真实 HTTPS 登录，只有单独的开发模式用例显式匿名。证书例外限定夹具公钥 SPKI，不全局忽略 TLS 错误。持久 profile 使用测试专用 `--password-store=basic`，避免系统密钥环访问，不保存真实凭据；结束后清理自建临时目录。

所有命令在本票工作树根目录执行：

```sh
# A：唯一的完整本地门禁
CHROME_NO_SANDBOX=1 CHROME_BIN=/bin/google-chrome flock /tmp/shiyi-web-auth-make-check.lock make check

# B：用户要求的直接浏览器入口
CHROME_NO_SANDBOX=1 CHROME_BIN=/bin/google-chrome node --test tests/browser/web-auth.test.mjs

# C：独占镜像验证（与 A/B 不并发）
flock /tmp/shiyi-web-auth-image-check.lock make image-check IMAGE=shiyi:web-auth-05

# D：文档一致性；仅定位，不能代替 A
bash docs/check-readme.sh

# E：新增的浏览器子目标；执行与 B 相同的测试，不是另一套设施
CHROME_NO_SANDBOX=1 CHROME_BIN=/bin/google-chrome make browser-check
```

| 命令 | 本票实际结果 | 解释 |
| --- | --- | --- |
| A | **exit 2，未通过** | prereqs、vendor 一致性、vet、全包 build、全部 Go race 测试、冻结集、17 条浏览器测试均通过；随后 TTY smoke 失败，未到该次 `readme-check` |
| B | **exit 0，17 pass / 0 fail / 0 skip** | 最终直接运行约 24.8 秒，全部真实 HTTPS 用例执行 |
| C | **exit 2，未通过** | 镜像构建成功、镜像内 `rg --version` 成功；容器 smoke 的宿主 TTY 初始化失败，未进入认证容器启停/探活用例 |
| D | **exit 0，270 项通过** | 独立实际运行；不是把 A 中未到达的步骤算作已执行 |
| E | **exit 0，17 pass / 0 fail / 0 skip** | 接入后完整跑过；A 又执行一次相同用例 |

缺工具负向检查也实际执行：[前置脚本](<../../scripts/browser-prereqs.sh>)在 `CHROME_BIN=/nonexistent/browser-test-chrome` 时 exit 1 并明确要求 Chrome，在 `PATH=/nonexistent /bin/bash scripts/browser-prereqs.sh` 时 exit 1 并明确要求 Node。Node 主版本固定为 24；缺工具不是 skip 或允许跳过浏览器门禁的开关。

### 环境阻塞与交接

A、C 的相同失败现场：

```text
File "<stdin>", line 116, in hash_password
PermissionError: [Errno 13] Permission denied
[sandbox: file access denied under workspace-write mode]
```

对应[真实终端入口](<../../scripts/smoke.sh#L119-L123>)的 `os.openpty()`；已知受限设备为 `/dev/ptmx`。没有改设备权限、换设备、改 smoke、skip 或更改冻结数据来救绿。子会话审批禁用，不能申请扩大权限，也没有重试被拒绝的操作。尝试 `send_message` 通知 `lead` / `parent` 返回目标不可用，需由上层转交。

Lead 应在最终候验 HEAD 上，以该工作树为 `workdir`，实际执行上面的 **A 原命令**和 **C 原命令**（C 独占）。只有 Lead 能在正常命令被策略拒绝后，依据工具规则取得针对完全相同命令的单次审批；审批不继承。本记录未把尚待补验的部分标绿。镜像构建另有 Docker 的 `SecretsUsedInArgOrEnv` 提示，指向的是认证目录变量而非嵌入凭据；构建本身成功，不是此次退出原因。

## 小纵向红绿与接缝

沿用 03 的[原生 CDP 驱动](<../../tests/browser/driver.mjs>)和[真实 Go 夹具](<../../tests/browser/fixture/main.go>)，没有另造框架。真实组件为 auth/httpapi/web/classify/answer/store/search；只替身外部 `llm.Client` 与认证时钟。控制协议仅 stdin/stdout，无额外 HTTP 路由。

1. 新增“关闭重开”用例，先观察默认持久 context 首次导航超时；对照既有无痕 context 登录通过。单独增加测试 `--password-store=basic` 后，导航成功并准确红在缺少 `restartBrowser`。补入真实 `Browser.close`、等待旧进程退出、同 profile 新进程后，通过 Cookie 保留、空私人视图和真实业务提交断言。新页面有公开空态提示，因此断言没有私人条目/标记，而非要求整个容器文本为空。
2. 新增“服务/auth 重构”用例，先红在未实现 `restart-service`；然后在既有夹具按公开 Close/New 顺序停止 HTTPS、释放认证锁、同目录/哈希重建真实组件并恢复原来源，两条生命周期用例一起通过。
3. `make browser-check` 接入前实跑 exit 2（无该目标），接入后 E 全部通过；文档检查先实际报 13/270 缺失，再同步三份说明后 D 为 270/270。

生命周期测试不预塞/导入 Cookie，不读取 Chrome 或 auth 私有序列化。浏览器重开使用同一个临时默认持久 profile（不是无痕 context），验证旧进程正常 exit 0、新 PID、同一 Cookie 由 Chrome 留存；重新打开页面后不再登录。服务重构使用同一个临时认证目录与完全相同哈希；不是重新生成相同密码的另一哈希。两者先展示原文、查询回答、引用并填写草稿，然后验证新页面没有恢复历史/草稿、没有自动重放，用户主动新提交仍真实获准。此服务重构不冒充 OS 进程重启；后者由现有 smoke 承担，受上述环境阻塞。

## 浏览器真实测试名

以下名称均来自[同一测试文件](<../../tests/browser/web-auth.test.mjs>)，B 全部执行，原有 15 条未删减；B02/B03 为本票补充。

| 编号 | 测试名 |
| --- | --- |
| B01 | `HTTPS login receives a protected Cookie and submits original text` |
| B02 | `closing and reopening Chrome preserves the valid Cookie but not private history` |
| B03 | `service and auth reconstruction retain browser login without restoring private history` |
| B04 | `expired login keeps only an in-memory draft and never replays it` |
| B05 | `model failure preserves draft; logout-all requires password and works with model down` |
| B06 | `a delayed business response cannot restore the private view after logout and relogin` |
| B07 | `same-Cookie tabs clear private DOM and draft when another tab logs out` |
| B08 | `actual back/forward cache restoration checks auth and never restores a private snapshot` |
| B09 | `password rate limit is distinct from expiry and recovers with controlled time` |
| B10 | `network failure does not claim logout success, and auth outage is not model failure` |
| B11 | `development anonymous mode is explicit and offers no password or logout-all` |
| B12 | `original text, model answer and real citation render as text without browser storage` |
| B13 | `password form native fallback never puts the password in a URL or body` |
| B14 | `an old authenticated session check cannot undo a newer cross-tab logout` |
| B15 | `another device can revoke an admitted request before its response is displayed` |
| B16 | `real CSRF rejection is distinct from model failure and never retries the original text` |
| B17 | `current logout clears private view and draft without logging out another device` |

## Testing Decisions 1–13 对照

对应[规格 Testing Decisions](<spec.md#L144-L174>)。A 的 Go/race 子阶段实际运行全部 Go 测试；下列不是只靠阅读测试名字推定通过。B 编号对应上表完整真实测试名。HTTP/Go 和浏览器通过不等于所有进程/容器步骤通过。

| 项 | 测试证据与可观察结果 | 本票执行状态 |
| --- | --- | --- |
| 1 未登录/错误/空密码/伪造与旧 Bearer，公开页面和 healthz | [HTTP 授权](<../../internal/httpapi/auth_test.go>) `TestPasswordLoginProtectsBusinessBeforeParsing`；[安全测试](<../../internal/httpapi/auth_security_test.go>) `TestForgedAndDuplicateCookiesNeverAuthorize`；[HTTP 回归](<../../internal/httpapi/server_test.go>) `TestBearerNeverAuthorizes`、`TestHealthzReportsOK`；[Web 挂载](<../../internal/httpapi/web_test.go>) `TestWebIsMountedAtTheRoot`；B01/B05 真实公开登录 UI | A 的 Go 子阶段、B 通过；进程 smoke 本票未到达 |
| 2 Cookie、原文记录与查询引用，查询/回答不持久化 | [真实记忆链路](<../../internal/httpapi/auth_memory_test.go>) `TestPasswordLoginRecordsAndQueriesRealMemoryWithoutPersistingConversation`；[业务验收](<../../internal/httpapi/acceptance_test.go>) `TestAcceptanceQueryLeavesNoTrace`、`TestAcceptanceRecordRowHasExactlyThreeFields`；B01/B12 检查实际安全 Cookie、原文与真实搜索引用；B02/B03 不恢复私人历史 | A 的 Go 子阶段、B 通过；不以浏览器 DOM 独自证明磁盘无写入 |
| 3 独立设备/当前退出/全部退出再验证/旧 Cookie 拒绝 | [HTTP 生命周期](<../../internal/httpapi/auth_test.go>) `TestSessionsAndRevocationsSurviveRestart`；[复制凭据重放](<../../internal/httpapi/auth_lifecycle_test.go>) `TestReloginRotatesCredentialsAndRejectsCopiedRevokedCookie`；B05/B17 用独立 context，B07 用同 context 共享 Cookie | A 的 Go 子阶段、B 通过；进程/容器复制撤销 Cookie 重放已有 smoke，本票受 TTY 阻塞 |
| 4 闲置 7 天/绝对 30 天、有效使用与轮询区别 | [时钟生命周期](<../../internal/httpapi/auth_lifecycle_test.go>) `TestIdleDeadlineIsNotExtendedByPollingOrRejectedRequests`、`TestAdmittedBusinessAttemptRefreshesDurablyBeforeDecoding`、`TestActiveSessionStillExpiresAtThirtyDays`；精确边界前 1ns/到期、允许业务失败也持久续闲置、绝对期限不变；B04 推进时钟验证到期草稿 | A 的 Go 子阶段、B 通过；无长时间 sleep |
| 5 同目录重构保有效/注销不复活/换哈希/损坏丢失 fail closed | [HTTP 生命周期](<../../internal/httpapi/auth_test.go>) `TestSessionsAndRevocationsSurviveRestart`；[持久认证边界](<../../internal/httpapi/auth_lifecycle_test.go>) `TestReloginRotatesCredentialsAndRejectsCopiedRevokedCookie`、`TestChangedHashAndLostStateRequireFreshLogin`、`TestStatePermissionsCorruptionAndSingleWriterFailClosed`、`TestUnavailableDurableStateCannotAuthorizeOrReportLogoutSuccess`；B02/B03 补真实浏览器持久 Cookie/服务重构 | A 的 Go 子阶段、B 通过；实际服务进程/容器重启仍待 Lead 在本票 HEAD 补验 |
| 6 跨站登录/提交/注销、Origin/CSRF/JSON/转发欺骗 | [HTTP 安全矩阵](<../../internal/httpapi/auth_security_test.go>) `TestEveryMutationRequiresOriginBoundCSRFAndJSON`、`TestBootstrapIsBoundReusableAndExpiresWithoutGrantingAccess`、`TestPasswordEndpointsBoundAndValidateJSONBeforeAttempts`；B16 修改真实请求 token 得到真实 403、不进模型/不重放，同源新提交成功 | A 的 Go 子阶段、B 通过 |
| 7 密码尝试限速可触发/恢复、非永久锁定 | [限速窗口](<../../internal/httpapi/auth_test.go>) `TestPasswordAttemptsShareRecoverableGlobalWindow` 同时覆盖 login/logout-all、伪造 X-Forwarded-For 不能拆额度、Retry-After 与边界恢复；B09 验证 UI 区分及受控时间恢复 | A 的 Go 子阶段、B 通过 |
| 8 模型持续失败不影响认证/注销，未分类原文不保存 | [安全测试](<../../internal/httpapi/auth_security_test.go>) `TestAuthenticationWorksDuringModelFailureAndNeverLogsSecrets`；[业务验收](<../../internal/httpapi/acceptance_test.go>) `TestAcceptanceModelUnavailableWritesNothing`；B05 模型持续报错仍可登录/密码再验证/全部退出且保草稿；B10 区分认证故障与模型故障 | A 的 Go 子阶段、B 通过；真实进程模型故障路径在 smoke 中，本票未到达 |
| 9 错误/日志不含测试密码、凭据、模型 key、原文 | [HTTP 标记测试](<../../internal/httpapi/auth_security_test.go>) `TestAuthenticationWorksDuringModelFailureAndNeverLogsSecrets`；[错误映射](<../../internal/httpapi/server_test.go>) `TestErrorMessagesLeakNothing`；[模型适配器](<../../internal/llm/client_test.go>) `TestHTTPClientNeverLeaksKeyOrProviderBody`、`TestHTTPClientLogsStatusWithoutKeyOrBody`；[服务日志](<../../cmd/shiyi/serve_test.go>) `TestRunServeLogHasNoSecret`；B13 密码表单不进入 URL/body | A 的 Go 子阶段、B 通过；独立进程/容器日志扫描本票未到达 |
| 10 真 HTTPS 浏览器 Cookie/UI/草稿/重登/退出/返回恢复 | B01–B17。B08 必须观察真实 `BackForwardCacheRestore` 与 `pageshow.persisted`；B06/B14/B15 延迟已真实生成的响应验证竞态；B12 真实原文/回答/引用 XSS 文本化；B02/B03 不恢复历史 | B/E 与 A 的 browser-check 子阶段均 17/17，通过且 0 skip；没有以源码字符串替代 |
| 11 缺失/无效配置、旧 Bearer、显式开发模式 | [配置验收](<../../internal/config/auth_test.go>) `TestAuthConfigurationFailsClosedAndRequiresExplicitOrigin`、`TestAuthEnvironmentOverridesEvenWhenEmpty`、`TestLegacyAuthSettingsAlwaysRequireMigration`、`TestInvalidConfigNeverEchoesCredentialInput`；[构造验证](<../../internal/httpapi/auth_lifecycle_test.go>) `TestAuthConstructorRejectsBadHashesAndOrigins`；[开发 CSRF](<../../internal/httpapi/auth_security_test.go>) `TestDevelopmentAnonymousStillRequiresBrowserCSRF`；B11 明确开发 UI | A 的 Go 子阶段、B 通过；进程/容器安全配置拒绝路径本票未到达 |
| 12 服务启动停止/容器探活/旧 CLI 拒绝/Web HTTP 保留 | [真实 serve 生命周期](<../../cmd/shiyi/serve_auth_test.go>) `TestRealServeKeepsAuthenticationAvailableAndReleasesStateAfterShutdown`、`TestFailedHTTPAssemblyClosesAuthenticationModule`；[serve 回归](<../../cmd/shiyi/serve_test.go>) `TestRunServeStartsAndStops`；[管理入口](<../../cmd/shiyi/management_test.go>) `TestRemovedMemoryClientFailsWithoutEchoingInput`、`TestHashPasswordRejectsArgumentsWithoutEchoingSecrets`、`TestHashPasswordRejectsNonTerminalWithoutReadingServeConfig`；[真实进程/容器 smoke](<../../scripts/smoke.sh>) 覆盖 TTY、安全启动、HTTPS、重启/撤销及容器非 root/独立卷/探活 | **仅 Go 子阶段通过；A/C 均在 TTY 被阻断，独立进程与容器完整验收未完成，不能标绿** |
| 13 文档入口/迁移/边界，保留原文/引用/冻结集/不存查询回答保护 | [README](<../../README.md>)、[部署](<../../docs/deployment.md>)、[浏览器说明](<../../tests/browser/README.md>)；[文档检查](<../../docs/check-readme.sh>) 270 项；[冻结集测试](<../../internal/frozen/frozen_test.go>) `TestFrozenManifestsAreIntact`、`TestFrozenClassification`、`TestFrozenQueries`；A 原有业务测试仍执行，冻结数据无修改 | D 通过，A 的冻结集子阶段通过；[CI 配置](<../../.github/workflows/ci.yml>)仅接入未远端运行 |

## 尚待补验，不纳入已通过

1. Lead 对最终候验提交实际跑完 A 的真实 TTY/进程 smoke 和末尾文档检查、C 的完整容器 smoke，并记录获批命令与结果。
2. 推送后由真实 CI 环境执行 Node 24/Chrome 安装、完整 `make check` 与容器 job；当前不能推定远端结果。
3. 实际公网代理、证书、网络绕过防护、模型供应商、备份和宿主加密属于部署者责任，本票未操作或宣称验证。
