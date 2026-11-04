# 01: 记录路径：一句话变成一条记录

**What to build:** 用户能通过一个 HTTP 请求把一句话交给 Shiyi；被判定为记录时，原文立即成为一条可检索的记忆，响应返回记录时间；被判定为无法识别时，明确告诉用户没有保存。这个切片打通配置、分类、记录存储与记录入口，是后续所有切片的地基。

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

- [x] 服务能按配置的模型地址、模型名和密钥环境变量启动，`GET /healthz` 返回 200。
- [x] `POST /messages` 收到一条被判定为记录的消息时返回 201，响应包含 `kind:"record"`、`version:1` 和 `time`。
- [x] 数据目录新增一行 JSONL，字段恰好为 `version`、`content`、`time`；`content` 与提交原文逐字一致。
- [x] 连续提交两条记录，产生的 `time` 严格递增且互不相同。
- [x] 收到无法识别的消息时返回 200，数据目录不新增任何行。
- [x] 分类模型返回非法内容或调用失败时，本次不写文件，并返回明确错误。
- [x] 查询路径尚未实现时返回明确的未实现错误，且不写文件；查询行为在 02 补齐。
- [x] 测试使用假 LLM 覆盖记录、无法识别和不落盘路径，可在无网络环境运行。

## Comments

已完成。集成在 `feat/shiyi-v1`。

- 配置：`internal/config`（TOML + 环境变量覆盖 + 启动校验 + `~` 展开）。TOML 字符串时长（`"20s"`）用私有 raw 结构承接后转 `time.Duration`。
- 存储：`internal/store`（append-only JSONL、按月/周/日 UTC 分片、`fsync`、目录次同步、严格递增时间、半行隔离、中间损坏拒绝启动）。
- 分类：`internal/classify`（`classify_v1`，`mixed` → `query`，非法输出 → `model_invalid_response`）。
- 入口：`internal/httpapi`（`POST /messages`、`GET /healthz`、鉴权、body 上限、错误码表）。
- 测试：存储缝 + HTTP 缝，表驱动，无网络。
- 验证：`go build ./... && go vet ./... && go test ./... -race`，端到端冒烟（真实二进制 + 假模型）全过。
