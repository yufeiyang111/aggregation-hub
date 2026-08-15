# Phase 6: OAuth Provider Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 建立安全 OAuth 基础设施，先完成 Claude/Codex 可行性验证，再实现至少一个符合官方机制且达到 L5 的 OAuth Provider Adapter。

**Architecture:** 系统浏览器完成授权；Core 临时监听回环回调；PKCE Verifier 只保存在内存；Token 直接进入 CredentialStore；Provider Adapter 通过 TokenProvider 获取短期 Access Token。

**Tech Stack:** Go OAuth/PKCE、Windows CredentialStore、Tauri Opener、React UI、PowerShell L5 Smoke。

## Global Constraints

- 先做官方文档和服务条款可行性 Spike；结论不通过时不得强行实现。
- 禁止 Cookie、Web Session、账号密码和浏览器自动化抓取。
- Access/Refresh Token 不进入 SQLite、WebView、日志、URL、命令行或崩溃报告。
- OAuth Adapter 未达到 L5 时标记 experimental 且默认关闭。
- 实施日必须重新核对官方文档；2026-08-02 的设计基线不能代替未来执行日证据。
- 未获用户明确授权时不 Commit、不 Push。

---

### Task 6.1: Claude/Codex OAuth 可行性 Spike

**Requirements:** `FR-OAUTH-001/002/005`、`AC-006`。

**Files:**
- Create: `docs/implementation/oauth-feasibility-report.md`
- Modify: `docs/references.md`
- Modify: `docs/adr/0004-credential-store.md` only if an accepted mechanism changes credential boundaries
- Modify: `docs/01-product-requirements.md` only if both providers are formally unsupported

**Interfaces:** Produces a go/no-go decision per provider；consumes current official docs and user-authorized local observations。

- [ ] **Step 1: 刷新官方证据**

重新获取 Claude Code 与 Codex 当前认证/配置文档，记录 URL、执行日期、支持账户类型、授权入口、Token Helper 或 Gateway Hook。

- [ ] **Step 2: 只读检查官方客户端机制**

只检查用户授权的本机官方配置路径，不打印或复制 Token；确认是否存在受支持的第三方网关凭据机制。

- [ ] **Step 3: 完成逐 Provider 判定**

回答授权端点/Client Registration、PKCE、Refresh、允许的 Gateway/API Surface、Required Header/Protocol、账号共享限制和 Revoke 机制。

- [ ] **Step 4: 输出三态结论**

每个 Provider 分类为 `supported`、`experimental_with_warning` 或 `not_supported`，并给出引用和限制；缺少文档不能解释为允许。

- [ ] **Step 5: 执行文档安全扫描**

```powershell
pnpm docs:check
$hits = Select-String -LiteralPath docs/implementation/oauth-feasibility-report.md -Pattern 'Bearer\s+[A-Za-z0-9._-]{16,}|eyJ[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{8,}'
if ($hits) { throw 'possible OAuth secret in feasibility report' }
```

Expected: 可行性报告有清晰结论，且不含 Token、Cookie、邮箱或个人账号标识；若两者均不支持，停止 Adapter 实现并形成正式限制报告。Suggested commit when authorized: `docs: record OAuth provider feasibility evidence`。

---

### Task 6.2: OAuth Session、PKCE 与一次性回调

**Requirements:** `FR-OAUTH-001~004`、`NFR-SEC-001/003/005`。

**Files:**
- Create: `apps/core/internal/oauth/session.go`
- Create: `apps/core/internal/oauth/pkce.go`
- Create: `apps/core/internal/oauth/callback.go`
- Create: `apps/core/internal/oauth/session_test.go`
- Create: `apps/core/internal/controlplane/oauth_handler.go`
- Modify: `contracts/control-plane.openapi.yaml`
- Modify: `apps/desktop/src-tauri/src/runtime_commands.rs`

**Interfaces:** Produces `OAuthService.StartSession/CompleteSession/CancelSession` and session status endpoint。

- [ ] **Step 1: 写安全失败测试**

覆盖随机 State、S256 PKCE、10 分钟过期、一次性消费、错误 State、Replay、Cancel、超大 Query、错误 Path/Method 和 Listener Closure。

- [ ] **Step 2: 实现内存 Session**

Verifier/State 来自 `crypto/rand`，仅以内存 Session ID 索引；不持久化 Verifier；完成、取消和过期都清理。

- [ ] **Step 3: 实现一次性回环 Callback**

