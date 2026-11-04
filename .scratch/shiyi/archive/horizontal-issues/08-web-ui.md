# 08 - 极简 Web 界面

Status: ready-for-agent
Depends on: 06

## 目标

提供一个单页界面，让用户能记录、查询并看到回答与引用。

## 范围

- `GET /` 返回内嵌的 `index.html`（`go:embed`），无前端构建步骤。
- 原生 HTML/CSS/JS；一个输入框、一个发送按钮、消息列表。
- 只调用 `POST /messages`：
  - record：显示「已记录」与时间。
  - query：显示回答、状态、引用列表（时间 + 内容）。
  - unrecognized：显示提示。
  - 错误：显示错误码与信息。
- 不做多会话、历史浏览、设置页、登录页、Markdown 渲染、流式输出。
- 所有内容用 `textContent` 渲染，禁止把模型输出或原文插入 `innerHTML`。
- 页面刷新后清空，不复原历史。

## 验收

- 能完成一次记录和一次查询，引用可见。
- 模型输出中的 HTML/脚本不会被执行。
- 页面在移动端窄屏可用。
- 无外部 CDN、无遥测。

## 参考

- [design.md](../design.md)。
