# Issue tracker: Local Markdown

本项目的任务与规格以 Markdown 文件形式保存在 `.scratch/`。

## 约定

- 每个功能一个目录：`.scratch/<feature-slug>/`。
- 规格文件：`.scratch/<feature-slug>/spec.md`。
- 实施任务：`.scratch/<feature-slug>/issues/<NN>-<slug>.md`，从 `01` 开始编号，每个任务单独一个文件。
- 分诊状态记录在任务文件顶部附近的 `Status:` 行；状态值见 `docs/agents/triage-labels.md`。
- 评论和讨论历史追加到文件末尾的 `## Comments` 标题下。

## 当技能要求“发布到任务跟踪器”

按上述约定创建规格或任务文件；需要时创建对应目录。

## 当技能要求“获取相关任务”

读取用户给定路径的文件。只有编号时，在当前功能目录内定位对应任务；无法唯一确定时，请用户指明功能或文件路径。

## Wayfinding operations

若使用 `/wayfinder`，以一个地图文件协调多个子任务：

- 地图：`.scratch/<effort>/map.md`，记录 Notes、Decisions-so-far 和 Fog。
- 子任务：`.scratch/<effort>/issues/<NN>-<slug>.md`，从 `01` 编号，正文记录待回答的问题；`Type:` 为 `research`、`prototype`、`grilling` 或 `task`。
- 本节子任务的 `Status:` 使用 `open`、`claimed` 或 `resolved`，与分诊标签区分。
- 阻塞关系：顶部附近记录 `Blocked by: NN, NN`；所有依赖均为 `resolved` 时解除阻塞。
- 下一任务：扫描子任务目录，选择未认领、未阻塞的开放任务，按编号从小到大。
- 认领：开始工作前，先设置 `Status: claimed` 并保存。
- 完成：在 `## Answer` 下追加答案，设置 `Status: resolved`，再把摘要和链接追加到地图的 Decisions-so-far。
