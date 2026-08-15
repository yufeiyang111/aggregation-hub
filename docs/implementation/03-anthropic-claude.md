# Phase 3: Anthropic Messages and Claude Code Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Anthropic Messages 入口与上游 Adapter，并让真实 Claude Code 通过本地网关完成流式文本、Tool Calling 和取消，达到 L4。

**Architecture:** Anthropic Ingress 与 Adapter 分别转换到/从 Normalized Contract；Messages SSE 的事件顺序由独立状态机生成；Claude Code 配置模板由受测试的纯函数生成。

**Tech Stack:** Go 1.26、Anthropic Messages/SSE、React 19、TypeScript 5.9.3、Vitest、PowerShell live smoke。

## Global Constraints

- 实施时重新核对当前 Anthropic 与 Claude Code 官方文档，并把核对日期写入 `docs/references.md`。
- Beta、Thinking 等 Header 必须使用 allowlist，不得盲目透传。
- Tool Use/Tool Result ID、内容块索引和事件顺序必须严格保持。
- Claude Code L4 必须来自真实客户端进程；Fake Client 只能提供 L1/L2。
- 不保存 Prompt、响应正文、Tool 参数或完整上游错误体。
- 未获用户明确授权时不 Commit、不 Push。

---

### Task 3.1: Anthropic Messages Ingress

**Requirements:** `FR-API-001`、`FR-API-007~010`、`FR-MODEL-004~006`、`NFR-REL-001/004`。

**Files:**
- Create: `apps/core/internal/ingress/anthropic/request.go`
- Create: `apps/core/internal/ingress/anthropic/normalize.go`
- Create: `apps/core/internal/ingress/anthropic/response.go`
- Create: `apps/core/internal/ingress/anthropic/handler.go`
- Create: `apps/core/internal/ingress/anthropic/handler_test.go`
- Modify: `apps/core/cmd/aggregation-hub-core/main.go`
- Modify: `docs/04-api-design.md`
- Modify: `docs/references.md`

> 修正：`control-plane.openapi.yaml` 仅描述 Tauri Rust 与 Core 的内部控制面；`/v1/messages` 属于 Data Plane，不能混入该契约。

**Interfaces:** Produces `POST /v1/messages`；consumes Local Auth、Router、Normalized Contract。

**2026-08-15 状态：** 已完成非流式 text、tool_use、tool_result 纵向切片与严格输入边界（L1）；`stream` 和 Thinking 输出将在 Task 3.3 完成前显式返回 `unsupported_feature`，不得描述为 Claude Code 兼容已完成。

**实施记录（2026-08-15）：**

- 已完成严格 DTO、大小限制、system/text/tool_use/tool_result 归一化、非流式 Anthropic message 序列化，以及 Data Plane 路由注册。
- 已通过：`go test ./internal/ingress/anthropic -v`、`go test ./cmd/aggregation-hub-core ./internal/ingress/anthropic -v`、`pnpm check`。
- `go test -race` 在本机 Windows Go Race 链接阶段因 `windynrelocsym` / `__imp_*` 工具链错误失败，且同样影响未改动的 `openai_chat`、`routing` 包；该项保持未验证，未通过修改系统环境或降低测试门禁绕过。

- [ ] **Step 1: 写失败契约测试**

覆盖 system string/block、user/assistant content、tool_use、tool_result、stream、`max_tokens` 必填、Thinking 能力、错误角色和错误块顺序。先运行：

```powershell
cd apps/core
go test ./internal/ingress/anthropic -v -race
```

Expected: FAIL，原因是 DTO、Normalizer 和 Handler 尚未实现。

- [ ] **Step 2: 实现严格请求边界**

定义显式 DTO 和 union，不在主路径使用无约束 `map[string]any`；应用请求体大小、数组长度、字符串长度、Tool Schema 深度和总内容块数量限制；复用共享鉴权中间件支持 `x-api-key` 与 Bearer。

- [ ] **Step 3: 实现 NormalizedRequest 转换**

System 单独归一化；Tool Use/Result 映射内部 ID；从 Thinking、Tool、Vision 字段推导所需能力；不支持能力在调用上游前返回稳定错误码。

- [ ] **Step 4: 实现非流式输出和安全错误**

