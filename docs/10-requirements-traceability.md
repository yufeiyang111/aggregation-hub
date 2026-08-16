# Aggregation Hub 需求追踪矩阵

> 文档编号：TRACE-001  
> 状态：Phase 0 自动化门禁已在本机通过；Phase 1 Task 1.1 的 SQLite、迁移和设置仓储已实现。真实 Desktop Sidecar、真实 Provider、真实客户端和 OAuth 证据仍须在对应阶段补齐。

## 1. 规则

每项实现任务必须引用需求 ID；每项需求至少关联模块、接口/数据和验证。新需求先进入 PRD，再进入本矩阵。Fake 测试不能替代真实验证。

## 2. 功能需求

| 需求 | 模块 | 接口/数据 | 验证 |
|---|---|---|---|
| FR-DESK-001~006 | Desktop Sidecar 生命周期与 Runtime DTO | `RuntimeStatus`、受限 stdin bootstrap、回环 Data Plane | Rust 生命周期测试、契约检查、后续 Desktop E2E |
| FR-PROV-001~003 | Provider Service/Adapter Registry | providers、headers | 创建正负例、Schema |
| FR-PROV-004~005 | Provider Service | PATCH/DELETE、软删除 | 影响确认、历史保留 |
| FR-PROV-006 | Provider Test | health_checks | DNS/TLS/auth/rate limit 分类 |
| FR-PROV-007 | Model Discovery | provider_models | 同步、缺失标记、不自动删除 |
| FR-PROV-008 | Header Policy | provider_headers | 保护头、CRLF 负向测试 |
| FR-OAUTH-001~004 | OAuth/TokenProvider | oauth_accounts | PKCE/state/刷新/撤销 |
| FR-OAUTH-005 | OAuth Adapter | 真实账户 | L5 或限制报告 |
| FR-MODEL-001~003 | Model Registry/Router | provider_models 唯一约束 | 命名空间和冲突 |
| FR-MODEL-004~006 | Capability Validator | 能力字段 | 支持/拒绝矩阵 |
| FR-MODEL-007 | Models Ingress | GET /v1/models | 可见性与鉴权 |
| FR-API-001 | Anthropic Ingress | /v1/messages | 契约 + Claude Code L4 |
| FR-API-002 | Responses Ingress | /v1/responses | 契约 + Codex L4 |
| FR-API-003 | Chat Ingress | /v1/chat/completions | Chat 契约 |
| FR-API-004~006 | Models/Health/Auth | /v1/models、/health | 鉴权和最小响应 |
| FR-API-007~010 | Stream/Cancellation | NormalizedEvent、requests | SSE、Tool、取消、不重放 |
| FR-KEY-001~005 | Local Key Service | local_access_keys | 生成、哈希、轮换、撤销 |
| FR-OBS-001~004 | Observability | requests、日志 | 元数据、筛选、秘密扫描 |
| FR-OBS-005 | Token 汇总 | requests、usage_daily | 输入/输出/缓存/Reasoning Token、已报告计数、缓存命中率未知语义 |
| FR-OBS-006 | Diagnostics | export | allowlist 和敏感扫描 |
| FR-CONN-001~004 | Client Setup | 模板生成器 | 快照、官方字段、真实连接 |

## 3. 已实施切片的具体映射

本节记录已落地的文件和验证边界。契约、单元测试和临时 SQLite 仅构成 L1 证据；它们不替代真实 Desktop 生命周期、Provider、客户端或 OAuth 验证。

### Phase 0 自动化基线

