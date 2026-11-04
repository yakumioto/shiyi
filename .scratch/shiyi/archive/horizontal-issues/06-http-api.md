# 06 - HTTP API 与分类入口

Status: ready-for-agent
Depends on: 02, 03, 05

## 目标

实现 `POST /messages` 的完整链路：分类、记录、查询、错误码、鉴权与超时。

## 范围

- `internal/classify`：
  - 分类提示词 `classify_v1`。
  - 解析模型 JSON，接受 `record`、`query`、`unrecognized`；`mixed` 视为 `query`。
  - 非法输出映射为 `ErrInvalidResponse`。
- `internal/httpapi`：
  - `POST /messages`，请求 `{"content":"..."}`。
  - body 上限 1 MiB；`content` 去空白后为空 → 400。
  - 可选 Bearer token 鉴权；`/healthz` 不鉴权。
  - 分类：
    - `record` → `store.Append` → `201 {"kind":"record","time","version"}`。
    - `query` → `answer.Answer` → `200 {"kind":"query","answer","citations","status"}`。
    - `unrecognized` → `200 {"kind":"unrecognized"}`。
  - 错误映射严格按规格错误码表；所有错误响应为 `{"error":{"code","message"}}`。
  - 请求总超时 60 秒；客户端断开时取消下游调用。
- 依赖注入：`llm.Client`、`store.Store`、`answer.Answerer` 均可在测试中替换。
- 集成测试：临时数据目录、假 LLM、真实 `rg`、`httptest.Server`。
  - record 后文件恰好一行；query/unrecognized 后文件不变。
  - 模型超时/坏输出 → 503 且无新文件。
  - 非法引用 → 502。
  - 鉴权、413、400。

## 验收

- 规格第 13 节验收清单中所有 HTTP 行为通过。
- 错误响应不泄露密钥、不返回模型原始错误体。
- `go test ./internal/httpapi -race` 通过。

## 参考

- [design.md](../design.md)。
- [ADR 0004](../../../docs/adr/0004-llm-classification-and-mixed-as-query.md)、[ADR 0005](../../../docs/adr/0005-service-availability-follows-model.md)。
