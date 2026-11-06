# 04: 移除记忆 CLI 并迁移服务部署与文档

Status: ready-for-agent
Execution: completed
Blocked by: 02

## 范围

按 ../spec.md CLI/部署/隐私要求与 ../seams.md。写范围 cmd/shiyi/、scripts/smoke.sh、Dockerfile、Makefile、.github/workflows/ci.yml、README.md、config.example.toml、docs/deployment.md、docs/config.docker.toml、docs/check-readme.sh；批准为安全 TTY 哈希管理新增固定版本 golang.org/x/term 及必要 x/sys 的 go.mod/go.sum/vendor 最小改动。不能改 internal/web、tests/browser 或认证接缝。与 03 并行。旧 smoke 固定端口期间使用 `flock /tmp/shiyi-web-auth-make-check.lock make check` 串行门禁；本票迁移 smoke 时改成动态端口与真实就绪检查。

移除 send/repl、客户端地址/token/格式化及专属测试，保留 serve 配置校验/信号退出及 HTTP 能力；旧调用明确失败。提供安全交互式哈希生成管理入口（不是记忆客户端），不在参数/日志暴露明文。默认安全的配置/镜像启动与探活，认证状态卷/权限；更新真实认证冒烟，make image-check 验证安全模式。文档包括迁移、HTTPS 固定来源/反向代理防绕过责任、哈希生成/恢复和同密码重哈希撤销、7/30 天期限、JSONL 明文/磁盘及备份加密、服务器与模型信任、模型失败不存原文、限制/后续方向。不得说部署已实际配置，不修改用户反向代理。

## 验收

- [x] 旧客户端与专属测试移除，serve 生命周期保留、旧命令失败验证。
- [x] 可复制哈希生成与密码恢复指引；日志无秘密。
- [x] 文档检查保留原文/引用/冻结集/不保存查询回答，去除旧 CLI 强制断言。
- [x] `make check` 和 `make image-check` 实际执行并报告；容器默认不靠匿名模式过关。
- [x] 合并最新集成 tip，无冻结接缝漂移。

## Comments

已完成。实现提交 `4af1a56`、`a28674f`、`4c01228`、`f0e2016`、`959f550`；票 tip `959f550` 包含当时最新集成 `b371d1a`，集成提交 `e7cf539`，分支 `feat/web-auth`。

Lead 在固定票工作树实际执行 `flock /tmp/shiyi-web-auth-make-check.lock make check` 与 `make image-check IMAGE=shiyi:web-auth-04-retry`，均 exit 0；合入后再次运行主树 `make check`，ALL GREEN。包含真实 TTY 无回显/双确认/信号恢复、旧 CLI 拒绝、HTTPS Cookie 登录、原文/引用/不落查询、非 root 容器与独立认证卷 0700/文件 0600、存活检查、正常重启/撤销持久化/全部退出/同密码重哈希失效/秘密日志，文档检查 240 项通过。

普通 workspace-write 沙箱因 /dev/ptmx 返回 EACCES，曾使完整门禁失败；经用户获准的同一命令单次更宽权限重试才完成真实验收，没有跳过测试、改变设备权限或以匿名模式救绿。Docker 的 SecretsUsedInArgOrEnv 提示命中 SHIYI_AUTH_STATE_DIR，但该值只有 /auth 路径，镜像未烘焙哈希。无冻结接缝/记忆 fixtures 改动，无真实部署操作。

历史：原实施者长期无代码改动，已停止；原 `.worktrees/web-auth-04` / `tkt/web-auth-04` 明确放弃，保留到最终清理。替代工作树为 `.worktrees/web-auth-04-retry`，所有实际交付来自该树。
