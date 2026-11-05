# 04: 移除记忆 CLI 并迁移服务部署与文档

Status: ready-for-agent
Execution: claimed
Blocked by: 02

## 范围

按 ../spec.md CLI/部署/隐私要求与 ../seams.md。写范围 cmd/shiyi/、scripts/smoke.sh、Dockerfile、Makefile、.github/workflows/ci.yml、README.md、config.example.toml、docs/deployment.md、docs/config.docker.toml、docs/check-readme.sh；批准为安全 TTY 哈希管理新增固定版本 golang.org/x/term 及必要 x/sys 的 go.mod/go.sum/vendor 最小改动。不能改 internal/web、tests/browser 或认证接缝。与 03 并行。旧 smoke 固定端口期间使用 `flock /tmp/shiyi-web-auth-make-check.lock make check` 串行门禁；本票迁移 smoke 时改成动态端口与真实就绪检查。

移除 send/repl、客户端地址/token/格式化及专属测试，保留 serve 配置校验/信号退出及 HTTP 能力；旧调用明确失败。提供安全交互式哈希生成管理入口（不是记忆客户端），不在参数/日志暴露明文。默认安全的配置/镜像启动与探活，认证状态卷/权限；更新真实认证冒烟，make image-check 验证安全模式。文档包括迁移、HTTPS 固定来源/反向代理防绕过责任、哈希生成/恢复和同密码重哈希撤销、7/30 天期限、JSONL 明文/磁盘及备份加密、服务器与模型信任、模型失败不存原文、限制/后续方向。不得说部署已实际配置，不修改用户反向代理。

## 验收

- [ ] 旧客户端与专属测试移除，serve 生命周期保留、旧命令失败验证。
- [ ] 可复制哈希生成与密码恢复指引；日志无秘密。
- [ ] 文档检查保留原文/引用/冻结集/不保存查询回答，去除旧 CLI 强制断言。
- [ ] `make check` 和 `make image-check` 实际执行并报告；容器默认不靠匿名模式过关。
- [ ] 合并最新集成 tip，无冻结接缝漂移。

## Comments

待实施。
