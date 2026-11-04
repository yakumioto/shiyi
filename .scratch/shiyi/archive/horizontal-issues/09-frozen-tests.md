# 09 - 冻结测试集与回归门禁

Status: ready-for-agent
Depends on: 06

## 目标

建立可复现、不可就地修改的冻结测试集，用假模型覆盖分类、检索、引用与失败路径。

## 范围

- 目录 `testdata/frozen/v1/`：
  - `manifest.json`：每个文件的 SHA-256。
  - `classify.jsonl`：`{name, input, model_response, expected}`。
  - `query.jsonl`：`{name, input, model_turns:[...], expected}`。
- 分类用例至少覆盖：
  - 明确记录、明确查询、混合输入、闲聊/无法识别、空内容边界、模型非法 JSON、模型超时。
- 查询用例至少覆盖：
  - 单轮命中、多关键词、多轮检索、无结果、预算耗尽、非法引用、模型返回非 JSON、工具参数非法。
- 测试使用：
  - 临时数据目录与真实 `rg`；
  - 假 LLM 按夹具逐轮返回；
  - 真实分类/回答/HTTP 链路（优先走 `httptest`）。
- `manifest.json` 在测试启动时校验；哈希不匹配直接失败。
- `v1` 冻结后不得修改；新增用例进 `v2/`，测试同时跑所有版本。
- 提供 `scripts/eval-live.sh`：用真实模型跑同一批输入，输出准确率与差异；不接入 CI，不阻断发布。
- Makefile 增加 `test-frozen`，CI 执行 `go test ./... -race` 与冻结集。

## 验收

- 删除或修改 `v1` 任一文件后，测试失败并指出文件。
- 假模型下全部用例稳定通过，重复运行结果一致。
- 无网络访问。
- `scripts/eval-live.sh` 在无密钥时给出明确提示并退出。

## 参考

- [design.md](../design.md)。
- [ADR 0004](../../../docs/adr/0004-llm-classification-and-mixed-as-query.md)。
