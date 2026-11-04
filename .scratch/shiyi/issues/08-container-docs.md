# 08: 容器与使用文档

**What to build:** 用户能用容器运行 Shiyi，把数据目录挂载到宿主机；升级或重启容器后记录仍在。README 能让人从零完成一次记录和一次查询，并说明配置、鉴权、健康检查与已知限制。

**Blocked by:** 04 (失败与持久化边界), 05 (CLI), 06 (Web).

**Status:** ready-for-agent

- [x] 容器镜像构建成功，运行环境内 `rg --version` 可执行。
- [x] 挂载数据卷后，重启容器记录仍然存在。
- [x] 按 README 的命令能完成一次记录和一次查询。
- [x] 配置示例覆盖模型、数据目录、分片、鉴权与预算，密钥只通过环境变量提供。
- [x] 健康检查在容器内返回 200。
- [x] README 明确列出已知限制：无回答持久化、无幂等、无会话上下文、不支持附件、跨换行检索限制。

## Comments

已完成。集成在 `feat/shiyi-v1`。

- `Dockerfile` 多阶段构建：builder 用 vendor 离线编译，runtime 为 alpine + ripgrep + 非 root 用户，数据卷 `/data`，`HEALTHCHECK` 用 busybox wget 探 `/healthz`。
- 已实测：镜像构建成功、容器内 `rg --version` 正常、`/healthz` 200、挂载卷重启后记录仍在、无 API key 启动即失败并给出可操作提示。
- `config.example.toml` 与 design.md §8 一致，密钥只写变量名；`docs/config.docker.toml` 为容器内默认配置。
- `README.md` 覆盖从零记录+查询、配置、鉴权、健康检查、CLI、数据格式与 11 条已知限制。
- `docs/check-readme.sh` 交叉校验 README / Dockerfile / 配置 / 错误码 / CLI 子命令（177 项），改动后跑一次即可发现漂移。
