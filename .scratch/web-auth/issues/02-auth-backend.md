# 02: 安全登录、持久化会话与 HTTP 授权

Status: ready-for-agent
Execution: completed
Blocked by: 01

## 范围

按 ../spec.md 全部后端要求和 ../seams.md 实现。写范围：internal/auth、internal/config、internal/httpapi、internal/api、go.mod/go.sum/vendor；同时负责 cmd/shiyi/serve.go、serve_service.go 的最小真实 auth.New 组装与 Shutdown 后资源释放；必要时最小适配现有 cmd 测试、internal/frozen 的 HTTP 测试组装与 scripts/smoke.sh 的显式开发模式/CSRF，使本票独立门禁通过（不能提前移除 CLI；不得修改 testdata 冻结 fixtures/manifest）。不要改 internal/web。

正常默认 fail closed；缺失/空白/无效哈希及旧配置拒绝；旧默认 Bearer 环境变量也识别。成熟哈希，不透明凭据，服务端可撤销，7 天闲置/30 天绝对期限，状态检查不 touch，同配置重启保持、换哈希撤销、丢失重登/损坏拒绝、原子持久化并限制权限。登录和再次验证限速有界可恢复；Origin+CSRF 覆盖所有状态变更，JSON 不能绕过；不信任转发头；业务授权前不解析/调用模型/写入；认证不依赖模型；no-store 且日志无秘密。

## 验收

- [x] HTTP 行为覆盖规格 Testing Decisions 1–9、11；可控时钟，无长时间等待。
- [x] 真实 Store 的记录/查询引用、认证目录分离和不持久化查询/回答得到覆盖。
- [x] 双浏览器 jar、当前退出/全部退出、期限边界、重启/损坏/换哈希、CSRF/来源伪造、限速恢复及秘密日志均验证。
- [x] 旧鉴权不再授权，正常默认不匿名；开发匿名必须显式配置。
- [x] `make check` 通过，合并最新集成 tip，无冻结接缝漂移。

## Comments

已完成。集成分支 `feat/web-auth`，集成提交 `6b0d3d9`；实现提交 `3b3d017`、`74dfb4a`、`88c6ca5`、`51faf6e`、`310f352`，票 tip `5a9f126` 已含当时最新集成 `63ff33a`。合并后 `make check` exit 0：vendor/vet/build/race/frozen、进程 smoke、185 项文档检查全部通过，工作树干净。

- 采用固定并 vendor 的 `golang.org/x/crypto` v0.58.0 bcrypt；认证状态原子替换 + file/dir fsync，0700/0600、单写者锁、凭据摘要和有界会话集合。
- 19 个新增 HTTP 认证用例、5 个配置用例、2 个真实服务组装/生命周期用例；包含密码登录接真实 store/search 的记录→引用→查询不落盘。
- 慢速密码 body 曾造成全局认证锁阻塞；已以真实 HTTP 并发回归先观察失败，再将 body IO 与 bcrypt 比较移出锁，重新进锁复核资格；会话检查和注销可正常工作，延迟请求不能复活旧资格。
- 冻结 fixtures/manifest、Web 和接缝契约未改；CLI/示例/部署文档迁移仍归 04。未执行新功能真实浏览器或 `make image-check`，不以现有 HTTP/进程门禁代替后续验收。
