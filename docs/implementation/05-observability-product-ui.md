# Phase 5: Observability and Product UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 完成请求状态、Token/缓存命中率、健康、诊断、Dashboard、请求/用量页面、设置、保留、备份恢复和可访问性，使日常使用不依赖终端。

**Architecture:** Observability 只接收脱敏事件；请求终态在 SQLite 事务内更新 Token 日汇总，UI 通过 Control Plane 分页查询；图表与表格共用同一查询模型；备份和维护由 Core 执行，WebView 不接触任意文件路径。

**Tech Stack:** Go 1.26、SQLite、React Query、Recharts、Zod、Vitest、Tauri Commands、PowerShell E2E。

## Global Constraints

- 不保存 Prompt、回复、Tool 参数、请求 Header 和完整上游错误体。
- Token 未知与真实的零 Token 必须分开；缓存命中率只使用输入与缓存 Token 均已报告的请求。
- 请求状态机只能产生一个终态。
- 诊断导出使用字段和文件 allowlist，不直接打包数据库。
- UI 包含 loading、empty、error、success 状态，并保持键盘和缩放可用。
- 未获用户明确授权时不 Commit、不 Push。

---

### Task 5.1: 请求状态机与持久化

**Requirements:** `FR-OBS-001~004`、`NFR-REL-004`、`AC-005`。

**Files:**
- Create: `apps/core/internal/observability/request_event.go`
- Create: `apps/core/internal/observability/request_event.go`
- Create: `apps/core/internal/observability/stream.go`
- Create: `apps/core/internal/observability/request_state_test.go`
- Create: `apps/core/internal/storage/request_repository.go`
- Create: `apps/core/internal/storage/request_repository_test.go`
- Create: `apps/core/migrations/0002_request_observability.sql` only if Phase 1 schema needs additive changes
- Modify: all three Ingress handlers。

**Interfaces:** Produces `Recorder.Start/MarkStreaming/Complete/Fail/Cancel` and restart recovery。

- [x] **Step 1: 写状态转换失败测试**

已覆盖 pending→streaming→success/fail/cancel、terminal→anything 拒绝、并发完成竞争、流式终态和现有 restart recovery。

- [x] **Step 2: 定义脱敏事件**

`RequestRecord`/`RequestTransition` 只含请求 ID、协议、Provider/模型快照、时间、状态、Token、错误类别和耗时；类型中不提供 Body/Header/Tool 参数字段。

- [x] **Step 3: 实现事务与终态 Finalizer**

Repository 使用事务；三个 Ingress 通过共享的流式终态包装器和生命周期锁保证正好一个终态；启动时继续只把 pending/streaming 更新为 `aborted_by_restart`。Task 5.2 再加入有界异步队列和 `usage_daily` 幂等汇总。

- [x] **Step 4: 验证**

```powershell
cd apps/core
go test ./internal/observability ./internal/storage ./internal/ingress/... -v
```

定向普通测试和 `go vet ./...` 已通过；Windows 本机执行 `-race` 时被 Go race runtime 的 PE 链接错误阻塞，未把它记为通过。Suggested commit when authorized: `feat(observability): persist request lifecycle metadata`。

---

### Task 5.2: Token、缓存命中率与日汇总

**Requirements:** `FR-OBS-002/005`、`NFR-REL-002/004`。

**Files:**
- Create: `apps/core/internal/observability/usage.go`
- Create: `apps/core/internal/observability/usage_test.go`
- Create: `apps/core/internal/storage/usage_repository.go`
- Create: `apps/core/internal/storage/usage_repository_test.go`
- Create: `apps/core/migrations/0003_usage_token_reporting.sql`
- Modify: `apps/core/internal/storage/request_repository.go`
- Modify: `apps/core/internal/storage/migrate_test.go`

**Interfaces:** Produces idempotent UTC daily Token upsert and integer cache-hit-rate basis points; no pricing or cost calculation。

- [x] **Step 1: 写 Token 与缓存命中率失败测试**

覆盖输入/输出/缓存/Reasoning Token、缓存命中率的正常/零/未知/上游不一致/最大整数边界；不使用浮点累计。

- [x] **Step 2: 实现 Token 已报告语义与前向迁移**

新增 Token 已报告计数和可比较缓存分子/分母列。缺失上游 Token 不累计已报告计数，UI 后续必须显示未知而不是 0；既有费用字段和价格表不删除，但 V1 不写入或展示。

