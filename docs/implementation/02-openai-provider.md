# Phase 2: OpenAI-Compatible Provider Vertical Slice Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 交付第一个完整 Provider 纵向切片：创建 OpenAI Compatible Provider、同步模型、通过 `/v1/chat/completions` 完成非流式/流式/Tool Calling，并达到真实 Provider L3。

**Architecture:** Ingress 将 Chat Completions 转成 NormalizedRequest；Router 返回唯一 RoutePlan；OpenAI Adapter 用 CredentialStore 和安全 Transport 调用上游；NormalizedEvent 再由 Ingress 输出 OpenAI SSE。

**Tech Stack:** Go net/http、SQLite、React Query/Zod、Vitest、Fake Upstream。

## Global Constraints

- 不支持字段明确拒绝，不把 Chat 请求直接透传。
- 流式开始后不重试。
- 自定义 Header 不得覆盖认证和传输保护头。
- Provider 创建失败必须补偿凭据。
- 真实凭据只用于本地 L3 测试，不进入仓库和日志。

---

### Task 2.1: Normalized Request/Response/Event

**Files:**
- Create: `apps/core/internal/normalize/types.go`
- Create: `apps/core/internal/normalize/validation.go`
- Create: `apps/core/internal/normalize/validation_test.go`
- Create: `contracts/normalized-event.schema.json`

**Interfaces:**
- Produces: `NormalizedRequest`、`NormalizedResponse`、`NormalizedEvent`、`StreamEmitter`。
- Consumes: Provider Capabilities。

- [x] Write failing tests for System separation、Tool schema depth、invalid tool result ID、single terminal event。
- [x] Define explicit union types: TextPart、ReasoningPart、ToolCallPart、ToolResultPart；禁止主路径 `map[string]any`。
- [x] Implement validation with configurable size/depth limits and required capability extraction。
- [x] Implement event sequence validator: one response_start, zero/many deltas, exactly one response_end/error。
- [ ] Run:

```powershell
cd apps/core
go test ./internal/normalize -v -race
go test ./...
```

当前验证：`go test ./internal/normalize -v`、`go vet ./internal/normalize` 与 `go test ./...` 已通过；`-race` 因本机 Go Windows race linker 自身报错暂未通过，详见任务报告。Suggested commit: `feat(core): define normalized protocol contract`。

---

### Task 2.2: Adapter Registry 与 OpenAI Adapter 配置

**Files:**
- Create: `apps/core/internal/adapter/adapter.go`
- Create: `apps/core/internal/adapter/registry.go`
- Create: `apps/core/internal/adapter/registry_test.go`
- Create: `apps/core/internal/adapter/openai/config.go`
- Create: `apps/core/internal/adapter/openai/config_test.go`

**Interfaces:**
- Produces: `Adapter` interface、`Registry.Register/Create`、OpenAI Config Schema。
- Consumes: Normalized Contract、Transport、Credential。

- [x] Write registry tests: duplicate type rejected、unknown type typed error、factory creates independent adapter。
- [x] Implement interface signatures from `docs/06-provider-adapter-design.md`。
- [x] Define OpenAI config: `wire_api` enum `chat_completions|responses`、chat/models paths、auth header mode；secret fields prohibited in config JSON。
- [x] Register `openai-compatible` and `local-openai-compatible` with different NetworkPolicy。
- [ ] Run `go test ./internal/adapter/... -v -race`。Expected PASS。

当前验证：`go test ./internal/adapter/... -v`、`go vet ./internal/adapter/...` 与 `go test ./...` 已通过；`-race` 因本机 Go Windows race linker 自身报错暂未通过，详见任务报告。Suggested commit: `feat(core): add adapter registry and OpenAI configuration`。

---

### Task 2.3: Chat Completions Ingress

**Files:**
- Create: `apps/core/internal/ingress/openai_chat/request.go`
- Create: `apps/core/internal/ingress/openai_chat/normalize.go`
- Create: `apps/core/internal/ingress/openai_chat/response.go`
- Create: `apps/core/internal/ingress/openai_chat/handler.go`
- Create: `apps/core/internal/ingress/openai_chat/handler_test.go`
- Modify: `apps/core/internal/dataplane/server.go`

**Interfaces:**
- Produces: `POST /v1/chat/completions`。
- Consumes: Local Auth、Router、Adapter Service、Normalized Contract。

