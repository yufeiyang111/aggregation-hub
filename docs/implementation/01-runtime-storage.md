# Phase 1: Runtime, Storage, Authentication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 Core 建立可恢复 SQLite、设置、Local Access Key、CredentialStore、Provider/模型仓储、确定性 Router 和安全 HTTP Transport。

**Architecture:** Core 通过 Repository/Service 分层管理数据；秘密保存在 CredentialStore；Data Plane 中间件只接触 Local Key 哈希；Router 只返回不含完整凭据的 RoutePlan。

**Tech Stack:** Go 1.26、modernc.org/sqlite v1.54.0、Windows Credential Manager、SQLite WAL、Go net/http。

## Global Constraints

- 迁移只前向追加，失败不 reset。
- Local Key 只保存 SHA-256 哈希，完整值只返回一次。
- Provider/模型软删除保留历史。
- Router 不读取秘密，Adapter 不直接写数据库。
- Public Provider 与 Local Provider 使用不同网络策略。
- 未获授权时不 Commit/Push。

---

### Task 1.1: SQLite 连接、迁移和设置

**Files:**
- Create: `apps/core/internal/storage/db.go`
- Create: `apps/core/internal/storage/migrate.go`
- Create: `apps/core/internal/storage/migrate_test.go`
- Create: `apps/core/internal/storage/settings_repository.go`
- Create: `apps/core/internal/storage/settings_repository_test.go`
- Create: `apps/core/migrations/0001_initial.sql`
- Create: `apps/core/migrations/migrations.go`
- Modify: `apps/core/go.mod`、`apps/core/go.sum`

**Interfaces:**
- Produces: `storage.Open(path string) (*sql.DB, error)`、`storage.Migrate(ctx, db, fs) error`、`SettingsRepository.Get/Set`。
- Consumes: Phase 0 Core bootstrap。

- [x] **Step 1: 添加 SQLite 依赖并写迁移失败测试**

Run:

```powershell
cd apps/core
go get modernc.org/sqlite@v1.54.0
```

Test creates a temporary DB, runs embedded migrations, asserts `schema_migrations` contains version 1 and `PRAGMA foreign_keys` equals 1。Before implementation expected FAIL。

- [x] **Step 2: 实现安全 Open**

```go
func Open(path string) (*sql.DB, error) {
    db, err := sql.Open("sqlite", path)
    if err != nil { return nil, err }
    db.SetMaxOpenConns(1)
    for _, pragma := range []string{
        "PRAGMA foreign_keys=ON",
        "PRAGMA journal_mode=WAL",
        "PRAGMA synchronous=NORMAL",
        "PRAGMA busy_timeout=5000",
    } {
        if _, err := db.Exec(pragma); err != nil { db.Close(); return nil, err }
    }
    return db, db.Ping()
}
```

V1 单写连接优先正确性；读性能不足时再通过 ADR 调整。

- [x] **Step 3: 编写 0001_initial.sql**

按 `docs/05-database-design.md` 创建 `schema_migrations`、`app_settings`、`local_access_keys`、`providers`、`provider_headers`、`provider_models`、`model_prices`、`oauth_accounts`、`provider_health_checks`、`requests`、`usage_daily`、`audit_events` 与索引。不得省略 CHECK/UNIQUE/FOREIGN KEY。

- [x] **Step 4: 实现迁移校验和与事务**

`Migrate` 读取按版本排序的嵌入 SQL，计算 SHA-256；已执行版本校验和不同则返回 `migration_checksum_mismatch`；新迁移在事务中执行并插入记录。

- [x] **Step 5: 实现 SettingsRepository**

```go
type SettingsRepository interface {
    Get(ctx context.Context, key string) (json.RawMessage, error)
    Set(ctx context.Context, key string, value json.RawMessage, now time.Time) error
}
```

仅 allowlist：gateway.listen_port、gateway.request_timeout_ms、retention.request_days、retention.log_days、desktop.start_on_login、ui.theme、ui.locale。未知 key 返回错误。

