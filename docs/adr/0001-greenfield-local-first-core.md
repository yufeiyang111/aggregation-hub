# ADR-0001：独立实现 Local-first 网关核心

- 状态：已接受
- 日期：2026-08-02

## 背景

New API 等项目证明了 Channel、Adapter、模型映射和协议转换的可行性，但同时包含用户、支付、额度、运营和多租户能力。Aggregation Hub 的目标是本地单用户桌面工具。

## 决策

独立实现 Aggregation Hub 核心，借鉴公开架构概念但不复制 New API 的 AGPL 源码。范围聚焦 Data Plane、Provider/模型、规范化协议、Adapter、SQLite、用量、诊断和受支持 OAuth。

## 结果

优点：产品边界清晰、可选择宽松许可证、无运营包袱、安全按桌面场景设计。代价：必须自行实现并真实验证 SSE、Tool Calling、Reasoning 和客户端兼容。

拒绝方案：直接 Fork New API；永久把完整 New API 作为 Sidecar。