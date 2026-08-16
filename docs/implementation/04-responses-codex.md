# Phase 4: OpenAI Responses and Codex Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 OpenAI Responses 入口、Function Call/Output、Reasoning 和流式事件，并让真实 Codex 通过自定义 Provider 达到 L4。

**Architecture:** Responses 不是 Chat Completions 的字段改名；它拥有独立 Ingress 和 Serializer，但与其他入口共享 Normalized Contract、Router、Adapter、Transport 和 Observability。

**Tech Stack:** Go 1.26、OpenAI Responses、Codex config、React 19、TypeScript 5.9.3、PowerShell live smoke。

## Global Constraints

- 实施时重新核对当前 OpenAI Responses 与 Codex 官方文档，并记录核对日期和适用版本。
- 不把 Chat Completions 兼容当作 Codex 完整兼容。
- Function Call ID、`call_id` 和 Function Call Output 必须可逆映射。
- Reasoning 未支持时明确拒绝，不得混入普通文本或伪造内容。
- 流式开始后不得重试或切换 Provider；未知 Usage 保持未知。
- 未获用户明确授权时不 Commit、不 Push。

---

### Task 4.1: Responses Ingress DTO 与规范化

**Requirements:** `FR-API-002/007~010`、`FR-MODEL-004~006`、`NFR-MAIN-002`。

**Files:**
- Create: `apps/core/internal/ingress/openai_responses/request.go`
- Create: `apps/core/internal/ingress/openai_responses/normalize.go`
- Create: `apps/core/internal/ingress/openai_responses/handler.go`
- Create: `apps/core/internal/ingress/openai_responses/handler_test.go`
- Modify: `apps/core/internal/dataplane/server.go`
- Modify: `contracts/control-plane.openapi.yaml`

**Interfaces:** Produces `POST /v1/responses`；consumes Normalized Contract and Router。

- [x] **Step 1: 写失败 DTO 测试**

覆盖 string/input item、instructions、function tool、function_call_output、Reasoning、stream、`max_output_tokens`、未知项和 V1 不支持项。

- [x] **Step 2: 实现严格输入模型**

使用显式 item union 和请求大小限制；当前支持文本、function_call、function_call_output。hosted tool、file、image、Reasoning 和 stream 暂返回结构化不支持错误，不静默丢弃。

- [x] **Step 3: 实现 NormalizedRequest 转换**

Instructions 映射到 System；Function Call/Result 通过 `call_id` 关联；从 Reasoning 和 Tool 字段推导 Required Capabilities。

- [ ] **Step 4: 验证**

```powershell
cd apps/core
go test ./internal/ingress/openai_responses -v -race
go test ./internal/ingress/... ./internal/routing/... -race
```

定向验证已通过：`go test ./internal/ingress/openai_responses -v`。本阶段只完成入口和规范化；Responses 上游 Adapter、非流式响应序列化和流式事件仍待后续任务。Suggested commit: `feat(api): normalize OpenAI Responses requests`。

---

### Task 4.2: Responses 非流式输出与 OpenAI Adapter Wire API

**Requirements:** `FR-PROV-001/003`、`FR-API-002/008`、`NFR-MAIN-001/002`。

**Files:**
- Create: `apps/core/internal/ingress/openai_responses/response.go`
- Create: `apps/core/internal/ingress/openai_responses/response_test.go`
- Modify: `apps/core/internal/adapter/openai/request.go`
- Modify: `apps/core/internal/adapter/openai/response.go`
- Modify: `apps/core/internal/adapter/openai/config_test.go`
- Create: `apps/core/testdata/openai/responses/`

**Interfaces:** Produces Responses JSON；OpenAI Adapter supports `wire_api=responses` independently from Chat Completions。

- [ ] **Step 1: 加入非流式 Fixture**

覆盖 output_text、function_call、Reasoning summary、Usage、incomplete、failed、401、429、5xx 和未知字段。

- [ ] **Step 2: 实现 Responses Request Builder**

目标路径为 `/v1/responses`；不得先转换为 Chat DTO；Provider Secret 只由 CredentialStore 注入。

- [ ] **Step 3: 实现上游与入口转换**

上游 Responses 转为 `NormalizedResponse`，入口再序列化为 response object/status/output items；未知 Usage 使用 `nil`，失败映射稳定安全错误码。

- [ ] **Step 4: 验证两个 Wire API**

```powershell
cd apps/core
go test ./internal/adapter/openai ./internal/ingress/openai_responses -v -race
```

Expected: Chat Completions 与 Responses 契约均 PASS。Suggested commit when authorized: `feat(adapter): support OpenAI Responses wire protocol`。

---

### Task 4.3: Responses 流式事件、Function 与 Reasoning

**Requirements:** `FR-API-007~010`、`NFR-REL-001/004`、`NFR-PERF-001~003`。

**Files:**
- Create: `apps/core/internal/adapter/openai/responses_stream.go`
- Create: `apps/core/internal/adapter/openai/responses_stream_test.go`
- Create: `apps/core/internal/ingress/openai_responses/stream.go`
- Create: `apps/core/internal/ingress/openai_responses/stream_test.go`
- Create: `apps/core/testdata/openai/responses/stream/`