- [x] **Step 6: 验证**

Run:

```powershell
cd apps/core
gofmt -w .
go test ./internal/storage -v
go test ./...
pnpm core:test:race
go vet ./...
```

Expected: 迁移、校验和、未知设置、JSON 损坏测试全部 PASS。

**实施记录（2026-08-14）**：已用真实临时 SQLite 文件验证初始 DDL、外键、迁移幂等、校验和漂移拒绝、失败迁移事务回滚、设置白名单与损坏 JSON 拒绝。`pnpm core:test:race` 使用 `-ldflags=-linkmode=external` 保持 Windows Go race 检查可执行；该切片的最高证据等级为 L1。

Suggested commit when authorized: `feat(core): add sqlite migrations and settings repository`。

---

### Task 1.2: Local Access Key 生成与 Data Plane 鉴权

**Files:**
- Create: `apps/core/internal/security/localkey.go`
- Create: `apps/core/internal/security/localkey_test.go`
- Create: `apps/core/internal/storage/localkey_repository.go`
- Create: `apps/core/internal/storage/localkey_repository_test.go`
- Create: `apps/core/internal/dataplane/auth_middleware.go`
- Create: `apps/core/internal/dataplane/auth_middleware_test.go`
- Modify: `apps/core/internal/dataplane/server.go`

**Interfaces:**
- Produces: `LocalKeyService.Create/Verify/Revoke`、`RequireLocalKey(next http.Handler)`。
- Consumes: SQLite、crypto/rand、SHA-256。

- [x] **Step 1: 写生成和单次展示测试**

```go
func TestCreateReturnsFullKeyButStoresOnlyHash(t *testing.T) {
    key, record, err := service.Create(ctx, "default", nil)
    if err != nil { t.Fatal(err) }
    if !strings.HasPrefix(key, "ah_local_") { t.Fatalf("bad prefix") }
    if bytes.Contains(record.TokenHash, []byte(key)) { t.Fatal("plaintext stored") }
}
```

Also test 32 random bytes、不同 Key 哈希不同、Verify 固定结果、revoked false。

- [x] **Step 2: 实现 Key 类型**

```go
type LocalKeyRecord struct {
    ID, Name, Prefix, Suffix, Status string
    TokenHash []byte
    CreatedAt time.Time
    LastUsedAt *time.Time
    ExpiresAt *time.Time
}
```

Generate `ah_local_` + base64url 32-byte random。Hash full string with SHA-256。比较使用 `subtle.ConstantTimeCompare`。

- [x] **Step 3: 实现 Repository 与轮换**

Create inserts active hash；Revoke updates status/revoked_at；Verify queries active candidates by prefix then constant-time compares。Create rotation can accept expiresAt for old key overlap。

- [x] **Step 4: 写 Middleware 负向测试**

Cases: `/health` bypasses middleware at router level；missing header 401；invalid key 401；Authorization/x-api-key conflict 409；valid either header 200；response includes request ID but no key。

- [x] **Step 5: 实现 Middleware**

Extract credential with strict scheme and size limit。Never log supplied value。Store authenticated key ID in request context for request audit。

- [x] **Step 6: 验证**

Run:

```powershell
cd apps/core
go test ./internal/security ./internal/storage ./internal/dataplane -v
go test ./... -race
```

Expected: PASS；SQLite file/string scan不包含生成的完整 Key。

**实施记录（2026-08-14）**：已实现 `ah_local_` + 32 随机字节的 Local Key、SHA-256 哈希、固定时序比较、撤销、过期候选淘汰、SQLite 明文扫描、`Authorization: Bearer` / `X-API-Key` 互斥校验及 request ID。`NewRouter` 让 `/health` 显式绕过鉴权；Core 进程尚未在启动阶段初始化数据库和首次密钥，实际进程接线留待 Task 1.7，因此本切片最高证据为 L1。

