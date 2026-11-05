# 05: 真实 HTTPS 浏览器与端到端验收

Status: ready-for-agent
Execution: pending
Blocked by: 03, 04

## 范围

按 ../spec.md Testing Decisions 全面核对，重点 §10 真实浏览器。继承 03 在 tests/browser/ 交付的最小真实 HTTPS 浏览器夹具并扩展，不重复另造一套。写范围 tests/browser/、浏览器测试工具/测试服务、scripts/、必要依赖清单与锁文件、Makefile/.github/workflows/ci.yml 的浏览器验证入口、docs/deployment.md 的验证前提以及 ../validation.md；应用实现缺陷报 Lead，不直接越界修补。不得以源代码字符串检查替代浏览器测试，不用真实模型与真实凭据。

使用测试 HTTPS + 真实浏览器；测试 Cookie 实际接收/发送、多设备、记录查询、认证过期保留草稿且重登不重放、模型错误不保存、主动退出清屏、多标签页及后退/恢复失效处理、刷新无历史。提供可重复命令并纳入明确 CI/本地验证流程；避免不可靠 sleep。

## 验收

- [ ] 实际运行真实 HTTPS 浏览器流程并记录环境前提与结果。
- [ ] HTTP/服务/容器/浏览器的验收覆盖矩阵完整，无未执行标绿。
- [ ] `make check`、浏览器命令及 `make image-check` 最终执行并记录。
- [ ] 合并最新集成 tip，无冻结接缝漂移。

## Comments

待实施。