| 需求 ID | 文件 | 覆盖边界 | 验证命令与证据 |
|---|---|---|---|
| FR-DESK-001~006 | `apps/desktop/src-tauri/src/core_process.rs`、`contracts/control-plane.openapi.yaml`、`contracts/fixtures/runtime.json` | Runtime DTO、受限 stdin bootstrap、回环 ready URL、管理令牌不进入快照 | `pnpm rust:test`；`node scripts/check-generated.mjs --self-test`；真实 Desktop E2E 待补 |
| FR-API-005 | `apps/core/internal/health`、`scripts/test-all.ps1`、`.github/workflows/ci.yml` | `/health` 的最小响应与 Phase 0 串行门禁 | `pnpm core:test`；本机 `scripts/test-all.ps1`；回环进程 smoke 待补 |
| NFR-SEC-001 | `apps/core/internal/config`、`apps/core/internal/dataplane`、`contracts/control-plane.openapi.yaml` | Data Plane 与 Control Plane URL 仅接受 `127.0.0.1` | Core socket/ready URL 单元测试；契约 self-test |
| NFR-SEC-002 | `apps/desktop/src-tauri/src/core_process.rs`、`contracts/control-plane.openapi.yaml` | WebView 快照不含管理令牌或 Control URL；React 不直接访问 Control Plane | `pnpm rust:test`；Control Plane 实现与 E2E 待 Phase 1 Task 1.7 |
| NFR-MAIN-004 | `scripts/check-generated.mjs`、`scripts/test-all.ps1`、`.github/workflows/ci.yml` | 文档、契约和语言门禁串行执行，检查脚本不重写输入 | `pnpm docs:check`；Redocly lint；本机 Phase 0 Gate |

### Phase 1 Task 1.1：SQLite、迁移和设置

| 需求 ID | 文件 | 覆盖边界 | 验证命令与证据 |
|---|---|---|---|
| NFR-REL-002~003 | `apps/core/internal/storage/db.go`、`migrate.go`、`apps/core/migrations/0001_initial.sql` | 单连接 WAL、外键、前向迁移、校验和漂移拒绝、失败不 reset | `go test ./internal/storage -v`；`pnpm core:test:race`（L1） |
| NFR-MAIN-003 | `apps/core/migrations/migrations.go`、`apps/core/internal/storage/migrate_test.go` | 迁移嵌入二进制、仅向前追加、事务记录迁移版本与 SHA-256 | 临时 SQLite 的幂等、回滚与所有 V1 表测试（L1） |
| NFR-MAIN-004 | `apps/core/internal/storage/settings_repository.go`、本矩阵 | 设置 key 白名单、有效 JSON 写入、损坏 JSON 显式报错 | `go test ./internal/storage -v`；`go vet ./...`（L1） |
### Phase 1 Task 1.2：Local Access Key 与 Data Plane 鉴权组件

| 需求 ID | 文件 | 覆盖边界 | 验证命令与证据 |
|---|---|---|---|
| FR-KEY-001~005 | `apps/core/internal/security/localkey.go`、`apps/core/internal/storage/localkey_repository.go` | 32 字节随机 Key、哈希持久化、仅展示一次、过期重叠字段、立即吊销 | `go test ./internal/security ./internal/storage -v`；SQLite 文件哨兵扫描（L1） |
| FR-API-006 | `apps/core/internal/dataplane/auth_middleware.go`、`server.go` | `/health` 路由级绕过；其他已注册 Data Plane 路由可统一要求 Bearer 或 `X-API-Key` | 中间件缺失、冲突、无效、成功和上下文 key ID 测试（L1） |
| NFR-SEC-001/003 | Local Key 哈希、固定时序比较、无秘密错误体和 request ID | 不记录提供的 Key；数据库不保存完整 Key | `pnpm core:test:race`（L1）；实际 Core 接线待 Task 1.7 |
### Phase 1 Task 1.3：CredentialStore

| 需求 ID | 文件 | 覆盖边界 | 验证命令与证据 |
|---|---|---|---|
| FR-PROV-003 | `apps/core/internal/credential/*.go` | 上游秘密存 Windows Credential Manager；SQLite 只在后续 Provider 表中存引用 | MemoryStore 与 Windows Generic Credential 合同测试（L1） |
| NFR-SEC-003/005 | `SecretValue`、`Ref`、WindowsStore | SecretValue 禁止 JSON 序列化；无 Shell/临时秘密文件；随机测试项清理 | `go test ./internal/credential -v`；`pnpm core:test:race`（L1） |

### Phase 1 Task 1.4：Provider、模型与凭据编排