Suggested commit: `feat(core): protect data plane with local access keys`。

---

### Task 1.3: CredentialStore 抽象与 Windows 实现

**Files:**
- Create: `apps/core/internal/credential/store.go`
- Create: `apps/core/internal/credential/memory_store.go`
- Create: `apps/core/internal/credential/memory_store_test.go`
- Create: `apps/core/internal/credential/windows_store.go`
- Create: `apps/core/internal/credential/windows_store_test.go`
- Create: `apps/core/internal/credential/reference.go`

**Interfaces:**
- Produces: `CredentialStore.Put/Get/Delete/Probe`、`CredentialRef`。
- Consumes: Windows Credential Manager API；tests use MemoryStore。

- [x] **Step 1: 定义失败契约测试**

Shared test suite verifies Put/Get、overwrite、delete、missing、copy isolation and Probe。Memory store first fails because implementation absent。

- [x] **Step 2: 定义接口**

```go
type Ref string

type SecretValue struct { Bytes []byte }

type Store interface {
    Put(context.Context, Ref, SecretValue) error
    Get(context.Context, Ref) (SecretValue, error)
    Delete(context.Context, Ref) error
    Probe(context.Context) Status
}
```

`SecretValue` 不实现 Stringer/MarshalJSON；日志层拒绝该类型。

- [x] **Step 3: 实现 MemoryStore**

使用 mutex + defensive copy。测试 Get 修改返回值不会改变存储值。

- [x] **Step 4: 实现 WindowsStore**

使用 Windows Credential Manager 的 Generic Credential，TargetName 固定 `AggregationHub/<ref>`；不调用 shell，不写临时文件。非 Windows 文件提供 build-tag unsupported implementation。

- [x] **Step 5: Windows 集成测试**

测试生成随机 ref/value，Put/Get/Delete，defer 清理。测试日志只输出 ref，不输出 value。该测试标记 integration，但可在 Windows CI 运行。

- [x] **Step 6: 验证**

Run:

```powershell
cd apps/core
go test ./internal/credential -v
go test ./... -race
```

Expected: Memory/Windows contract PASS；Credential Manager 中测试条目已清理。

**实施记录（2026-08-14）**：MemoryStore 与 Windows Credential Manager Generic Credential 均通过同一 Put/Get/覆盖/Delete/缺失/副本隔离/Probe 合同测试。Windows 集成测试使用随机 `AggregationHub/integration/...` 引用并通过 `t.Cleanup` 删除；测试和错误信息不输出秘密值。该切片的最高证据等级为 L1。

Suggested commit: `feat(core): add operating-system credential store`。

---

### Task 1.4: Provider 与模型 Repository/Service

**Files:**
- Create: `apps/core/internal/provider/types.go`
- Create: `apps/core/internal/provider/service.go`
- Create: `apps/core/internal/provider/service_test.go`
- Create: `apps/core/internal/storage/provider_repository.go`
- Create: `apps/core/internal/storage/provider_repository_test.go`
- Create: `apps/core/internal/storage/model_repository.go`
- Create: `apps/core/internal/storage/model_repository_test.go`

**Interfaces:**
- Produces: `ProviderService.Create/Update/Enable/Disable/Delete`、`ModelRepository.FindByPublicID`。
- Consumes: CredentialStore、SQLite。

> **实施约束（ADR-0006）**：本任务支持 `none`、`api_key`、`bearer_token`；OAuth 账户与自定义 Header 延后到对应阶段。软删除不复用 slug；删除与旧凭据清理采用提交后的审计补偿。

- [x] **Step 1: 写 Provider slug/secret 补偿失败测试**

Cases: invalid slug；duplicate slug；auth type requires credential；CredentialStore Put succeeds but DB insert fails => new ref deleted；response DTO only masked credential。

- [x] **Step 2: 定义核心类型**