**Interfaces:** Produces current Responses SSE event names from `NormalizedEvent`。

- [ ] **Step 1: 刷新官方事件 Fixture**

重新核对官方流式事件文档，在 `docs/references.md` 记录核对日期；将实际事件名称和最小 JSON 结构写入 testdata，而不是凭记忆实现。

- [ ] **Step 2: 写任意 TCP 分块测试**

覆盖 response.created、output item added/done、text delta、Reasoning summary delta、Function arguments delta、completed、error 和 truncated stream。

- [ ] **Step 3: 实现独立状态机**

上游 Parser 与入口 Serializer 分离；Function arguments 只在完成时校验 JSON；保持 `call_id` 映射；terminal event 只能出现一次。

- [ ] **Step 4: 测试取消与背压**

客户端断开后取消上游 Context；慢客户端使用有界缓冲；首个 SSE 事件发送后不重试。

- [ ] **Step 5: 验证**

```powershell
cd apps/core
go test ./internal/adapter/openai ./internal/ingress/openai_responses -v -race
```

Expected: PASS。Suggested commit when authorized: `feat(stream): support Responses functions and reasoning`。

---

### Task 4.4: Codex 配置生成与 UI

**Requirements:** `FR-CONN-002~004`、`FR-KEY-003~005`、`NFR-UX-001~004`。

**Files:**
- Create: `apps/desktop/src/features/connections/codexConfig.ts`
- Create: `apps/desktop/src/features/connections/codexConfig.test.ts`
- Create: `apps/desktop/src/pages/CodexSetupPage.tsx`
- Create: `apps/desktop/src/pages/CodexSetupPage.test.tsx`
- Modify: `apps/desktop/src/app/router.tsx`
- Modify: `docs/references.md`

**Interfaces:** Produces current `config.toml` Provider snippet and environment-variable instructions。

- [ ] **Step 1: 刷新官方配置证据**

核对 Codex 当前自定义 Provider 配置；把 `model_providers.<id>.base_url`、认证环境变量和 `wire_api="responses"` 等字段记录为带日期 Fixture。

- [ ] **Step 2: 写纯函数快照测试**

使用 Provider ID `aggregation_hub`、本地 URL、环境模板变量和 Public Model ID；覆盖 PowerShell 示例与转义规则。

- [ ] **Step 3: 实现 UI 四态和安全复制**

提供配置复制、本地鉴权、Responses 文本和 Function 测试；不自动写用户配置；已有 Local Key 不重新展示，新 Key 只允许一次完整复制。

- [ ] **Step 4: 验证**

```powershell
pnpm web:typecheck
pnpm web:lint
pnpm web:test
```

Expected: PASS。Suggested commit when authorized: `feat(desktop): add Codex connection guide`。

---

### Task 4.5: Codex L4 真实验证

**Requirements:** `AC-002`、`FR-CONN-004`、`NFR-SEC-003/005`。

**Files:**
- Create: `tests/live/codex-smoke.ps1`
- Modify: `tests/live/README.md`
- Modify: `docs/10-requirements-traceability.md`
- Modify: `docs/references.md`

**Interfaces:** Produces repeatable L4 evidence；consumes installed Codex、real Responses-compatible Provider、local Core。

- [ ] **Step 1: 实现安全前置检查**

验证 Codex 可执行文件、版本和所需环境变量，不输出 Secret；创建临时 Codex home/config 指向 Aggregation Hub，不修改用户生产配置。

- [ ] **Step 2: 运行真实 Function 场景**

使用无敏感数据的临时仓库执行流式编码请求和至少一次 Function/Tool 操作，记录请求 ID、Provider/模型标签、退出码和结果。

- [ ] **Step 3: 运行取消场景**

取消运行中的请求，断言 Core 状态为 `cancelled`、上游 Body 关闭、无重试和重复终态。

- [ ] **Step 4: 扫描敏感数据**

扫描日志、SQLite、诊断包和脚本输出，确认无 Sentinel Credential、Prompt 和 Function arguments。

- [ ] **Step 5: 记录证据并执行总门禁**

记录真实执行日期、Codex 版本、命令、Provider/模型、结果和限制到本地未跟踪证据文件；公开矩阵仅写脱敏摘要。运行：

```powershell
pnpm check
powershell -NoProfile -File tests/live/codex-smoke.ps1
```

Expected: L4 PASS；否则形成精确阻塞报告，不得用自写客户端替代 Codex。Suggested commit when authorized: `test: add Codex live compatibility workflow`。

## Phase 4 Gate

- [ ] Responses non-stream/stream、Function、Reasoning、Usage、错误和取消契约通过。
- [ ] Codex 模板与当前官方配置一致，并有核对日期。
- [ ] 真实 Codex 达到 L4，或存在明确、可复核的阻塞报告。
- [ ] Chat Completions 与 Responses Adapter 保持独立测试。
- [ ] `pnpm check` 与 Phase 4 最小命令全部通过。