| 需求 ID | 文件 | 覆盖边界 | 验证命令与证据 |
|---|---|---|---|
| FR-PROV-001~004 | `apps/core/internal/provider/*.go`、`apps/core/internal/storage/provider_repository.go` | Provider 输入、slug/认证约束、CredentialStore 写入与补偿、DTO 脱敏、版本控制、启用/禁用、软删除和无秘密审计 | `go test ./internal/provider ./internal/storage -v`；临时 SQLite + MemoryStore（L1） |
| FR-MODEL-001~003 | `apps/core/internal/storage/model_repository.go` | 公开模型 ID 派生、Provider 内唯一、同步新模型默认禁用、缺失模型标记、能力覆盖 JSON 保留 | 模型同步、恢复、重复输入与 FindByPublicID 测试（L1） |
| NFR-REL-002~003 | Provider/Model Repository、ADR-0006 | 显式 SQLite 事务、乐观锁、Provider/模型软删除、删除/替换后的凭据清理失败审计 | `go test ./... -count=1`；`go test -race -ldflags=-linkmode=external ./...`（L1） |

### Phase 1 Task 1.5：能力校验与确定性路由

| 需求 ID | 文件 | 覆盖边界 | 验证命令与证据 |
|---|---|---|---|
| FR-MODEL-001~006 | `apps/core/internal/routing/*.go`、`apps/core/internal/provider/capabilities*.go` | 命名空间模型唯一解析、单 Provider 路由、可路由状态、能力覆盖与显式能力拒绝 | `go test ./internal/routing ./internal/provider -v`；临时 SQLite 路由测试（L1） |
| NFR-MAIN-001~002 | Router 窄查询接口、`RoutePlan` | Router 不读取 CredentialStore，不返回秘密，不包含随机/权重/fallback/retry 列表 | 路由计划凭据引用副本隔离与负向测试（L1） |

### Phase 1 Task 1.6：安全网络与上游 Transport

| 需求 ID | 文件 | 覆盖边界 | 验证命令与证据 |
|---|---|---|---|
| NFR-SEC-004 | `apps/core/internal/security/network.go`、ADR-0007 | URL 协议/用户信息/fragment、Public HTTPS、DNS/IP 分类、云元数据、回环/私网与 Local 例外 | 危险 URL、IPv4/IPv6 metadata、Public/Local 地址负向测试（L1） |
| NFR-REL-001 | `apps/core/internal/transport/client.go` | 已校验 IP 拨号、分阶段超时、请求 Context 取消传播、流式空闲 Body 关闭 | Fake 上游取消 500ms 目标测试（L1） |
| NFR-SEC-003/004 | Transport Redirect/Error Summary | 跨主机移除敏感 Header、限制错误体、受限 Content-Type、不能关闭 TLS 验证 | Fake redirect、TLS 失败、错误摘要边界测试（L1） |
### Phase 1 Task 1.7：Control Plane、启动恢复与 Provider 初始管理

