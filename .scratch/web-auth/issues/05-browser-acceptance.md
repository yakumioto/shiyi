# 05: 真实 HTTPS 浏览器与端到端验收

Status: ready-for-agent
Execution: completed
Blocked by: 03, 04

## 范围

按 ../spec.md Testing Decisions 全面核对，重点 §10 真实浏览器。继承 03 在 tests/browser/ 交付的最小真实 HTTPS 浏览器夹具并扩展，不重复另造一套。写范围 tests/browser/、浏览器测试工具/测试服务、scripts/、必要依赖清单与锁文件、Makefile/.github/workflows/ci.yml 的浏览器验证入口、README.md/docs/deployment.md 的验证前提、docs/check-readme.sh 的浏览器入口一致性，以及 ../validation.md、../acceptance-map.md；应用实现缺陷报 Lead，不直接越界修补。不得以源代码字符串检查替代浏览器测试，不用真实模型与真实凭据。

使用测试 HTTPS + 真实浏览器；测试 Cookie 实际接收/发送、多设备、记录查询、认证过期保留草稿且重登不重放、模型错误不保存、主动退出清屏、多标签页及后退/恢复失效处理、刷新无历史。提供可重复命令并纳入明确 CI/本地验证流程；避免不可靠 sleep。

## 验收

- [x] 实际运行真实 HTTPS 浏览器流程并记录环境前提与结果。
- [x] HTTP/服务/容器/浏览器的验收覆盖矩阵完整，无未执行标绿。
- [x] `make check`、浏览器命令及 `make image-check` 最终执行并记录。
- [x] 合并最新集成 tip，无冻结接缝漂移。

## Comments

已完成。实现提交 `9e85c52`，候验及证据提交 `1f62f13`，集成提交 `f0226ca`，分支 `feat/web-auth`。原 15 条真实 HTTPS 浏览器回归保留，新增同一临时 profile 真实关闭/重开 Chrome、同认证目录/哈希重构服务后保留登录两条，均验证不恢复私人历史或自动重放；17/17 通过、0 skip。`make browser-check` 已接入唯一 `make check` 与 CI，缺工具明确失败，文档检查 270 项通过。

实施者初验的完整门禁因 PTY 沙箱权限失败，未虚报通过。Lead 对固定候验 HEAD 在原工作树获准原样补跑完整 `make check` 和 `make image-check IMAGE=shiyi:web-auth-05`，均 exit 0；合入后的主树再次运行完整 `make check`，ALL GREEN，包括浏览器、TTY、认证进程、Go/race/冻结集与文档。容器验收包含默认安全认证、非 root、独立状态卷权限、存活、重启/撤销/重哈希与秘密日志。

完整测试名与 Testing Decisions 1–13 的证据见 [验收记录](<../validation.md>) 和 [验收追踪](<../acceptance-map.md>)。未推送，远端 CI 未运行；未操作或声称验证实际公网部署。没有应用实现、冻结接口或冻结 fixtures 改动。