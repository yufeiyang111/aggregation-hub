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

- [x] **Step 1: 加入非流式 Fixture**

已加入完整响应、incomplete、failed、reasoning 与未知输出夹具；401、429、5xx 通过上游错误映射测试覆盖。

- [x] **Step 2: 实现 Responses Request Builder**

目标路径为 `/v1/responses`；不得先转换为 Chat DTO；Provider Secret 只由 CredentialStore 注入。

- [x] **Step 3: 实现上游与入口转换**

上游 Responses 转为 `NormalizedResponse`，入口再序列化为 response object/status/output items；未知 Usage 使用 `nil`，失败映射稳定安全错误码，并且不向客户端泄漏上游错误正文。

- [x] **Step 4: 验证两个 Wire API**

```powershell
cd apps/core
go test ./internal/adapter/openai ./internal/ingress/openai_responses -v
```

Expected: Chat Completions 与 Responses 契约均 PASS。实际已完成定向测试和仓库全量门禁；Windows race 链接问题保留为独立环境风险。

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

- [x] **Step 1: 刷新官方事件 Fixture**

已核对 OpenAI 官方 Streaming Responses 文档并在 `docs/references.md` 记录 2026-08-16；夹具记录实际事件名称和最小 JSON 结构。

- [x] **Step 2: 写任意 TCP 分块测试**

已覆盖 response.created、输出项新增/完成、text delta、Reasoning summary 的结构化拒绝、Function arguments delta、completed、incomplete、error、未知未来事件和 truncated stream。

- [x] **Step 3: 实现独立状态机**

上游 Parser 与入口 Serializer 分离；Function arguments 只在完成时校验 JSON；保持 `call_id` 映射；terminal event 只能出现一次。

- [x] **Step 4: 测试取消与背压**

客户端断开会取消上游 Context；Parser 同步等待 Emitter，避免无界缓冲；首个 SSE 事件发送后不重试，终态后不追加第二个错误事件。

- [x] **Step 5: 验证**

```powershell
cd apps/core
go test ./internal/adapter/openai ./internal/ingress/openai_responses -v
```

Expected: PASS。定向测试与仓库全量门禁已通过；Windows race 链接问题保留为独立环境风险。

---

### Task 4.4: Codex 配置生成与 UI

**Requirements:** `FR-CONN-002~004`、`FR-KEY-003~005`、`NFR-UX-001~004`。

**Files:**
- Create: `apps/desktop/src/features/connections/codexConfig.ts`
- Create: `apps/desktop/src/features/connections/codexConfig.test.ts`
- Create: `apps/desktop/src/features/connections/CodexSetupPage.tsx`
- Create: `apps/desktop/src/features/connections/CodexSetupPage.test.tsx`
- Modify: `apps/desktop/src/app/App.tsx`（当前导航事实源）
- Modify: `docs/references.md`

**Interfaces:** Produces current `config.toml` Provider snippet and environment-variable instructions。

- [x] **Step 1: 刷新官方配置证据**

已核对 Codex 官方 Config Reference 并在 `docs/references.md` 记录 2026-08-16；受测生成器固定 `model_provider`、`base_url`、`env_key`、`requires_openai_auth=false` 和 `wire_api="responses"`。

- [x] **Step 2: 写纯函数快照测试**

使用 Provider ID `aggregation_hub`、本地 URL、环境模板变量和 Public Model ID；覆盖 PowerShell 示例与转义规则。

- [x] **Step 3: 实现 UI 四态和安全复制**

提供配置复制、临时 PowerShell 环境变量、本地鉴权和用户显式触发的 Responses 文本/Function 诊断；诊断仅允许回环地址并只临时使用当前页面的新建 Key。不会自动写用户配置、系统环境变量或保存秘密/请求正文。

- [x] **Step 4: 验证**

```powershell
pnpm web:typecheck
pnpm web:lint
pnpm web:test
```

Expected: PASS。前端 typecheck/lint/18 项测试、Rust 16 项测试和仓库全量门禁已通过；未执行真实 Codex 或真实 Provider 测试。

---

### Task 4.5: Codex L4 真实验证

**Requirements:** `AC-002`、`FR-CONN-004`、`NFR-SEC-003/005`。

**Files:**
- Create: `tests/live/codex-smoke.ps1`
- Modify: `tests/live/README.md`
- Modify: `docs/10-requirements-traceability.md`
- Modify: `docs/references.md`

**Interfaces:** Produces repeatable L4 evidence；consumes installed Codex、real Responses-compatible Provider、local Core。

- [x] **Step 1: 实现安全前置检查**

已新增 `tests/live/codex-smoke.ps1`：验证 Codex 可执行文件、版本、回环 `/v1` 地址、Public Model ID 和环境变量名称；创建临时 `CODEX_HOME/config.toml` 指向 Aggregation Hub，不修改用户生产配置。默认仅预检，只有显式 `-RunLive` 才读取当前进程中的 Local Access Key。

- [ ] **Step 2: 运行真实 Function 场景**

脚本已提供显式 `-RunLive` 的无敏感数据 Tool/Function 场景：隔离临时工作目录、`codex exec --json`、固定成功标记，以及本地访问密钥输出扫描。尚未使用真实 Provider/Codex 执行，不能声明 L4。

- [ ] **Step 3: 运行取消场景**

取消端到端断言依赖后续可观测性请求追踪；当前不得用终止 Codex 进程替代 Core `cancelled`、上游 Body 关闭、无重试和重复终态的验证。

- [ ] **Step 4: 扫描敏感数据**

扫描日志、SQLite、诊断包和脚本输出，确认无 Sentinel Credential、Prompt 和 Function arguments。

- [ ] **Step 5: 记录证据并执行总门禁**

真实运行时记录执行日期、Codex 版本、命令、Provider/模型、结果和限制到本地未跟踪证据文件；公开矩阵仅写脱敏摘要。当前仅完成安全预检脚本，尚未执行 L4。运行：

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
