# Web 认证实施图

集成分支：`feat/web-auth`；起点：`660df6d`。规格：[spec.md](spec.md)。用户于 2026-11-05 授权补齐任务并继续实施。

## 图与完成约定

`01 → 02 → {03, 04} → 05`。01 是串行接缝准备，合并且 `make check` 通过之前不并行实施。每票独立 `.worktrees/web-auth-NN`、分支 `tkt/web-auth-NN`，基于集成分支最新 tip；报告前 merge 集成分支。实施者必须加载 `tdd`，用 HTTP 行为测试为主，不能修改冻结测试集。

保持 `Status: ready-for-agent` 分诊标签；采用仓库既有关闭方式：完成验收复选框，并在 Comments 写“已完成”、集成提交与实际验证记录。另用 `Execution: pending/claimed/completed` 明确执行状态；只有依赖完成且合并门禁通过才解除阻塞。集成者负责最终关闭，不由实施者提前宣称整票完成。

## 接缝与写范围

01 确定并提交认证配置类型、认证深模块构造器/公共契约、HTTP 路由/线格式/错误映射和接缝清单；不启用未实现的认证。02 实现 config/auth/httpapi 和必要的现有测试/冒烟适配；03 仅 Web 静态资源与 Web 测试；04 仅 cmd、部署脚本、文档、示例、Docker/Makefile/CI；05 浏览器测试设施及验证流程。并行阶段 03/04 不改冻结接缝，确需改动先报告 Lead，停止相关实施再统一调整。

每票先阅读仓库 AGENTS 与 docs/agents/{issue-tracker,triage-labels,domain,parallel-implementation}.md、GLOSSARY 及相关 ADR。用 read/edit/write 改文件；不能覆盖别人未提交工作。所有门禁用 `make check`，不能另手写 go test/go vet 命令代替。依赖/vendor 更新需提交后再跑 vendor consistency。任何测试未运行必须据实说明。

## 安全设计待 01 冻结

成熟自适应密码哈希；不透明随机 Cookie + 服务端持久化撤销；独立认证目录与严格权限；配置明确 HTTPS 公共来源；Origin + CSRF 保护包括登录；状态检查不推进闲置期限；有效业务操作才 touch；有界可恢复限速；代理转发头不可信；故障不匿名回退。

所有票合并后执行 `code-review`，单独新实施者修复审查发现，再关闭规格与清理已合并工作树。不操作真实部署，不推送远端，不自动创建 PR。