- [ ] Write table tests for text、system、multi-turn、tools、tool_choice、stream、invalid model、unsupported vision、unknown top-level field policy。
- [ ] Implement strict DTO with request size limit and safe request ID。
- [ ] Normalize System separately；map tools/tool results with ID validation；derive RequiredCapabilities。
- [ ] Convert non-streaming NormalizedResponse into OpenAI response；never expose Provider credential/base URL。
- [ ] Register route behind Local Key middleware。
- [ ] Run `go test ./internal/ingress/openai_chat ./internal/dataplane -v`。Expected PASS。

Suggested commit: `feat(api): add OpenAI chat completions ingress`。

---

### Task 2.4: OpenAI Upstream 非流式、SSE 与 Tool

**Files:**
- Create: `apps/core/internal/adapter/openai/request.go`
- Create: `apps/core/internal/adapter/openai/response.go`
- Create: `apps/core/internal/adapter/openai/stream.go`
- Create: `apps/core/internal/adapter/openai/adapter_test.go`
- Create: `apps/core/internal/transport/sse.go`
- Create: `apps/core/internal/transport/sse_test.go`
- Create: `apps/core/testdata/openai/*.jsonl`

**Interfaces:**
- Produces: OpenAI Adapter BuildRequest/ParseResponse/ParseStream。
- Consumes: Transport、Credential、Normalized Contract。

- [ ] Build Fake Upstream fixtures for non-stream text/tool、SSE text/tool argument arbitrary chunks、usage、401/429/500/truncated。
- [x] First run adapter contract and expect FAIL due absent implementation。
- [x] Implement request URL with structured Resolve；apply Credential；protected Header cannot be overridden；set upstream model ID。
- [x] Implement bounded SSE parser supporting multi-line data and chunk boundaries。
- [ ] Emit NormalizedEvents with tool argument deltas and one terminal event；usage unknown remains nil。
- [ ] Verify client cancellation closes upstream and no retry after first emitted byte。
- [ ] Run:

```powershell
cd apps/core
go test ./internal/adapter/openai ./internal/transport -v -race
go test ./...
```

Expected PASS。Suggested commit: `feat(adapter): proxy OpenAI chat streams and tools`。

---

### Task 2.5: Provider/模型 Control API 与 Desktop 页面

**Files:**
- Modify: `contracts/control-plane.openapi.yaml`
- Modify: `apps/core/internal/controlplane/provider_handler.go`
- Create: `apps/core/internal/controlplane/model_handler.go`
- Create: `apps/desktop/src/services/controlClient.ts`
- Create: `apps/desktop/src/schemas/provider.ts`
- Create: `apps/desktop/src/features/providers/ProviderListPage.tsx`
- Create: `apps/desktop/src/features/providers/ProviderWizard.tsx`
- Create: `apps/desktop/src/features/providers/ProviderWizard.test.tsx`
- Create: `apps/desktop/src/features/models/ModelListPage.tsx`
- Modify: `apps/desktop/src/app/App.tsx`

**Interfaces:**
- Produces: Provider CRUD/test/sync、Models list/enable/disable UI。
- Consumes: Control Plane、Zod、React Query。

- [x] Add OpenAPI schemas with credential input-only and masked response；run contract check expecting generated/type drift failure。
- [x] Implement explicit Tauri commands and `controlClient` methods, not generic arbitrary URL proxy。
- [ ] Write ProviderWizard tests: required fields、invalid slug/base URL、duplicate submit blocked、masked credential not resubmitted、test failure actionable。
- [ ] Implement wizard steps and model selection；new synced models default disabled。
- [x] Implement list loading/empty/error/success、filter、pagination、enable confirmations。
当前进度：Core 已实现同步模型默认禁用、模型控制面列表/筛选/分页、乐观锁启用/禁用、能力覆盖、手工模型创建、模型参数覆盖和手工模型软删除；Data Plane `GET /v1/models` 仅暴露 Provider 与模型均可路由的已启用模型。桌面端已通过受限 Tauri Commands 提供 OpenAI 兼容服务创建、编辑、测试、同步、服务启停、删除确认、模型启停、能力编辑、参数编辑和手工模型管理；编辑请求只允许固定 Provider/Model 路径、乐观锁版本和 allowlist 后的配置，密钥字段为空时保留既有凭据，填写新值时才替换，完整值不回显到列表或浏览器存储。凭据轮换/撤销和真实客户端联调仍未实现。

