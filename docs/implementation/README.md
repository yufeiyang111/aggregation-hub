# Aggregation Hub 实施计划索引

> 状态：可执行基线  
> 基线日期：2026-08-02  
> 总计划：[../13-implementation-plan.md](../13-implementation-plan.md)

## 1. 使用方式

1. 先阅读总计划、`../ai/AI_CONTEXT.md` 和当前 Phase 文档。
2. 按 Phase 顺序执行；Phase 3 与 Phase 4 可在 Phase 2 Gate 通过后并行。
3. 每个 Task 遵循测试先行：失败测试 → 最小实现 → 最小验证 → 扩大验证。
4. 没有用户明确授权时，只检查 `git diff`，不 Commit、不 Push。
5. 真实 Provider、Claude Code、Codex 和 OAuth 验证必须单独标记 L3/L4/L5，不能用 Fake 测试替代。
6. 完成任务后在对应复选框记录命令、结果、日期、证据等级和剩余风险。

## 2. 阶段目录

| Phase | 文档 | 退出产物 |
|---:|---|---|
| 0 | [00-foundation.md](./00-foundation.md) | 工具链、Workspace、Go Core、Tauri Desktop、Sidecar 生命周期、CI 门禁 |
| 1 | [01-runtime-storage.md](./01-runtime-storage.md) | SQLite、Local Key、CredentialStore、Provider/模型仓储、Router、安全 Transport |
| 2 | [02-openai-provider.md](./02-openai-provider.md) | OpenAI Compatible 纵向切片、Chat API、Provider/模型 UI、真实 L3 |
| 3 | [03-anthropic-claude.md](./03-anthropic-claude.md) | Messages、Anthropic Adapter、Thinking/Tool、Claude Code L4 |
| 4 | [04-responses-codex.md](./04-responses-codex.md) | Responses、Reasoning/Function、Codex L4 |
| 5 | [05-observability-product-ui.md](./05-observability-product-ui.md) | 请求状态、用量、诊断、完整管理 UI、备份恢复、桌面 E2E |
| 6 | [06-oauth.md](./06-oauth.md) | OAuth Spike、PKCE/刷新框架、首个官方 OAuth Adapter、L5 |
| 7 | [07-release-hardening.md](./07-release-hardening.md) | 安全终审、托盘/自启、Windows 安装包、SBOM、干净 VM 发布门禁 |

## 3. 推荐执行边界

- 一个 Worker 一次只执行一个 Task；不要把多个 Phase 混在同一变更中。
- Phase 3 与 Phase 4 并行时，`apps/core/internal/normalize/`、OpenAPI 契约和生成类型由单一负责人协调，避免接口漂移。
- 任何数据库 Schema、公开 API、认证方式或安全边界变化，先更新设计文档或 ADR，再修改实现计划和代码。
- 真实凭据由用户在本机交互式输入；测试脚本只能读取临时进程环境，并在报告中记录“已运行/未运行”，不能记录值。
- 发布 Gate 失败时停止发布，不通过跳过测试、关闭 TLS、扩大监听地址或降低鉴权强度来绕过。

## 4. 完成定义

一个 Task 只有同时满足以下条件才可勾选完成：

- 计划列出的文件已按现有架构实现；
- 失败测试先被观察到，随后通过；
- 最小验证和受影响模块的 broader checks 均有结果；
- 安全负向用例通过，且输出不含秘密；
- 文档、契约、Schema 和生成文件保持同步；
- `git diff --check` 无错误；
- 未验证事项被明确列出，没有被描述为已经完成。