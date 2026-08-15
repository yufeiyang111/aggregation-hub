# ADR-0007：Provider 网络分类与安全上游传输

- 状态：已接受
- 日期：2026-08-14

## 背景

V1 的 Provider 表没有单独的网络类别字段，但网络策略必须区分互联网 Provider 与用户明确配置的本地服务，同时防止 Base URL、DNS 解析和重定向触发 SSRF 或认证头泄露。

## 决策

1. V1 根据 `adapter_type` 决定网络类别：仅 `local-openai-compatible` 是 Local Provider；其他 Adapter 一律是 Public Provider。网络类别不接受 UI 或请求方任意覆盖。
2. Public Provider 只允许 HTTPS，且每次 URL、DNS 解析与重定向都拒绝回环、私有、链路本地、未指定、多播和云元数据地址。
3. Local Provider 可以访问用户显式配置的回环或私有地址，但仍永久拒绝云元数据地址、未指定地址和多播地址；协议仍仅允许 HTTP/HTTPS。
4. Transport 禁止环境代理与关闭 TLS 校验；不提供忽略证书错误或全局自定义 CA 选项。
5. DNS 拨号在解析后校验所有候选地址，并直接连接已校验 IP，避免校验与实际连接之间重新解析。
6. 重定向最多五跳；每一跳重新校验，跨主机时显式移除 `Authorization`、`Proxy-Authorization`、`X-API-Key` 和 `Anthropic-API-Key`。
7. 流式请求不使用 `http.Client.Timeout` 截断总时长；由调用 Context、Transport 分阶段超时和响应 Body 空闲监视共同控制。错误摘要有界读取、关闭 Body 并返回受限 Content-Type。

## 后果

- 已保存的 Public HTTP Base URL 会在本次校验后无法创建或更新；用户应改为 HTTPS，或者明确使用 `local-openai-compatible` 接入本地 HTTP 服务。
- 自定义内网 Provider 在 V1 必须使用 Local Adapter；未来若需要更多网络分类，需新增显式 Schema 字段和迁移，不得放宽默认 Public 策略。
- Fake 测试只能证明策略与代理链路（L1/L2），不证明任何真实 Provider 的可用性。
