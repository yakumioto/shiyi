## Agent skills

### Issue tracker

任务与规格使用本地 Markdown，存放于 `.scratch/<feature-slug>/`；读取、创建或更新任务前，先读 `docs/agents/issue-tracker.md`。

### Triage labels

使用默认的五个 triage 标签；判断或更新分诊状态前，先读 `docs/agents/triage-labels.md`。

### Domain docs

采用 single-context：根目录 `GLOSSARY.md` + `docs/adr/`；探索代码或讨论领域模型前，先读 `docs/agents/domain.md`。

### 门禁

`make check` 是本仓库唯一的本地门禁，逐条对应 CI 的测试任务；声称「绿了」之前先跑它，不要手搓 `go test` / `go vet`。

### 并行实施

两张以上票同时开工、一票一工作树、或要先冻结跨包接缝时，先读 `docs/agents/parallel-implementation.md`。