- [x] **Step 3: 实现事务日汇总**

请求终态更新成功后，在同一事务内 UPSERT `usage_daily`；同一 Request ID 的第二个终态会被状态机拒绝，因此不会重复累计。

- [x] **Step 4: 执行并发与总门禁验证**

```powershell
cd apps/core
go test ./internal/observability ./internal/storage -v
go vet ./...
go test ./...
cd ../..
pnpm check
pnpm core:test:race
```

2026-08-16 已通过定向单元/迁移/并发汇总、Core `go vet`/`go test`、仓库 `pnpm check` 与 `pnpm core:test:race`。Suggested commit when authorized: `feat(observability): add token usage daily summaries`。

---

### Task 5.3: 脱敏日志与诊断包

**Requirements:** `FR-OBS-004/006`、`NFR-SEC-003/005`、`AC-004`。

**Files:**
- Create: `apps/core/internal/security/redact.go`
- Create: `apps/core/internal/security/redact_test.go`
- Create: `apps/core/internal/observability/logger.go`
- Create: `apps/core/internal/controlplane/diagnostics_handler.go`
- Create: `apps/core/internal/controlplane/diagnostics_handler_test.go`
- Create: `apps/desktop/src/pages/DiagnosticsPage.tsx`
- Create: `apps/desktop/src/pages/DiagnosticsPage.test.tsx`
- Modify: `apps/desktop/src-tauri/src/runtime_commands.rs`
- Create: `tests/e2e/diagnostics-secret-scan.ps1`

**Interfaces:** Produces structured `SafeLogger`, diagnostics ZIP metadata and open-known-directory command。

- [x] **Step 1: 写 Sentinel Secret 失败测试**

已覆盖 Bearer、x-api-key、OAuth code、Tool arguments、Prompt、带 Query URL 和疑似 API Key 的 Sentinel；新增 ZIP 扫描脚本同时验证安全归档通过、含 Sentinel 的归档被拒绝。

- [x] **Step 2: 实现类型与字段级保护**

SafeLogger 仅接收受限错误元数据，并通过 RequestRecorder 装饰器接入失败终态；URL 去除 UserInfo、Query 和 Fragment，危险文本与标识符拒绝或脱敏，ZIP 二次扫描作为纵深防御。

- [x] **Step 3: 实现诊断 allowlist**

已实现精确 ZIP allowlist：运行时、迁移、CredentialStore Probe、Provider Health、最近安全错误和 manifest；拒绝未知、缺失、路径穿越与包含 Sentinel 的条目，不导出数据库、完整配置或任意路径。

- [x] **Step 4: 验证 ZIP 与 UI**

```powershell
cd apps/core
go test ./internal/security ./internal/observability ./internal/controlplane -v -race
cd ../..
pnpm web:test
powershell -NoProfile -File tests/e2e/diagnostics-secret-scan.ps1
```

2026-08-16 已通过 Core 诊断/Control Plane/安全日志定向测试、ZIP 安全与拒绝 Sentinel 工件检查、OpenAPI lint、`pnpm check`（Desktop 22 项测试、Go 全量测试、Rust 17 项测试）与 `pnpm core:test:race`。证据为 L1；未执行真实 Provider、Claude Code、Codex 或 OAuth。Suggested commit when authorized: `feat(diagnostics): export redacted runtime evidence`。

---

### Task 5.4: Dashboard、Requests 和 Usage UI

**Requirements:** `FR-OBS-001~005`、`NFR-UX-001/002/004`。

**Files:**
- Create: `apps/core/internal/controlplane/request_handler.go`
- Create: `apps/core/internal/controlplane/usage_handler.go`
- Create: `apps/core/internal/controlplane/observability_handler_test.go`
- Modify: `contracts/control-plane.openapi.yaml`
- Create: `apps/desktop/src/pages/DashboardPage.tsx`
- Create: `apps/desktop/src/pages/RequestListPage.tsx`
- Create: `apps/desktop/src/components/RequestDetailDrawer.tsx`
- Create: `apps/desktop/src/pages/UsagePage.tsx`
- Create: related query、schema and test files under `apps/desktop/src/features/observability/`

**Interfaces:** Produces cursor-paginated request list, request metadata detail and time-series/summary endpoints。

- [x] **Step 1: 写 Control API 失败测试**

覆盖 `page_size` 上限、稳定 Cursor、Sort allowlist、时区、未知 Token/缓存命中率单列、无 Body/Header/Tool 参数和无 N+1 Query。

