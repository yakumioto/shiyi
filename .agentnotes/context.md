# Shiyi 实施上下文（所有实施子代理必读）

本文件是给实施子代理的**上下文指针**，不是规格的替代品。规格以 `.scratch/shiyi/spec.md`、
`.scratch/shiyi/design.md`、`.scratch/shiyi/issues/<NN>-*.md` 为准。

## 0. 先读这些

1. `.scratch/shiyi/spec.md` — 产品范围、用户故事、实现决策
2. `.scratch/shiyi/design.md` — 技术细节（数据模型、配置、错误码表、提示词、验收清单）
3. `.scratch/shiyi/issues/<你的票号>-<slug>.md` — 你这一票的验收清单
4. `GLOSSARY.md` — 领域词汇（记录/查询/原文/记录时间/引用/记忆条目…）。命名代码与测试时用这里的词。
5. `docs/adr/0003..0006` — 相关 ADR

## 1. 环境（重要，务必先设置）

沙箱内 `~/.cache/go-build`、`~/.go/pkg/mod` **不可写**。所有 `go` 命令前必须：

```bash
export GOENV=/home/mioto/workspace/me/shiyi/.goenv/env
```

该文件已配置：`GOCACHE`、`GOMODCACHE` 指向仓库内目录，`GOFLAGS=-mod=vendor`（依赖已 vendor，**无需网络**），
`GOSUMDB=off`。

- Go 版本 1.26。模块名 `shiyi`。
- 唯一第三方依赖：`github.com/pelletier/go-toml/v2`（TOML 解析），已在 `vendor/` 中。**不要新增依赖**。
- 系统有真实 `rg`（ripgrep 14.1.0），可直接调用；测试也用它。
- 测试**不得访问网络**。

## 2. 代码布局与包边界

模块根：`/home/mioto/workspace/me/shiyi`（主仓库）；你的工作树在 `.worktrees/<ticket>`。

```
cmd/shiyi/          CLI 入口（子命令 serve / 一次性 / REPL）
internal/config/    TOML 配置加载、环境变量覆盖、启动校验
internal/store/     append-only JSONL 记忆条目存储（分片、fsync、崩溃恢复）
internal/llm/       OpenAI-compatible Chat Completions 客户端（可注入假实现）
internal/classify/  意图判定（record/query/unrecognized）+ classify_v1 提示词
internal/search/    ripgrep 预筛 + Go 精确匹配
internal/answer/    多轮检索与带引用回答编排
internal/httpapi/   HTTP 服务：POST /messages、GET /healthz、鉴权、错误码
internal/web/       go:embed 的极简单页（票 06）
testdata/frozen/    冻结测试集（票 07）
```

**包边界是契约。** 跨包只通过导出的 Go 接口交互，不加内部 hack。
若你认为某个包边界必须调整，**先在票的最终报告里说明理由**，不要默默改别人的包。

## 3. 测试缝（已与用户确认，见 design.md §11）

只在以下三个缝上写测试，不要测私有函数或内部字段：

1. **HTTP 入口（最高缝）** — 注入假 LLM、真实存储、真实 ripgrep、临时数据目录，
   通过 `POST /messages` 验证分类、落盘、查询、引用、状态码、鉴权、错误路径。CLI 与 Web 复用此缝。
2. **记录存储** — 直接验证时间严格递增、分片（月/周/日）、写入后持久化、半行恢复、中间损坏失败。
3. **LLM HTTP 边界** — 用 `httptest` 验证 OpenAI-compatible 请求形状、工具调用解析、超时、非 2xx、坏 JSON、密钥不外泄。

要求：确定性、无网络、可并行、表驱动优先；失败信息要能直接指出哪条行为坏了。

## 4. 全局不变量（任何票都不能违反）

- 记忆条目 JSON 行**恰好三个字段**：`version`（恒为 1）、`content`（原文逐字）、`time`（UTC RFC3339Nano）。
- `time` 严格递增且唯一，既是标识也是引用锚点。
- **只有被判定为 record 的输入才写文件。** 查询、无法识别、模型失败一律不写。
- 记录**不可变**：没有编辑、删除、去重、幂等键。
- 查询与回答**不落盘**：无查询历史、无回答档案、无审计日志、无会话状态。
- 错误响应统一 `{"error":{"code","message"}}`；错误码严格按 design.md §7.1 表。
- 日志**不包含**密钥与完整原文。
- API key **只**从配置指向的环境变量读取，不写配置、不写日志。
- 引用必须来自本次查询实际检索到的记录，否则 `502 invalid_citation`。
- 无检索结果时返回 `status:"no_match"`，固定回答「没有找到相关记忆」，丢弃模型文本。

## 5. 不要做的事

- 不要引入数据库、向量检索、embedding、索引、标签、摘要。
- 不要加会话上下文、多轮追问流、回答持久化、Markdown 渲染、流式输出。
- 不要为「将来可能需要」预留抽象或 TODO 桩。
- 不要修改 `vendor/`、`go.mod`、`go.sum`（除非确实需要新依赖，先说明）。
- 不要改 `.scratch/` 下的规格文件（由 Lead 统一更新票状态）。
- 不要 `git push`。

## 6. 分支与提交规则

- 你的工作树在 `.worktrees/<ticket>`，分支 `tkt/<NN>-<slug>`。
- 开始前确认工作树基于集成分支 `feat/shiyi-v1`；不是则重置到它。
- 提交信息用中文或英文祈使句，格式 `feat(<pkg>): <简述>` / `fix(<pkg>): <简述>`。
- 提交前必须本地跑通：
  ```bash
  export GOENV=/home/mioto/workspace/me/shiyi/.goenv/env
  go build ./... && go vet ./... && go test ./... -race
  ```
- 完成后**把集成分支最新 tip 合并进你的分支**（`git merge feat/shiyi-v1`），解决冲突后再报告。
- 最终报告用中文，包含：做了什么、测试怎么跑通的、改动文件清单、**需要 Lead 知道的取舍或风险**。