- [ ] Run:

```powershell
pnpm web:typecheck
pnpm web:lint
pnpm web:test
pnpm core:test
node scripts/check-generated.mjs
```

Expected PASS。Suggested commit: `feat(desktop): manage OpenAI providers and models`。

---

### Task 2.6: Chat 端到端与真实 Provider L3

**Files:**
- Create: `tests/e2e/chat-proxy.ps1`
- Create: `tests/live/openai-compatible-smoke.ps1`
- Create: `tests/live/README.md`
- Modify: `docs/10-requirements-traceability.md`
- Modify: `docs/references.md`

**Interfaces:**
- Produces: L2 Fake E2E、L3 真实 Provider 证据脚本。
- Consumes: built Core、Local Key、Provider control API。

- [ ] `chat-proxy.ps1` starts Core with temp data/MemoryStore test mode、creates local key/provider/model through Control Plane、runs non-stream/text/tool/cancel、then checks SQLite/logs for sentinel secret absence。

当前验证：`apps/core/cmd/aggregation-hub-core/main_test.go` 的 `TestCoreOpenAICompatibleLocalProviderEndToEnd` 已通过真实 Core 进程与 `httptest` Local OpenAI Compatible Fake Provider 验证：模型同步与启用、受 Local Access Key 保护的模型列表、非流式文本、SSE 文本、非流式 Tool、SSE Tool arguments 分片，以及下游关闭响应 Body 后上游请求 Context 取消。该结果仅为 L2（真实 Core + Fake Provider）；独立 PowerShell E2E 脚本、SQLite/日志 Sentinel 扫描和真实 Provider L3 尚未完成。
- [ ] Run L2 script and expect all assertions PASS。
- [ ] `openai-compatible-smoke.ps1` accepts secrets only via process environment `AH_TEST_BASE_URL`/`AH_TEST_API_KEY`/`AH_TEST_MODEL`; it refuses to run if any missing and never echoes them。
- [ ] Run L3 only in controlled local environment；capture date、provider label、model、commands、HTTP status、stream/tool result and redaction scan in a local untracked evidence file。
- [ ] Update compatibility matrix and traceability with achieved level；if L3 unavailable, report exact blocker without claiming success。
- [ ] Run full gate:

```powershell
pnpm check
powershell -NoProfile -File tests/e2e/chat-proxy.ps1
pnpm build:desktop
```

Expected: all static/L2 PASS；L3 separately documented。

Suggested commit: `test: add OpenAI-compatible end-to-end coverage`。

### 补充进度：模型能力覆盖

- [x] `PATCH /internal/v1/models/{id}` 通过 `version` 与强类型 `capability_override` 更新模型能力；未知字段、非布尔值、缺少覆盖对象和版本冲突均被拒绝。
- [x] SQLite 仓储以事务保存覆盖与审计事件；下一次上游模型同步保留覆盖，目录和 Router 使用有效能力。
- [x] 桌面端通过显式 `update_model_capabilities` Tauri Command 提供能力编辑和“恢复上游声明”；WebView 不接触管理令牌或原始数据库 JSON。
- [x] L1 验证：Provider/SQLite/Control Plane 单测、Rust 单测、React 交互测试；未进行真实 Provider、Claude Code 或 Codex 验证。

### 补充进度：手工模型与参数覆盖

- [x] `POST /internal/v1/providers/{providerId}/models` 创建手工模型，默认停用且不被上游同步覆盖。
- [x] `PATCH /internal/v1/models/{id}/limits` 保存上下文长度/最大输出覆盖，空对象恢复上游声明；`DELETE` 仅允许软删除手工模型。
- [x] 新增 SQLite 迁移保存两个参数覆盖列；Core、Tauri 和桌面模型页均使用 typed DTO，不暴露原始数据库 JSON。
- [x] L1 验证覆盖参数解析边界、仓储持久化/同步保护、服务乐观锁、Control Plane 路由、Rust 编译/单测和 React 交互测试；未进行真实 Provider、Claude Code 或 Codex 验证。

## Phase 2 Gate

- [ ] Provider/模型 UI 可完成配置并不泄露凭据。
- [ ] Chat non-stream、SSE、Tool、错误、取消契约通过。
- [ ] 同名模型命名空间隔离通过。
- [ ] 日志/DB/诊断秘密哨兵扫描通过。
- [ ] 至少 L2；发布稳定前达到真实 Provider L3。



