# 06: Web：极简浏览器界面

**What to build:** 用户打开首页就能发送一句话，看到它被记录，或看到带引用的回答；界面只呈现当前会话，不保存历史。模型输出按纯文本显示，不能执行脚本。

**Blocked by:** 02 (查询路径).

**Status:** ready-for-agent

- [x] 首页能发送记录并显示记录时间。
- [x] 首页能发送查询并显示回答、状态与引用原文。
- [x] 回答或原文中的 HTML/脚本不会被执行。
- [x] 刷新后不保留任何历史。
- [x] 无外部 CDN、无遥测、窄屏可用。
- [x] 手工冒烟覆盖记录、查询、无法识别与错误提示。

## Comments

已完成。集成在 `feat/shiyi-v1`。

- `internal/web` 用 `go:embed` 内嵌单页；`web.Handler()` 挂在服务根路径，与 `POST /messages` 并列。
- 全部内容用 `textContent` 渲染；测试断言不存在 `innerHTML` / `document.write` / `eval(` / 外部 CDN / 浏览器持久化 API。
- 刷新不保留历史；窄屏媒体查询；无遥测。
- 已知边界：页面不附加 `Authorization`，启用 Bearer 鉴权时应改用 CLI（README 已写明）。
- 每个请求只发一次 `POST /messages`，不携带历史。
