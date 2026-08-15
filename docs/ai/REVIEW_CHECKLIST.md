# Aggregation Hub AI 审查清单

## 范围与架构

- [ ] 关联需求 ID、Issue 或 ADR。
- [ ] 没有无关重构。
- [ ] Ingress、Normalizer、Router、Adapter、Transport、Repository 边界未破坏。
- [ ] 新 Provider 没有在 Router 增加供应商分支。
- [ ] WebView 不直接访问 SQLite、管理令牌和完整凭据。

## 输入与类型

- [ ] Body、Path、Query、Header 均验证。
- [ ] 外部 JSON 使用 Schema 或强类型。
- [ ] 没有无约束 `any` 穿过业务层。
- [ ] 字符串、数组、深度和大小有限制。
- [ ] 排序和 Header 使用 allowlist。

## 认证与凭据

- [ ] Data Plane 除 `/health` 外鉴权。
- [ ] Local Key 只保存哈希。
- [ ] 上游凭据使用 CredentialStore。
- [ ] Token 不在日志、URL、命令行、Store 和错误中。
- [ ] 凭据替换有补偿逻辑。
- [ ] OAuth 使用 state、PKCE、一次性回调和安全刷新。
- [ ] 无 Cookie、Session 和账号密码抓取。

## 网络与协议

- [ ] URL 只允许 HTTP/HTTPS，TLS 未关闭。
- [ ] 重定向重新验证且认证头不跨主机。
- [ ] 私有地址和元数据遵守网络策略。
- [ ] HTTP Transport 复用。
- [ ] Header、Body、SSE 和错误体有界。
- [ ] 客户端取消传播并关闭上游 Body。
- [ ] 流式开始后无重试/换 Provider。
- [ ] 不支持字段明确报错。
- [ ] System、Tool ID、Reasoning/Thinking 和 Usage 语义正确。
- [ ] 一个请求只有一个终态。

## 数据库

- [ ] Schema 改动有新迁移，旧迁移未修改。
- [ ] 外键、唯一约束、索引和事务合理。
- [ ] 金额使用整数微美元。
- [ ] 正文和完整凭据未写入数据库。
- [ ] 软删除不破坏历史。
- [ ] 迁移失败不 reset。

## 前端与日志

- [ ] 加载、空、错误、成功状态完整。
- [ ] 防重复提交和过期响应。
- [ ] 表单 Label、错误关联、弹窗焦点正确。
- [ ] 状态不只依赖颜色。
- [ ] 危险操作说明对象和后果。
- [ ] 无不可信 `dangerouslySetInnerHTML`。
- [ ] 完整凭据不进 localStorage/sessionStorage/URL。
- [ ] 日志和诊断无 Header、Body、Token、Tool 参数。

## 测试与报告

- [ ] Bug 有回归测试或解释。
- [ ] 正向、负向、边界、取消覆盖。
- [ ] Adapter 通过共享契约。
- [ ] DB 使用真实临时 SQLite。
- [ ] 安全变更有负向测试和秘密扫描。
- [ ] 运行最小测试和对应 format/lint/typecheck/build。
- [ ] 文档和追踪矩阵同步。
- [ ] Fake 与真实证据明确区分。
- [ ] 最终报告列出命令、结果、证据等级和剩余风险。