```go
type Provider struct {
    ID, Slug, Name, AdapterType, BaseURL string
    AuthType AuthType
    CredentialRef *credential.Ref
    LifecycleStatus ProviderStatus
    Enabled bool
    Timeout time.Duration
    Version int64
}

type ProviderModel struct {
    ID, ProviderID, UpstreamModelID, PublicModelID, DisplayName string
    Capabilities Capabilities
    Enabled bool
    Version int64
}
```

- [x] **Step 3: 实现 Repository**

所有 SQL 参数化；Create/Update 用事务；Update 需要 expectedVersion；Delete 软删除 Provider/模型；历史 requests 不删除。List 使用 cursor/page size 上限。

- [x] **Step 4: 实现 Service 凭据替换顺序**

Create: Put secret -> transaction -> audit；failure compensates Delete。Replace: Put new -> update DB -> delete old。Mask hint only prefix/suffix；不得返回 secret。

- [x] **Step 5: 模型 ID 和同步规则**

`PublicModelID = provider.Slug + "/" + upstreamModelID`；新同步模型默认 disabled；缺失模型变 `missing_upstream`；用户 capability override 保留。

- [x] **Step 6: 验证**

Run:

```powershell
cd apps/core
go test ./internal/provider ./internal/storage -v
go test ./... -count=1
go test -race -ldflags=-linkmode=external ./...
go vet ./...
``` 

Expected: 事务、软删除、冲突、补偿和模型唯一性 PASS。
**实施记录（2026-08-14）**：已实现 Provider/ProviderModel 强类型、参数化 SQLite Repository、事务审计、乐观版本控制、Provider/模型软删除、游标分页、Public Model ID 派生与模型同步规则。Provider 创建/替换凭据遵循 CredentialStore 写入、SQLite 事务与失败补偿顺序；DTO 不返回完整引用或秘密。OAuth 与自定义 Header 依 ADR-0006 延后。已用临时 SQLite 和 MemoryStore 覆盖非法/重复 slug、认证缺失、创建补偿、替换与旧凭据清理失败、软删除、模型同步与能力覆盖保留。最高证据等级为 L1。


Suggested commit: `feat(core): add provider and model domain services`。

---

### Task 1.5: Model Registry 与确定性 Router

**Files:**
- Create: `apps/core/internal/routing/route.go`
- Create: `apps/core/internal/routing/router.go`
- Create: `apps/core/internal/routing/router_test.go`
- Create: `apps/core/internal/provider/capabilities.go`
- Create: `apps/core/internal/provider/capabilities_test.go`

**Interfaces:**
- Produces: `Router.Resolve(ctx, publicModelID, RequiredCapabilities) (RoutePlan, error)`。
- Consumes: Provider/Model Repository；不消费 CredentialStore。

- [x] **Step 1: 写同名模型隔离测试**

Create two providers each with upstream `model-x`; assert `a/model-x` resolves a and `b/model-x` resolves b。Invalid/no slash、disabled model、auth_required provider all fail。

- [x] **Step 2: 定义 RoutePlan**

```go
type RoutePlan struct {
    ProviderID, ProviderSlug, AdapterType, BaseURL, UpstreamModelID string
    CredentialRef *credential.Ref
    Capabilities provider.Capabilities
    Timeout time.Duration
}
```

Router may carry CredentialRef identifier but must not call `Get` or expose secret。

- [x] **Step 3: 实现能力校验**

`RequiredCapabilities` tracks Stream、Tools、ParallelTools、Reasoning、Thinking、Vision。Missing capability returns typed `unsupported_feature` with feature name。

- [x] **Step 4: 实现 Resolve**

Single repository lookup by unique public ID；validate model/provider status；return immutable value。No random、weight、fallback、retry list。

- [x] **Step 5: 验证**

Run:

```powershell
cd apps/core
go test ./internal/routing ./internal/provider -v
go test -race -ldflags=-linkmode=external ./...
```
Expected: deterministic and capability negative cases PASS。