- [x] **Step 2: 实现分页查询与 OpenAPI**

只使用索引列过滤；Cursor 包含稳定排序键；契约生成类型后执行漂移检查。

- [x] **Step 3: 写 UI 四态和可访问性测试**

覆盖筛选、分页、未知 Token/缓存命中率 `—`、无正文 Detail、图表表格替代、键盘焦点和错误重试。

- [x] **Step 4: 实现页面**

沿用既有 feature hook 管理 Server State；Filter URL/State 不含 Secret；图表和表格使用同一已验证数据模型。用户明确要求不做费用统计，页面和契约不包含成本字段。

- [x] **Step 5: 验证**

```powershell
pnpm contracts:check
pnpm core:test
pnpm web:typecheck
pnpm web:lint
pnpm web:test
```

定向普通测试和 `go vet ./...` 已通过；Windows 本机执行 `-race` 时被 Go race runtime 的 PE 链接错误阻塞，未把它记为通过。Suggested commit when authorized: `feat(desktop): add request and usage observability pages`。

---

### Task 5.5: Provider 健康与模型能力测试

**Requirements:** `FR-PROV-006/007`、`FR-CONN-004`、`FR-MODEL-004~007`。

**Files:**
- `apps/core/internal/provider/health_service.go`：受限错误码与健康状态转换。
- `apps/core/internal/storage/health_repository.go`：最近七天的脱敏检查记录。
- `apps/core/internal/management/provider_operations.go`：仅在用户显式点击测试后记录健康结果并更新可推导状态。
- `apps/core/internal/controlplane/provider_health_handler.go`：受管理令牌保护的近期健康记录只读接口。
- `apps/desktop/src-tauri/src/{core_process,runtime_commands}.rs`、`apps/desktop/src/lib/desktop-api.ts`：受类型约束的 Desktop bridge。
- `apps/desktop/src/app/App.tsx`、`apps/desktop/src/components/ProviderHealthDialog.tsx`：按需读取的“记录”弹窗；不在页面加载时自动测试或读取。
- `contracts/control-plane.openapi.yaml`：`GET /internal/v1/providers/{id}/health` 契约。

**实际接口边界：** V1 的控制面“测试”动作固定调用 Adapter 的 `models` 测试；当前 OpenAI-compatible Adapter 通过读取模型列表验证连接和鉴权，Anthropic-compatible Adapter 明确返回 `unsupported_feature`。`connection` 与 `chat` 仍是 Adapter 内部测试种类，尚未暴露为单独的控制面操作；流式与工具测试及原生协议 Adapter 继续按用户决定留到全部 V1 功能完成后再评估。

- [x] **Step 1: 写安全错误分类与状态转换测试**

覆盖认证失败、CredentialStore 不可用、未支持能力、用户取消、未知错误码降级为 allowlist 内的安全分类，以及停用 Provider 不被健康检查重新启用。记录中只保存固定错误码、检查类型、延迟和时间，不保存上游正文、Header 或凭据。

- [x] **Step 2: 实现显式测试、保留与状态写入**

测试请求保留调用 Context 并由 Adapter/Transport 处理取消；页面不会自动发起测试。成功恢复为 `enabled`，认证/凭据问题转为 `auth_required`，其他可用性问题转为 `degraded`；`unsupported_feature` 与 `cancelled` 仅记录为 `skipped`，不改变 Provider 状态，也不删除模型。健康记录由 SQLite 仓储自动裁剪为最近七天。

- [x] **Step 3: 实现健康记录 UI 与能力覆盖入口**

服务列表提供显式“测试”“记录”“同步模型”操作；“记录”仅在用户打开弹窗时通过 WebView → Tauri → Control Plane 获取最近 20 条记录，并处理加载、空、错误和成功状态。模型能力覆盖仍使用现有的显式编辑弹窗，可恢复为上游声明。

- [x] **Step 4: 验证**

```powershell
pnpm --dir apps/desktop exec redocly lint ..\..\contracts\control-plane.openapi.yaml
pnpm web:typecheck
pnpm web:lint
pnpm web:test
pnpm core:vet
pnpm core:test
pnpm rust:test
pnpm docs:check
```

Expected: PASS。当前最高证据等级为 L1：Fake/单元、真实临时 SQLite、Control Plane、Rust bridge 与 React UI 测试通过；未执行真实 Provider、Claude Code、Codex 或 OAuth。

