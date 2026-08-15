# ADR-0003：命名空间模型与确定性路由

- 状态：已接受
- 日期：2026-08-02

## 决策

Public Model ID 使用 `provider-slug/upstream-model-id`。V1 中一个公开模型唯一映射到一个 Provider 和上游模型，不实现权重、随机、智能选择和跨 Provider 自动故障转移。

## 结果

路由、费用和故障归属可预测，同名模型不冲突，也不会未经用户同意替换模型。代价是模型 ID 较长，Provider slug 变更会影响客户端。因此 slug 创建后不可原地修改，只能克隆迁移。