**实施记录（2026-08-14）**：已实现无状态确定性 Router：严格使用公开模型 ID 查询、校验 Provider/模型可路由状态、保持单 Provider 路径、不读取 CredentialStore。能力覆盖仅接受受限布尔字段并在路由时应用；缺失能力返回带 feature 的类型化错误。测试覆盖同名上游模型隔离、禁用模型、`auth_required` Provider、无效公开 ID、能力拒绝和凭据引用副本隔离。最高证据等级为 L1。

Suggested commit: `feat(core): add deterministic model router`。

---

### Task 1.6: 安全 HTTP Transport 与取消

**Files:**
- Create: `apps/core/internal/transport/policy.go`
- Create: `apps/core/internal/transport/policy_test.go`
- Create: `apps/core/internal/transport/client.go`
- Create: `apps/core/internal/transport/client_test.go`
- Create: `apps/core/internal/security/network.go`
- Create: `apps/core/internal/security/network_test.go`

**Interfaces:**
- Produces: `transport.Factory.ForProvider(RoutePlan) UpstreamClient`、`NetworkPolicy.ValidateURL/ValidateResolvedIP`。
- Consumes: RoutePlan、Go net/http/net/url/netip。

- [x] **Step 1: 写 SSRF/TLS/redirect 负向测试**

Cases: file/gopher rejected；URL credentials rejected；metadata IPv4/IPv6 rejected；Public localhost/private rejected；Local localhost accepted；redirect to new host strips auth and revalidates；TLS verify cannot disable。

- [x] **Step 2: 实现 NetworkPolicy**

Use structured URL parsing and `netip` ranges。Provider kind Public/Local controls private ranges；metadata addresses always denied。DNS dial hook validates every resolved address。

- [x] **Step 3: 实现 Client Factory**

Reuse Transport per Provider；set Dial/Handshake/Header timeout、idle connection settings、max response header bytes；stream requests use context and idle watchdog rather than whole-body timeout。

- [x] **Step 4: 写取消测试**

Fake server blocks until request context done；cancel client context；assert upstream sees cancellation and response body closes within 500 ms target。

- [x] **Step 5: 限制错误体与重定向**

Read at most configured bytes for error summaries；sanitize Content-Type；redirect never forwards Authorization/x-api-key across host。

- [x] **Step 6: 验证**

Run:

```powershell
cd apps/core
go test ./internal/transport ./internal/security -v
go test -race -ldflags=-linkmode=external ./...
```
Expected: SSRF、redirect、cancel、timeout PASS。
**实施记录（2026-08-14）**：已实现 `NetworkPolicy`、基于 Adapter 类型的 Public/Local 网络分类、安全 DNS 拨号、受限 HTTP Transport、跨主机重定向敏感 Header 清除、TLS 默认校验、取消传播、流式空闲响应 Body 关闭以及有界错误摘要读取。`local-openai-compatible` 可访问本地 HTTP；其他 Adapter 必须使用 HTTPS，具体规则见 ADR-0007。Fake HTTP/TLS Server 测试覆盖危险协议、URL 用户信息、元数据地址、私有 DNS、重定向、TLS、取消和错误体边界；最高证据等级为 L1。

Suggested commit: `feat(core): add bounded secure upstream transport`。

---

### Task 1.7: Control Plane 基础与重启恢复

**Files:**
- Create: `apps/core/internal/controlplane/server.go`
- Create: `apps/core/internal/controlplane/auth.go`
- Create: `apps/core/internal/controlplane/runtime_handler.go`
- Create: `apps/core/internal/controlplane/provider_handler.go`
- Create: `apps/core/internal/controlplane/server_test.go`
- Create: `apps/core/internal/observability/recovery.go`
- Create: `apps/core/internal/observability/recovery_test.go`
- Modify: `apps/core/cmd/aggregation-hub-core/main.go`

**Interfaces:**
- Produces: `/internal/v1/runtime`、`/shutdown`、Provider CRUD 初始管理 API。
- Consumes: management token、Provider Service、SQLite。

