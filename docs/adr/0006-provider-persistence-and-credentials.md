# ADR-0006：Provider 持久化、凭据编排与模型同步语义

- 状态：已接受
- 日期：2026-08-14

## 背景

Provider 表同时保存生命周期状态、用户启用开关和 CredentialStore 引用；模型同步还需要在不覆盖用户意图的前提下更新上游目录。初始迁移已经对 slug、公开模型 ID 和 Provider 内上游模型 ID 设置唯一约束，因此实现不能把软删除当作可任意重建的新记录。

## 决策

1. Task 1.4 仅支持 `none`、`api_key` 与 `bearer_token` 认证。`oauth` Provider 在 OAuth Account 与官方授权流程完成前返回明确的阶段性错误，不能伪造可用的凭据引用。
2. Provider 的可路由条件为 `enabled=true` 且生命周期状态为 `enabled` 或 `degraded`；模型的可路由条件为 `enabled=true` 且生命周期状态为 `available` 或 `degraded`。`auth_required`、`disabled`、`missing_upstream` 与 `deleted` 都不可路由。
3. slug 创建后不可原地修改。软删除的 Provider 不复用 slug；用户需要重新接入时使用新的 slug 或由后续专用恢复流程恢复原记录。这与 ADR-0003 和当前唯一约束保持一致。
4. Provider 删除先在 SQLite 事务中软删除 Provider/模型并写无秘密审计事件；提交后再删除 CredentialStore 引用。凭据删除失败不回滚软删除，记录无秘密的清理失败审计并返回可判定错误，留给后续清理任务处理。
5. 替换凭据严格使用“写新引用 -> 提交数据库更新与审计 -> 删除旧引用”。数据库更新失败时删除新引用补偿；旧引用删除失败不回滚已经生效的新引用。
6. Task 1.4 不实现自定义 Provider Header；该能力在后续 Control Plane/Provider 输入契约完善后加入，避免把普通 Header、秘密 Header 和受保护 Header 的规则混入本阶段。
7. 同步模型时，新模型默认禁用；缺失上游模型标记为 `missing_upstream`，不物理删除；同步更新上游声明字段但保留 `enabled` 和 `capability_override_json`。公开模型 ID 始终由 `provider-slug/upstream-model-id` 派生，不能由调用方指定。能力覆盖仅允许 `supports_streaming`、`supports_tools`、`supports_parallel_tools`、`supports_reasoning`、`supports_thinking`、`supports_vision` 六个布尔字段；未知字段或非布尔值视为数据错误。
8. Provider 对外 DTO 仅返回 `credential.configured` 与基于原秘密生成的 `masked_hint`；不返回完整 CredentialStore 引用、秘密或可反推出引用的字段。
9. dapter_config_json 只保存非秘密配置；V1 拒绝含常见秘密字段名的对象，认证材料必须经专用 CredentialStore 输入传递。

## 后果

- 在 OAuth 和 Header 功能完成前，控制面需对这些输入提供明确的“不支持/尚未配置”错误，而不是静默忽略。
- 软删除记录占用原 slug 和模型唯一键；V1 用确定性、可审计的恢复/新 slug 策略换取数据库迁移的最小风险。
- 凭据库和 SQLite 不支持跨资源原子事务，因此补偿和审计是必要的可靠性边界；未清理的旧引用不会重新暴露给 Router。