绑定 `127.0.0.1:0`，校验精确 Path/Method/Query Size，返回最小静态 HTML 后关闭 Listener。

- [ ] **Step 4: 实现安全控制接口和浏览器打开**

Control API 返回 Authorization URL 与 Expiry，不返回 Verifier；日志移除 Query；Tauri 只在 HTTPS 与 Provider Allowlist 通过后打开 URL。

- [ ] **Step 5: 验证**

```powershell
cd apps/core
go test ./internal/oauth ./internal/controlplane -v -race
cd ../..
pnpm rust:test
pnpm contracts:check
```

Expected: PASS，Replay/错误 State/过期 Session 全部拒绝且 Listener 被关闭。Suggested commit when authorized: `feat(oauth): add PKCE authorization sessions`。

---

### Task 6.3: TokenProvider、刷新与撤销

**Requirements:** `FR-OAUTH-003/004`、`NFR-SEC-003/005`、`NFR-REL-004`。

**Files:**
- Create: `apps/core/internal/oauth/token_provider.go`
- Create: `apps/core/internal/oauth/refresh_group.go`
- Create: `apps/core/internal/oauth/token_provider_test.go`
- Create: `apps/core/internal/storage/oauth_repository.go`
- Create: `apps/core/internal/storage/oauth_repository_test.go`

**Interfaces:** Produces `AccessToken(ctx)`、`Refresh(ctx)` and `Revoke(ctx)`；consumes CredentialStore and safe Transport。

- [ ] **Step 1: 写共享行为测试**

有效 Token 不刷新；临近过期刷新；20 个并发调用只刷新一次；刷新失败进入 `auth_required`；Revoke 删除 Credential 并写 Audit。

- [ ] **Step 2: 实现秘密存储边界**

Token Bundle 作为 Secret Bytes 写入 CredentialStore；SQLite 只保存 Account Label、Scope、Expiry、Status 和 Credential Reference。

- [ ] **Step 3: 实现协调刷新与取消**

每账户 Singleflight；网络调用期间不持有全局锁；请求取消向 Token Exchange/Refresh 传播。

- [ ] **Step 4: 验证传输与脱敏**

```powershell
cd apps/core
go test ./internal/oauth ./internal/credential ./internal/storage ./internal/transport -v -race
```

Expected: PASS；Sentinel Token 不出现在错误、日志和 SQLite。Suggested commit when authorized: `feat(oauth): manage token refresh and revocation`。

---

### Task 6.4: 首个受支持 OAuth Adapter

**Requirements:** `FR-OAUTH-005`、`FR-API-007~010`、`NFR-MAIN-001/002`。

**Files:**
- Create: `apps/core/internal/adapter/oauthofficial/provider.go`
- Create: `apps/core/internal/adapter/oauthofficial/request.go`
- Create: `apps/core/internal/adapter/oauthofficial/response.go`
- Create: `apps/core/internal/adapter/oauthofficial/stream.go`
- Create: `apps/core/internal/adapter/oauthofficial/adapter_test.go`
- Create: `apps/core/testdata/oauthofficial/`
- Modify: `apps/core/internal/adapter/registry.go`
- Create: `tests/security/secret-scan.ps1`

**Interfaces:** Produces one Adapter backed by TokenProvider；consumes the feasibility report’s selected official mechanism。

- [ ] **Step 1: 执行条件 Gate**

只有至少一个 Provider 被分类为 `supported` 或 `experimental_with_warning` 才继续；不得复制不允许分发的 Official Client Secret。

- [ ] **Step 2: 写 Fake 契约测试**

覆盖 Token Exchange、Refresh、Non-stream、Stream、Tool、Cancel、401 Invalid Token 和 Revoke。

- [ ] **Step 3: 实现被选机制**

Endpoint、Header、Wire Protocol 和 Model Discovery 必须来自可行性报告；包内 `provider.go` 固定 Provider ID 与证据版本，不加入 Cookie/Session 分支。

- [ ] **Step 4: 限制 401 重试**

响应字节发送前遇到 Invalid Token 最多协调刷新一次；流式开始后不得 Replay。

- [ ] **Step 5: 验证并保持默认关闭**

```powershell
cd apps/core
go test ./internal/adapter/oauthofficial ./internal/oauth ./internal/adapter -v -race
cd ../..
powershell -NoProfile -File tests/security/secret-scan.ps1
```