**实施记录（2026-08-16）：** 已完成本任务当前 V1 范围；`pnpm check`、契约 lint 和 `pnpm core:test:race` 均通过。网络 DNS/TLS 细分错误仍由 Transport/Adapter 后续细化；当前 Adapter 未产出时会收敛为 `upstream_unavailable`，不得将此实现描述为真实 Provider 兼容性证明。Suggested commit when authorized: `feat(provider): add health and capability testing`。
---

### Task 5.6: 设置、数据保留、备份恢复

**Requirements:** `NFR-REL-002/003`、`NFR-MAIN-003`、`AC-005`、`NFR-UX-003/004`。

**Files:**
- Create: `apps/core/internal/storage/retention.go`
- Create: `apps/core/internal/storage/retention_test.go`
- Create: `apps/core/internal/storage/backup.go`
- Create: `apps/core/internal/storage/backup_test.go`
- Create: `apps/core/internal/controlplane/settings_handler.go`
- Create: `apps/core/internal/controlplane/maintenance_handler.go`
- Create: `apps/desktop/src/pages/SettingsPage.tsx`
- Create: `apps/desktop/src/pages/SettingsPage.test.tsx`
- Modify: `contracts/control-plane.openapi.yaml`
- Create: `tests/e2e/backup-restore.ps1`

**Interfaces:** Produces prune、backup、restore and restart-required settings。

- [ ] **Step 1: 写保留与恢复失败测试**

覆盖 Aggregate-before-delete、Batch Size、幂等、Audit、Cancel、WAL Checkpoint、迁移失败、Foreign Key 失败、磁盘满和权限失败。

- [ ] **Step 2: 实现受控备份**

备份写入应用已知目录，保留最近五份；Restore 前拒绝新请求并先备份当前数据库；失败不得 reset。

- [ ] **Step 3: 实现设置验证**

Port 1024~65535、Timeout 和 Retention 有上下限；V1 Host 不可编辑；Port Change 返回 `restart_required`。

- [ ] **Step 4: 实现危险操作 UI**

Restore、Clear、Rotate 和 Delete 使用二次确认，禁用重复提交，并在失败时提供恢复建议。

- [x] **Step 5: 验证**

```powershell
pnpm contracts:check
pnpm core:test
pnpm web:test
powershell -NoProfile -File tests/e2e/backup-restore.ps1
```

Expected: PASS；故障注入后原数据库和最近备份仍可恢复。Suggested commit when authorized: `feat(core): add retention backup and settings maintenance`。

---

### Task 5.7: 完整桌面 E2E 与可访问性

**Requirements:** `NFR-UX-001~004`、`FR-DESK-001~006`、`AC-003~005`。

**Files:**
- Create: `tests/e2e/desktop-workflow.ps1`
- Create: `tests/e2e/accessibility-checklist.md`
- Create: frontend accessibility tests in affected page folders
- Modify: `docs/09-testing-strategy.md`
- Modify: `docs/10-requirements-traceability.md`

**Interfaces:** Produces release-like Desktop E2E and accessibility evidence。

- [ ] **Step 1: 自动化主要用户流程**

覆盖首次启动、一次性 Key、Provider Wizard、模型启用、连接模板、请求列表、Usage、Diagnostics、Restart Persistence 和 Tray Hide/Restore。

- [ ] **Step 2: 验证可访问性**

覆盖键盘导航、Focus Trap/Restore、可见 Label、非颜色状态、200% Zoom 和 Reduced Motion。

- [ ] **Step 3: 验证故障恢复**

Core Crash 后 Desktop 按 1/3/10 秒重试，超过上限进入 failed；用户显式退出不触发重启。

- [ ] **Step 4: 运行总门禁**

```powershell
pnpm check
powershell -NoProfile -File tests/e2e/chat-proxy.ps1
powershell -NoProfile -File tests/e2e/desktop-workflow.ps1
pnpm build:desktop
```

Expected: PASS，并更新证据矩阵。Suggested commit when authorized: `test: cover complete desktop management workflow`。

## Phase 5 Gate

- [ ] 日常管理不依赖终端。
- [ ] 请求、Token/缓存命中率和健康数据正确且不含正文。
- [ ] 诊断包和日志 Sentinel 扫描通过。
- [ ] 备份恢复和保留任务可恢复、可取消。
- [ ] UI 四态、键盘、焦点、缩放和 Reduced Motion 通过。
- [ ] `pnpm check` 与 Phase 5 E2E 全部通过。