| 需求 ID | 文件 | 覆盖边界 | 验证命令与证据 |
|---|---|---|---|
| NFR-SEC-001~003 | `apps/core/internal/bootstrap/protocol.go`、`apps/core/cmd/aggregation-hub-core/main.go`、`apps/desktop/src-tauri/src/core_process.rs` | 受限 stdin 只接受管理令牌和绝对数据目录；桌面端使用 `%LOCALAPPDATA%\AggregationHub`；Core 仅监听回环地址 | Bootstrap 与运行时临时 SQLite 测试；`cargo test`（L1） |
| NFR-SEC-001~003 | `apps/core/internal/controlplane/*.go` | 固定管理令牌 Header、常量时间比较、无 CORS、随机回环 Control Plane、受控 Shutdown | Control Plane 鉴权、OPTIONS、Shutdown 和 Provider 路由负向测试（L1） |
| FR-PROV-001~004、NFR-REL-002~003 | `apps/core/internal/controlplane/provider_handler.go`、`apps/core/internal/provider/*.go` | Provider CRUD 使用显式 DTO、64 KiB Body 上限、版本冲突、无凭据回显和安全错误映射 | Fake Service/Reader 的 CRUD、超限 Body、秘密回显与错误映射测试（L1） |
| NFR-REL-004 | `apps/core/internal/observability/recovery.go` | 启动时仅将 `pending` / `streaming` 请求转为 `aborted_by_restart` | 临时 SQLite 请求状态恢复测试（L1） |
| FR-KEY-001、NFR-SEC-002/003 | `apps/core/internal/controlplane/local_key_handler.go`、`apps/core/cmd/aggregation-hub-core/main_test.go`、`apps/desktop/src-tauri/src/control_client.rs`、`apps/desktop/src/app/App.tsx` | 单次 Local Key 创建经 Rust Tauri bridge 交付给当前 WebView 内存；管理令牌、Control URL、CredentialStore 引用和上游凭据不进入 WebView；Data Plane 认证闭环 | Core 启动级 SQLite/HTTP 集成、Rust bridge 单元、React UI 单元测试（L1） |
| FR-OBS-004/006、NFR-SEC-003/005、AC-004 | `apps/core/internal/observability/{logger,diagnostics_recorder}.go`、`apps/core/internal/observability/diagnostics/`、`apps/core/internal/controlplane/diagnostics_handler.go`、`apps/desktop/src/pages/DiagnosticsPage.tsx`、`tests/e2e/diagnostics-secret-scan.ps1` | 失败请求的受限安全摘要、固定诊断 ZIP allowlist、管理令牌鉴权、固定目录打开和无绝对路径响应 | Go 单元/Control Plane 测试、React 诊断页测试、Rust 固定目录测试、ZIP 安全与 Sentinel 拒绝脚本、`pnpm check` 和 Race（L1） |
| FR-PROV-006/007、FR-CONN-004、FR-MODEL-004~007、NFR-SEC-002/003 | `apps/core/internal/{management,provider,storage,controlplane}`、`apps/desktop/src-tauri/src/{core_process,runtime_commands}.rs`、`apps/desktop/src/app/App.tsx` | 显式 Provider 模型测试、固定错误码/状态转换、最近七天脱敏健康记录、管理令牌鉴权和按需 Desktop 弹窗；不保存正文、Header 或凭据 | Go 单元/真实临时 SQLite/Control Plane 负向测试、Rust bridge 构建测试、React 加载/空/成功测试（L1）；未运行真实 Provider、Claude Code、Codex 或 OAuth |
| NFR-REL-002/003、NFR-MAIN-003、AC-005、NFR-UX-003/004 | `apps/core/internal/{maintenance,storage}`, `apps/core/internal/controlplane/maintenance_handler.go`, `apps/core/cmd/aggregation-hub-core/main.go`, `apps/desktop/src/{features/settings,pages/SettingsPage.tsx}`, `tests/e2e/backup-restore.ps1` | 设置版本乐观锁、固定回环 Host、端口/超时重启提示、终态请求批量保留、审计、固定目录 SQLite 一致性备份、pending 恢复和 pre-restore 回退证据；WebView 不接收路径或管理令牌 | 真实临时 SQLite、Core 启动、Control Plane、Rust bridge、React 二次确认与脱敏错误测试、备份恢复 L1 脚本（L1）；未执行干净 VM、磁盘满/ACL 注入、真实客户端或 Provider |
## 4. 非功能需求

