# 01 - 项目骨架、配置与健康检查

Status: ready-for-agent
Depends on: none

## 目标

建立可构建、可启动的 Go 项目骨架，完成配置加载、日志与最小 HTTP 服务。

## 范围

- 初始化 Go module（模块名 `shiyi`），目录：`cmd/shiyi`、`internal/config`、`internal/httpapi`。
- 用 TOML 加载配置，支持 `SHIYI_CONFIG`，默认 `~/.config/shiyi/config.toml`。
- 环境变量覆盖：`SHIYI_LISTEN`、`SHIYI_DATA_DIR`、`SHIYI_SHARD`；token 由 `auth_token_env` 指向的变量提供。
- 启动校验：必填模型配置、分片枚举、数据目录可写、`rg` 可执行、API key 环境变量存在。
- 使用 `log/slog` 输出结构化日志；不打印任何密钥或完整原文。
- `GET /healthz` 返回 `200 {"status":"ok"}`。
- `shiyi serve` 子命令；`shiyi --help`。
- Makefile：`build`、`test`、`vet`、`run`。

## 验收

- `go build ./...`、`go vet ./...` 通过。
- 配置缺失、分片值非法、API key 缺失时启动失败，错误信息可直接指导修复。
- 配置优先级有单测：环境变量 > 文件 > 默认值。
- 启动日志包含监听地址、数据目录、分片方式，不含密钥。
- `/healthz` 返回 200。

## 参考

- [design.md](../design.md)。
