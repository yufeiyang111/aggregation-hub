# Phase 5: Observability and Product UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 完成请求状态、Token/费用、健康、诊断、Dashboard、请求/用量页面、设置、保留、备份恢复和可访问性，使日常使用不依赖终端。

**Architecture:** Observability 只接收脱敏事件并通过有界队列写 SQLite；UI 通过 Control Plane 分页查询；图表与表格共用同一查询模型；备份和维护由 Core 执行，WebView 不接触任意文件路径。

**Tech Stack:** Go 1.26、SQLite、React Query、Recharts、Zod、Vitest、Tauri Commands、PowerShell E2E。

## Global Constraints

- 不保存 Prompt、回复、Tool 参数、请求 Header 和完整上游错误体。
- 费用使用整数微美元；未知价格与零费用必须分开。
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

### Task 5.2: 有界队列、费用与日汇总

**Requirements:** `FR-OBS-002/005`、`NFR-PERF-003`、`NFR-REL-002/004`。

**Files:**
- Create: `apps/core/internal/observability/queue.go`
- Create: `apps/core/internal/observability/queue_test.go`
- Create: `apps/core/internal/observability/pricing.go`
- Create: `apps/core/internal/observability/pricing_test.go`
- Create: `apps/core/internal/storage/usage_repository.go`
- Create: `apps/core/internal/storage/usage_repository_test.go`
- Create: `tests/e2e/observability-stress.ps1`

**Interfaces:** Produces non-blocking bounded recorder、integer cost and idempotent daily upsert。

- [ ] **Step 1: 写队列饱和失败测试**

低价值调试事件可丢弃，但 terminal/error 必须同步保留并递增 dropped counter；Shutdown 必须按超时 Drain。

- [ ] **Step 2: 写整数费用测试**

覆盖输入/输出/缓存/Reasoning Token、价格历史生效时间、溢出、未知价格和零价格；禁止浮点累计。

- [ ] **Step 3: 实现事务日汇总**

终态事务内 UPSERT `usage_daily`；重复消费同一 Request ID 不重复累计。

- [ ] **Step 4: 执行并发压力验证**

```powershell
cd apps/core
go test ./internal/observability ./internal/storage -v -race
cd ../..
powershell -NoProfile -File tests/e2e/observability-stress.ps1 -ConcurrentStreams 100
```

Expected: 单元和 Race PASS；压力脚本无丢失终态、重复计费或无界队列。Suggested commit when authorized: `feat(observability): add bounded usage and pricing pipeline`。

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

- [ ] **Step 1: 写 Sentinel Secret 失败测试**

使用 Bearer、x-api-key、OAuth code、Tool arguments、Prompt 和带 Query URL 形态的 Sentinel，先证明未加保护时扫描失败。

- [ ] **Step 2: 实现类型与字段级保护**

结构化字段拒绝 `SecretValue`；URL/Header 使用专用安全格式；Sink Regex 仅作为纵深防御。

- [ ] **Step 3: 实现诊断 allowlist**

只导出版本、运行时、迁移、CredentialStore Probe、Provider Health Summary 和近期安全错误；排除数据库、完整配置和任意路径。

- [ ] **Step 4: 验证 ZIP 与 UI**

```powershell
cd apps/core
go test ./internal/security ./internal/observability ./internal/controlplane -v -race
cd ../..
pnpm web:test
powershell -NoProfile -File tests/e2e/diagnostics-secret-scan.ps1
```

Expected: ZIP 无路径穿越、绝对路径和 Sentinel；UI 只打开受控目录。Suggested commit when authorized: `feat(diagnostics): export redacted runtime evidence`。

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

- [ ] **Step 1: 写 Control API 失败测试**

覆盖 `page_size` 上限、稳定 Cursor、Sort allowlist、时区、未知价格单列、无 Body/Header/Tool 参数和无 N+1 Query。

- [ ] **Step 2: 实现分页查询与 OpenAPI**

只使用索引列过滤；Cursor 包含稳定排序键；契约生成类型后执行漂移检查。

- [ ] **Step 3: 写 UI 四态和可访问性测试**

覆盖筛选、分页、未知费用 `—`、无正文 Detail、图表表格替代、键盘焦点和错误重试。

- [ ] **Step 4: 实现页面**

React Query 管理 Server State；Filter URL/State 不含 Secret；图表和表格使用同一已验证数据模型。

- [ ] **Step 5: 验证**

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
- Create: `apps/core/internal/provider/health_service.go`
- Create: `apps/core/internal/provider/health_service_test.go`
- Create: `apps/core/internal/storage/health_repository.go`
- Create: `apps/core/internal/controlplane/provider_test_handler.go`
- Modify: `contracts/control-plane.openapi.yaml`
- Modify: `apps/desktop/src/pages/ProviderDetailPage.tsx`
- Modify: `apps/desktop/src/pages/ModelDetailPage.tsx`
- Create: related UI tests。

**Interfaces:** Produces connectivity/auth/minimal/stream/tool test kinds and health status transitions。

- [ ] **Step 1: 写错误分类失败测试**

Fake Upstream 覆盖 DNS、TLS、Timeout、Auth、Rate Limit、Protocol、Cancel 和能力不支持。

- [ ] **Step 2: 实现显式可取消测试**

每种 Test Kind 使用独立 Context；不在页面加载时自动触发；失败更新 Provider degraded/auth_required，但不删除模型。

- [ ] **Step 3: 实现 UI 与能力覆盖确认**

每项检查独立显示结果和下一步；手工能力覆盖需要确认并可恢复；健康详情仅保留 7 天且不保存响应 Body。

- [ ] **Step 4: 验证**

```powershell
pnpm contracts:check
pnpm core:test
pnpm web:typecheck
pnpm web:test
```

Expected: PASS，且错误分类与状态转换全部稳定。Suggested commit when authorized: `feat(provider): add health and capability testing`。

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

- [ ] **Step 5: 验证**

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
- [ ] 请求、费用和健康数据正确且不含正文。
- [ ] 诊断包和日志 Sentinel 扫描通过。
- [ ] 备份恢复和保留任务可恢复、可取消。
- [ ] UI 四态、键盘、焦点、缩放和 Reduced Motion 通过。
- [ ] `pnpm check` 与 Phase 5 E2E 全部通过。