| 需求 | 控制 | 验证 |
|---|---|---|
| NFR-SEC-001 | Data Plane 和 Control Plane 仅使用回环地址；契约仅接受 `127.0.0.1` | Go 回环地址单元测试、Ready URL 校验和 OpenAPI self-test |
| NFR-SEC-002 | WebView -> Tauri Command -> Core；管理令牌与 Control URL 不序列化到 RuntimeSnapshot、DashboardSnapshot 或 WebView API；Local Key 仅一次性内存交付 | Rust 生命周期与本地 Control Client 单元测试；React UI 单元测试；Control Plane 鉴权与路由测试（L1） |
| NFR-SEC-003/005 | 受限 stdin bootstrap、CredentialStore 与秘密脱敏 | bootstrap 测试已覆盖；CredentialStore 待 Task 1.3 |
| NFR-SEC-004 | URL Policy/TLS | SSRF、TLS、Redirect 测试待 Task 1.6 |
| NFR-REL-001 | Context 取消 | 取消时延集成测试待 Task 1.6 |
| NFR-REL-002~003 | SQLite WAL、外键、单连接、事务迁移、失败不 reset | `go test ./internal/storage -v`；临时 SQLite L1 |
| NFR-REL-004 | 请求状态机 | 启动恢复 SQL 状态边界测试（L1）；完整请求状态机待后续入口阶段 |
| NFR-PERF-001~002 | 流式直通和背压 | 延迟、慢客户端、内存测试待后续入口阶段 |
| NFR-PERF-003 | 有界日志队列 | 拥塞和丢弃计数待 Phase 5 |
| NFR-PERF-004 | Tauri + Go 预算 | 发布构建实测待 Phase 7 |
| NFR-MAIN-001~002 | Adapter/分层 | 包依赖与共享 Adapter 测试待 Phase 2~4 |
| NFR-MAIN-003 | 嵌入式前向迁移与校验和 | `migrate_test.go` 的幂等、漂移和事务回滚测试 |
| NFR-MAIN-004 | 本矩阵与统一门禁 | `pnpm docs:check`、`scripts/test-all.ps1`、CI Gate |
| NFR-UX-001~004 | 页面状态/可访问性/确认/错误 | 组件与人工检查待 Phase 5 |
## 5. 验收追踪

| 场景 | 依赖 | 最低证据 |
|---|---|---|
| AC-001 Claude Code | FR-API-001、007~010、MODEL、PROV | L4 + Tool Calling |
| AC-002 Codex | FR-API-002、007~010、MODEL、PROV | L4 + 取消 |
| AC-003 同名隔离 | FR-MODEL-001~006 | 集成 + 双 Provider 烟雾 |
| AC-004 安全日志 | FR-OBS、NFR-SEC | 日志/DB/诊断扫描 |
| AC-005 重启恢复 | FR-DESK、NFR-REL | Desktop E2E + SQLite 恢复 |
| AC-006 OAuth | FR-OAUTH | L5 或正式限制报告 |

## 6. 实施后的映射

脚手架建立后，每项任务维护：需求 ID -> 设计章节 -> 代码文件 -> 测试 -> 验证命令 -> 真实证据。PR 描述列出受影响需求 ID；无需求 ID 的维护改动需关联 Issue 或 ADR。

2026-08-14 已记录：Phase 1 Task 1.1 覆盖 `NFR-REL-002~003`、`NFR-MAIN-003~004`，证据为真实临时 SQLite 的 L1 测试；不声称 Provider、Claude Code、Codex 或 OAuth 可用。

2026-08-16 已记录：Phase 4 Task 4.5 新增 `tests/live/codex-smoke.ps1` 和使用说明，覆盖隔离 `CODEX_HOME`、仅回环 `/v1`、显式 `-RunLive` Tool/Function smoke 与 Local Access Key 输出扫描；本次仅完成脚本语法、预检和项目门禁（L1），尚未执行真实 Provider/Codex，取消端到端断言待可观测性阶段。

2026-08-16 已记录：Phase 5 Task 5.1 落地 `RequestRecord`、事务仓储、单终态生命周期、流式终态包装器和三个协议入口接入；验证为 Core 定向测试、`go vet ./...`（L1），不保存正文/Header/Tool 参数；Windows `-race` 受本机 Go race runtime 链接错误阻塞，队列、费用和 UI 查询仍待后续任务。


| FR-OBS-001~005 / NFR-UX-001/002/004 | Task 5.4 请求与用量查询 | `observability/query.go`、`storage` 查询仓储、Control Plane 请求/用量接口、Tauri 桥接、`features/observability`、概览/请求/用量页面 | 后端游标与聚合单测、Control Plane 负向测试、桌面页面四态与抽屉测试 | 不包含费用统计或真实 Provider/客户端联调 |
