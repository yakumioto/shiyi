# 01: 冻结认证跨包接缝

Status: ready-for-agent
Execution: claimed
Blocked by: none

## 范围

阅读 ../spec.md 与 ../implementation.md，准备可编译的认证公共契约及接缝清单。写范围：internal/config/config.go、internal/config/auth_contract.go、internal/auth/、internal/api/auth.go、必要的接缝测试、../seams.md；不要改变现有运行行为或加入危险占位授权。

选择成熟哈希算法并冻结配置名称、默认值、错误哨兵、构造器签名、会话状态/CSRF 获取和变更端点的线格式及错误码。模块尽量深：认证模块封装密码/持久化/限速/来源/CSRF，HTTP 层只接入公共行为。明确时钟注入便于 HTTP 时间测试；UI 消费稳定 JSON；明确生产 Secure Cookie 与匿名开发例外。

## 验收

- [ ] 已提交可编译的配置、公共类型/接口、构造器类型与 HTTP 请求响应约定。
- [ ] seams.md 明确包归属、依赖方向、路由、配置和错误映射、来源/CSRF 协议。
- [ ] 没有在接缝阶段改变现有授权行为；`make check` 通过。

## Comments

待实施。