保持 content block 顺序，映射 stop reason；未知 Usage 保持 `nil`；错误响应只返回安全类别、请求 ID 和可执行建议。

- [ ] **Step 5: 验证**

```powershell
cd apps/core
go test ./internal/ingress/anthropic -v -race
go test ./internal/ingress/... ./internal/routing/... -race
```

Expected: PASS。Suggested commit when authorized: `feat(api): add Anthropic messages ingress`。

---

### Task 3.2: Anthropic Compatible Adapter

**Requirements:** `FR-PROV-002/003/006/008`、`FR-API-008`、`NFR-MAIN-001/002`。

**Files:**
- Create: `apps/core/internal/adapter/anthropic/config.go`
- Create: `apps/core/internal/adapter/anthropic/request.go`
- Create: `apps/core/internal/adapter/anthropic/response.go`
- Create: `apps/core/internal/adapter/anthropic/adapter_test.go`
- Create: `apps/core/testdata/anthropic/`

**Interfaces:** Produces `anthropic-compatible` Adapter；consumes RoutePlan、CredentialStore、safe Transport。

**2026-08-15 状态：** 已完成非流式请求/响应映射、`anthropic-version` 与受控认证头注入（L1）。自动模型发现、SSE、Thinking 输出与真实 Provider 验证尚未完成；Adapter 将流式和自动发现明确标记为未支持，不读取或保存完整凭据。

- [ ] **Step 1: 写 Fixture 契约测试**

加入 text、system、tool_use、tool_result、thinking、usage、401、429、5xx 和畸形 JSON Fixture；验证完整秘密和 Tool 参数不会进入错误或日志。

- [ ] **Step 2: 定义 Adapter 配置**

配置包含 messages path、`anthropic-version`、认证 Header 模式、Beta Header allowlist 和超时；JSON 配置禁止出现 Secret 值，只允许 Credential Reference。

- [ ] **Step 3: 实现请求转换**

使用 RoutePlan 的 upstream model ID；认证头由 Adapter 独占；自定义 Header 不得覆盖 Host、Content-Length、认证头和受保护版本头。

- [ ] **Step 4: 实现响应转换**

保持内容块顺序、stop reason、usage 来源和未知值语义；对上游 401/429/5xx 使用统一错误分类。

- [ ] **Step 5: 验证**

```powershell
cd apps/core
go test ./internal/adapter/anthropic -v -race
go test ./internal/adapter/... ./internal/transport/... -race
```

Expected: PASS。Suggested commit when authorized: `feat(adapter): add Anthropic-compatible requests`。

---

### Task 3.3: Anthropic SSE、Tool 与 Thinking

**Requirements:** `FR-API-007~010`、`NFR-REL-001/004`、`NFR-PERF-001~003`。

**Files:**
- Create: `apps/core/internal/adapter/anthropic/stream.go`
- Create: `apps/core/internal/adapter/anthropic/stream_test.go`
- Create: `apps/core/internal/ingress/anthropic/stream.go`
- Create: `apps/core/internal/ingress/anthropic/stream_test.go`
- Create: `apps/core/testdata/anthropic/stream/`

**Interfaces:** Produces valid Anthropic SSE sequence；consumes `NormalizedEvent` and cancellation context。

- [ ] **Step 1: 写任意分块失败测试**

Fixture 覆盖 `message_start`、`content_block_start`、`text_delta`、`input_json_delta`、`thinking_delta`、`content_block_stop`、`message_delta`、`message_stop`，并在任意 TCP 边界切块。

- [ ] **Step 2: 写事件状态机测试**

断言单一 start/stop、内容块索引递增、Tool JSON 在块结束后才解析、截断流产生 typed error、terminal event 只能出现一次。

- [ ] **Step 3: 实现上下游状态机**

上游 Parser 转成 `NormalizedEvent`，Ingress Serializer 再输出 Anthropic SSE；不得缓存完整响应或记录 Tool 参数。

- [ ] **Step 4: 验证取消和背压**

测试客户端取消在目标时间内关闭上游 Body；首个事件输出后不得重试；慢客户端不会造成无界内存增长。

- [ ] **Step 5: 验证**

```powershell
cd apps/core
go test ./internal/adapter/anthropic ./internal/ingress/anthropic -v -race
```

Expected: PASS。Suggested commit when authorized: `feat(stream): support Anthropic tools and thinking`。