- [x] **Step 1: 写 Control Plane 鉴权测试**

Missing/wrong token => 401；valid => runtime response；no CORS headers；OPTIONS not broadly allowed；shutdown requires POST and valid token。

- [x] **Step 2: 实现 Auth Middleware**

Management token only accepted in fixed internal header，constant-time compare，never logged。Server binds `127.0.0.1:0` and ready event reports chosen port。

- [x] **Step 3: 实现 Runtime/Shutdown**

Runtime returns state、data URL、version、started_at、last_error。Shutdown triggers graceful cancellation and returns accepted；Desktop timeout后才 kill。

- [x] **Step 4: 实现 Provider CRUD Handler**

Use explicit request DTO and size limit；response masks credentials；PATCH requires version；validation errors map to safe codes。

- [x] **Step 5: 实现请求恢复**

At startup run SQL update from pending/streaming to aborted_by_restart with completed_at；test does not touch succeeded/failed。

- [ ] **Step 6: Phase 1 验证**

Run:

```powershell
pnpm core:test
pnpm core:vet
pnpm rust:test
pnpm check
pnpm build:core
```

Then Desktop smoke: create DB、generate Local Key、restart app、verify settings persist and old in-flight fixture becomes aborted_by_restart。

Suggested commit: `feat(core): expose authenticated control plane and recovery`。
**实施记录（2026-08-14）**：Core 通过受限 stdin 接收桌面端解析后的 `%LOCALAPPDATA%\AggregationHub` 数据目录，创建 `backups`、`logs`、`diagnostics` 目录，打开并迁移 SQLite 后执行遗留请求恢复。Data Plane 现在以 `/health` 为唯一未鉴权路由，其余路径统一使用 Local Access Key 校验。Control Plane 固定使用管理令牌 Header 的常量时间比较、随机回环端口、无 CORS，并支持 Runtime/Shutdown、Provider 的列表、创建、读取、更新、启用、禁用、删除初始 API，以及受管理令牌保护的 `POST /internal/v1/local-keys` 单次 Local Key 创建 API。Provider 输入采用 64 KiB 有界 JSON DTO，拒绝未知字段和超限 Body，响应不返回凭据引用或明文凭据；PATCH、启停和删除均要求版本号。实现和 Fake/临时 SQLite 测试的最高证据等级为 L1；尚未验证真实 Provider、Tauri Desktop UI、Claude Code、Codex 或 OAuth。

**桌面桥接实施记录（2026-08-14）**：新增 Rust 本地 Control Plane Client，仅接受 `http://127.0.0.1:<port>` 和 `/internal/v1/` 白名单路径，设置 3 秒连接/读写超时与 256 KiB 响应上限。管理令牌只在 Rust 侧请求缓冲区中短暂使用，并在写入后清零；`dashboard_status` 仅投影运行时与 Provider 安全摘要，`create_local_key` 仅转发名称并一次性返回新 Key。React 通过 Tauri Command 调用这两个能力，未引入本地存储、管理令牌或完整上游凭据。Rust 单元测试覆盖回环 URL、路径白名单、成功响应解析及运行时快照脱敏；React 单元测试覆盖状态/Provider 摘要展示与通过桥接生成一次性 Key。该新增桥接的最高证据等级为 L1；尚未进行真实 Tauri 窗口、真实 Provider、Claude Code、Codex 或 OAuth 验证。

## Phase 1 Gate

- [ ] Fresh DB migrates and restart preserves data。
- [ ] Local Key full value never persists。
- [ ] Windows CredentialStore contract passes and test entries clean up。
- [ ] Provider/model repositories enforce constraints and soft deletion。
- [ ] Router is deterministic and rejects unsupported capability。
- [ ] Transport blocks dangerous targets and propagates cancel。
- [ ] Control Plane is random-loopback, authenticated, no CORS。
- [ ] `pnpm check` passes；证据等级最高 L2。
