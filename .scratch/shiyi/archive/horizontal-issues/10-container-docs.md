# 10 - 容器化与使用文档

Status: ready-for-agent
Depends on: 06

## 目标

提供可运行的单容器镜像与最小使用文档，确保容器内 `ripgrep` 可用、数据可持久化。

## 范围

- `Dockerfile`：多阶段构建；运行镜像包含 `ripgrep`；非 root 用户；暴露 8787。
- `.dockerignore`：排除 `.git`、`.scratch`、`testdata`、构建产物。
- 数据卷 `/data`；镜像默认 `SHIYI_DATA_DIR=/data`、`SHIYI_LISTEN=0.0.0.0:8787`。
- `HEALTHCHECK` 调用 `/healthz`。
- `config.example.toml`：与规格第 8 节一致，密钥用环境变量占位。
- `README.md`：
  - 一句话定位与范围裁剪；
  - 本地构建与运行；
  - 容器运行示例（挂载卷、传 API key、可选 token）；
  - 配置说明；
  - API 示例（record/query/unrecognized/错误）；
  - 已知限制（无回答持久化、无幂等、无会话上下文、附件不支持、跨换行搜索限制）。
- 不提供 docker-compose、Kubernetes 清单或多副本方案。

## 验收

- `docker build` 成功。
- 容器内 `rg --version` 可执行。
- 挂载卷后重启容器，记录仍在。
- `/healthz` 在容器内返回 200。
- README 中的命令可直接复制运行（除密钥外）。

## 参考

- [design.md](../design.md)。
- [ADR 0005](../../../docs/adr/0005-service-availability-follows-model.md)、[ADR 0006](../../../docs/adr/0006-only-records-are-persisted.md)。
