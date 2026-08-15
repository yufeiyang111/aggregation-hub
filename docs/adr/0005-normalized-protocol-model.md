# ADR-0005：通过规范化模型解耦入口和上游协议

- 状态：提议，随本批设计评审
- 日期：2026-08-02

## 决策

Anthropic Messages、OpenAI Responses 和 Chat Completions 都先转换为 NormalizedRequest/NormalizedEvent，再由目标 Adapter 转换成上游协议。

规范化模型使用显式联合类型，不以任意 Map 代替；不支持语义明确拒绝；Tool Call ID 建立请求级映射；System、Reasoning、Thinking 和 Usage 保留来源与未知状态。

该决策把 N×M 组合降低为新增一个 Ingress 或一个 Adapter，但要求认真维护规范化契约和回归测试。