Expected: Shared Adapter Contract 和 Secret Scan PASS；元数据仍为 experimental/default-off，直到 Task 6.6 达到 L5。Suggested commit when authorized: `feat(adapter): add experimental official OAuth provider`。

---

### Task 6.5: OAuth Desktop UI

**Requirements:** `FR-OAUTH-003/004`、`NFR-UX-001~004`。

**Files:**
- Create: `apps/desktop/src/pages/OAuthAccountsPage.tsx`
- Create: `apps/desktop/src/components/OAuthConnectDialog.tsx`
- Create: `apps/desktop/src/features/oauth/schemas.ts`
- Create: `apps/desktop/src/features/oauth/service.ts`
- Create: `apps/desktop/src/features/oauth/oauth.test.tsx`
- Modify: `apps/desktop/src/app/router.tsx`
- Modify: diagnostics summary files。

**Interfaces:** Produces connect/wait/refresh/revoke UI；consumes Control OAuth API。

- [ ] **Step 1: 写状态与交互失败测试**

覆盖 disconnected、authorizing、connected、refreshing、auth_required、revoked、timeout、Cancel 和 Duplicate Submit。

- [ ] **Step 2: 实现系统浏览器流程**

Connect 只打开系统浏览器；Dialog 显示倒计时和取消；不嵌入 Provider 登录页，不显示 Callback Query。

- [ ] **Step 3: 实现安全账户卡片**

只显示非敏感 Label、Scope、Expiry 和 Last Refresh；Revoke/Re-authorize 二次确认；Experimental Adapter 持续显示警告和文档入口。

- [ ] **Step 4: 处理 Desktop 生命周期**

窗口隐藏时保留授权等待状态；应用重启后把未完成 Session 标记过期，不尝试恢复 Verifier。

- [ ] **Step 5: 验证**

```powershell
pnpm web:typecheck
pnpm web:lint
pnpm web:test
pnpm rust:test
```

Expected: PASS；UI 快照与浏览器命令中无 Token、State、Code 或 Callback Query。Suggested commit when authorized: `feat(desktop): manage OAuth provider accounts`。

---

### Task 6.6: OAuth L5 真实验证与安全复核

**Requirements:** `AC-006`、`FR-OAUTH-001~005`、`NFR-SEC-003/005`。

**Files:**
- Create: `tests/live/oauth-official-smoke.ps1`
- Modify: `docs/implementation/oauth-feasibility-report.md`
- Modify: `docs/10-requirements-traceability.md`
- Modify: `docs/08-security-design.md`
- Modify: `docs/references.md`

**Interfaces:** Produces L5 evidence or formal failure/unsupported report。

- [ ] **Step 1: 使用专用测试账户完成授权**

账户必须由用户明确授权；系统浏览器中人工完成密码和 2FA，脚本不得自动化登录。

- [ ] **Step 2: 执行真实端到端场景**

完成 Login、Token Refresh、真实 Claude Code 或 Codex 流式文本、Tool Calling、Cancel 和 Revoke。

- [ ] **Step 3: 验证重启与跨机器行为**

应用重启后 Credential Reference 恢复账户但不暴露 Token；仅恢复 SQLite 到另一机器时必须进入 `auth_required`。

- [ ] **Step 4: 执行全面秘密扫描**

扫描 Process Command Line、Log、SQLite、Diagnostics 和 UI Snapshot；执行日再次复核当前条款与技术文档。

- [ ] **Step 5: 晋级或保持关闭**

只有 L5 和安全复核全部通过才把 Adapter 从 Experimental 晋级；否则保持默认关闭并发布限制说明。

- [ ] **Step 6: 验证**

```powershell
pnpm check
powershell -NoProfile -File tests/live/oauth-official-smoke.ps1
```

Expected: L5 PASS，或形成精确、可复核且不含秘密的失败/不支持报告；Live Result 不进入默认 CI。Suggested commit when authorized: `test: validate official OAuth provider end to end`。

## Phase 6 Gate

- [ ] Feasibility Report 有当前证据且不含秘密。
- [ ] PKCE、State、Replay、Timeout、Refresh 和 Revoke 测试通过。
- [ ] 至少一个 Adapter 达到 L5，或两家 Provider 都有用户批准的正式不支持报告。
- [ ] Experimental Adapter 在晋级前保持默认关闭。
- [ ] Code 和 Docs 中不存在 Cookie、Web Session 或账号密码路径。
- [ ] `pnpm check` 与适用的 L5 Script 完成并分级记录。