---

### Task 3.4: Claude Code 配置生成与 UI

**Requirements:** `FR-CONN-001/003/004`、`FR-KEY-003~005`、`NFR-UX-001~004`。

**Files:**
- Create: `apps/desktop/src/features/connections/claudeConfig.ts`
- Create: `apps/desktop/src/features/connections/claudeConfig.test.ts`
- Create: `apps/desktop/src/pages/ClaudeSetupPage.tsx`
- Create: `apps/desktop/src/pages/ClaudeSetupPage.test.tsx`
- Modify: `apps/desktop/src/app/router.tsx`
- Modify: `docs/references.md`

**Interfaces:** Produces copyable Base URL/Auth/Model templates and local/full-chain test actions。

- [ ] **Step 1: 刷新官方配置证据**

重新核对 Claude Code 当前网关、环境变量和模型设置文档，在 `docs/references.md` 记录 URL、核对日期和适用版本；若配置字段改变，先更新设计和快照。

- [ ] **Step 2: 写纯函数快照测试**

覆盖模板变量 `${AGGREGATION_HUB_API_KEY}`、本地 Base URL、选中的 Public Model ID、PowerShell 示例和不自动写第三方配置文件的约束。

- [ ] **Step 3: 实现配置生成器与页面四态**

UI 只组合已验证的生成器结果；提供复制按钮、键盘可达控件、错误提示、空状态和成功状态；已有 Key 只展示 prefix/suffix，新 Key 仅支持一次完整复制。

- [ ] **Step 4: 分离测试按钮**

分别提供 `/health`、本地鉴权、Messages 文本和 Tool Calling 测试，并明确显示 L1/L2/L3/L4 证据等级。

- [ ] **Step 5: 验证**

```powershell
pnpm web:typecheck
pnpm web:lint
pnpm web:test
```

Expected: PASS。Suggested commit when authorized: `feat(desktop): add Claude Code connection guide`。

---

### Task 3.5: Claude Code L4 真实验证

**Requirements:** `AC-001`、`FR-CONN-004`、`NFR-SEC-003/005`。

**Files:**
- Create: `tests/live/claude-code-smoke.ps1`
- Modify: `tests/live/README.md`
- Modify: `docs/10-requirements-traceability.md`
- Modify: `docs/references.md`

**Interfaces:** Produces repeatable L4 evidence；consumes installed Claude Code、real Provider、local Core。

- [ ] **Step 1: 实现安全前置检查**

脚本验证 Claude Code 可执行文件、版本、所需环境变量、Core 和端口；不得输出凭据值。临时 Claude Code 配置和工作区必须位于测试目录，不修改用户生产配置。

- [ ] **Step 2: 运行真实流式与 Tool 场景**

使用无敏感数据的临时仓库完成一次流式编码请求和至少一次 Tool 操作；记录客户端退出码、请求 ID、Provider/模型标签和结构化结果。

- [ ] **Step 3: 运行取消场景**

取消运行中的真实请求，验证 Core 请求状态为 `cancelled`，上游 Body 已关闭，且没有第二个终态或重试。

- [ ] **Step 4: 扫描敏感数据**

扫描 Core/Desktop 日志、SQLite、诊断包和测试输出，确认无 Sentinel Secret、Prompt 正文、Tool 参数和完整上游错误体。

- [ ] **Step 5: 记录证据并执行总门禁**

记录真实执行日期、Claude Code 版本、命令、Provider/模型、结果和限制到本地未跟踪证据文件；公开矩阵只写脱敏摘要。运行：

```powershell
pnpm check
powershell -NoProfile -File tests/live/claude-code-smoke.ps1
```

Expected: L4 PASS；否则保存精确阻塞原因，不声明 Claude Code 兼容。Suggested commit when authorized: `test: add Claude Code live compatibility workflow`。

## Phase 3 Gate

- [ ] Messages non-stream/stream、Tool、Thinking、错误和取消契约通过。
- [ ] 配置生成器与当前官方字段一致，并有核对日期。
- [ ] 真实 Claude Code 达到 L4，或存在明确、可复核的阻塞报告。
- [ ] 日志、SQLite、诊断和测试输出未持久化秘密或请求正文。
- [ ] `pnpm check` 与 Phase 3 最小命令全部通过。