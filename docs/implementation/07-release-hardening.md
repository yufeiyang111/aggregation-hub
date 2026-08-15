# Phase 7: Security Hardening and Windows V1 Release Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 完成安全加固、托盘与开机启动、Windows 安装包、供应链、迁移恢复、性能、干净 VM 和 V1 发布证据。

**Architecture:** Release 只打包受控 Desktop 和 Core Sidecar；安装包、更新和发布产物有来源与校验；兼容矩阵明确 L1~L5，不把构建通过当作真实客户端兼容。

**Tech Stack:** Tauri Bundle、GitHub Actions Windows runner、SBOM/依赖扫描、PowerShell release smoke、Windows 11 x64 clean VM。

## Global Constraints

- V1 Data Plane 仍只监听 `127.0.0.1`。
- 不弱化 TLS、鉴权、迁移和 CI Gate。
- 发布产物不含凭据、`.env`、调试日志、用户数据和真实 Secret Fixture。
- 没有可靠代码签名时不启用自动更新，并明确标注未签名。
- 不执行部署、Commit、Tag、Push 或公开发布，除非用户明确授权。

---

### Task 7.1: 安全基线终审与负向测试

**Requirements:** `NFR-SEC-001~005`、`AC-004`、`FR-OAUTH-001~004`。

**Files:**
- Create: `tests/security/run-all.ps1`
- Create: `tests/security/auth-negative.ps1`
- Create: `tests/security/network-negative.ps1`
- Create: `tests/security/input-limits.ps1`
- Modify: `tests/security/secret-scan.ps1`
- Create: `SECURITY.md`
- Modify: `docs/08-security-design.md`
- Modify: `docs/10-requirements-traceability.md`
- Modify: dependency policy configuration files established in Phase 0。

**Interfaces:** Produces release security report and private disclosure policy。

- [ ] **Step 1: 建立控制到测试矩阵**

将 `docs/08-security-design.md` 每项控制映射到自动测试或人工复核收据；没有证据的控制不能标记完成。

- [ ] **Step 2: 运行负向测试**

覆盖未鉴权/冲突鉴权、CORS、SSRF 与 metadata、DNS 变化、Redirect Auth Stripping、CRLF、超大 JSON/Tool/SSE、截断流、OAuth replay 和诊断路径穿越。

- [ ] **Step 3: 收紧 Tauri 权限**

审查 capabilities，删除未用插件权限；验证 WebView 不能执行任意 Shell、读取任意路径或获取管理令牌。

- [ ] **Step 4: 运行 Sentinel Secret 扫描**

从 Provider、OAuth、错误和 Tool 路径注入 Sentinel，扫描日志、SQLite、诊断包、崩溃输出和打包资源。

- [ ] **Step 5: 建立披露入口并验证依赖**

启用 GitHub Private Vulnerability Reporting，并在根 `SECURITY.md` 写明支持版本和私密报告流程；运行依赖与许可证扫描，不通过关闭 Gate 绕过发现。

- [ ] **Step 6: 验证**

```powershell
pnpm check
powershell -NoProfile -File tests/security/run-all.ps1
```

Expected: 全部 PASS；高危和中危未处理项为零。Suggested commit when authorized: `security: harden local gateway release boundary`。

---

### Task 7.2: 托盘、开机启动与优雅退出

**Requirements:** `FR-DESK-001~006`、`NFR-REL-004`、`NFR-UX-003/004`。

**Files:**
- Create: `apps/desktop/src-tauri/src/tray.rs`
- Create: `apps/desktop/src-tauri/src/lifecycle.rs`
- Create: `apps/desktop/src-tauri/src/lifecycle_test.rs`
- Modify: `apps/desktop/src-tauri/src/lib.rs`
- Modify: `apps/desktop/src/pages/SettingsPage.tsx`
- Modify: `apps/desktop/src/features/runtime/`

**Interfaces:** Produces tray Open/Start/Stop/Copy URL/Exit actions and OS autostart setting。

- [ ] **Step 1: 写生命周期失败测试**

覆盖托盘菜单状态、关闭窗口仅隐藏、显式退出标志阻止 Sidecar 自动重启、端口冲突和 owned PID 边界。

- [ ] **Step 2: 实现托盘与窗口行为**

关闭窗口隐藏；托盘双击显示并聚焦；显式 Exit 先停止接收新请求，再优雅关闭 Core。

- [ ] **Step 3: 实现开机启动**

使用官方 Tauri Autostart 插件，默认关闭；UI 从操作系统实际状态读取，不使用乐观假状态。

- [ ] **Step 4: 验证异常与快速操作**

测试快速 start/stop/restart、窗口崩溃/隐藏、Core 崩溃、端口冲突和超时后只终止受管子进程。

- [ ] **Step 5: 验证**

```powershell
pnpm rust:test
pnpm web:test
pnpm check
```

Expected: PASS。Suggested commit when authorized: `feat(desktop): finalize tray and startup lifecycle`。

---

### Task 7.3: Windows 打包与版本信息

**Requirements:** `NFR-PERF-004`、`NFR-MAIN-003/004`、`FR-DESK-001`。

**Files:**
- Modify: `apps/desktop/src-tauri/tauri.conf.json`
- Create: `apps/desktop/src-tauri/icons/`
- Create: `apps/desktop/src-tauri/resources/`
- Create: `scripts/build-release.ps1`
- Create: `scripts/check-version-consistency.mjs`
- Modify: root version source established in Phase 0。

**Interfaces:** Produces signed-or-clearly-unsigned MSI/NSIS artifact with bundled target-triple Core Sidecar。

- [ ] **Step 1: 建立单一版本源**

把版本传播到 Desktop、Core ready/health 和 Release Metadata；版本不一致时构建失败。

- [ ] **Step 2: 实现发布构建脚本**

顺序执行 frozen install、全量 Gate、trimmed Core build、Tauri Bundle、SHA-256 生成和资源 Secret 扫描；任何一步失败立即退出。

- [ ] **Step 3: 固定安装包元数据**

安装包名称为 `Aggregation Hub`，display publisher 为 `Aggregation Hub Contributors`，目标为 Windows x64；签名构建使用证书实际 Subject；卸载时明确询问是否保留用户数据。

- [ ] **Step 4: 验证运行时自包含**

在无源码树且未安装 Go/Rust/Node 的 Windows 环境运行安装包；若无代码签名证书，标注 unsigned 并禁用自动更新，不声明 trusted publisher。

- [ ] **Step 5: 验证**

```powershell
powershell -NoProfile -File scripts/build-release.ps1
```

Expected: MSI/NSIS、SHA-256 和资源扫描报告生成成功。Suggested commit when authorized: `build: package Windows desktop and core sidecar`。

#### Task 7.3 当前预发布安装包基线（2026-08-15）

- [x] NSIS Bundle 已启用，目标固定为 `nsis`，并打包 `aggregation-hub-core` Sidecar。
- [x] 安装模式为 `currentUser`；WebView2 使用 `downloadBootstrapper`，拒绝降级安装；开始菜单和桌面快捷方式由 NSIS hooks 创建和清理。
- [x] `scripts/build-release.ps1` 顺序执行 `pnpm check`、`pnpm build:desktop`、Setup 工件整理、SHA-256 与已知秘密标记静态扫描，并为避免覆盖已存在工件目录而失败退出。
- [x] `.github/workflows/windows-release-build.yml` 仅在推送 `v*` 版本标签时运行：从标签解析并校验版本、构建 unsigned NSIS 工件、保留 Actions Artifact，并仅使用 Actions 短期 `GITHUB_TOKEN` 创建同仓库 GitHub Pre-release 和上传工件；不使用用户 Token、外部对象存储或签名凭据。
- [x] `scripts/check-windows-installer-config.mjs` 覆盖 Bundle、WebView2、安装模式、Sidecar、快捷方式 hooks、标签触发、最小 Release 权限、预发布标记和工件上传的静态约束。

本记录只证明本地静态配置和构建链；代码签名、SBOM、许可证/Notices、干净 VM 安装/卸载/升级、真实 Provider、Claude Code、Codex 与 OAuth 验证仍未完成。Phase 7 Task 7.3/7.4 的其余步骤不得因为本基线而勾选完成。

**验证记录（2026-08-15）**：`pnpm check` 与 `pnpm --dir apps/desktop tauri build --no-bundle` 已通过，后者证明 Tauri 能读取安装器配置并完成 Desktop/Core release 编译。`pnpm release:windows` 已运行到 Tauri NSIS 打包阶段，但在下载外部 NSIS/WebView2 打包依赖时超过 12 分钟没有产生 `release\bundle\nsis` 或 Setup 文件；该受网络下载阻塞的进程树已被精确终止，未生成或覆盖发布工件。因此本切片仍不能声明 NSIS Setup、干净 VM 或真实安装验证成功。

---
### Task 7.4: CI Release、SBOM 与开源治理

**Requirements:** `NFR-MAIN-004`、`AC-004`；open-source requirements from `docs/12-open-source-and-release.md`。

**Files:**
- Create: `.github/workflows/release.yml`
- Create: `LICENSE`
- Create: `CONTRIBUTING.md`
- Create: `CODE_OF_CONDUCT.md`
- Create: `CHANGELOG.md`
- Create: root `README.md`
- Modify: dependency policy configuration files。

**Interfaces:** Produces reproducible release workflow, SBOM, notices and open-source governance。

- [ ] **Step 1: 确认许可证**

创建 `LICENSE` 前由用户确认 Apache License 2.0；若选择其他许可证，先重新检查依赖兼容性和分发要求。

- [ ] **Step 2: 实现受控 Release Workflow**

只对签名版本 Tag 触发；运行完整 CI，构建 Windows Artifact，生成 SHA-256、SBOM 和 Third-party Notices；真实 Provider 测试保留为受保护手工 Job。

- [ ] **Step 3: 完善开源文档**

根 README 写明 local-only 边界、证据矩阵、安装、配置、安全和禁止账号共享；CONTRIBUTING 引用 `docs/ai/AI_CONTEXT.md`、需求 ID、契约测试和 no-real-secret 规则。

- [ ] **Step 4: Dry Run**

在不发布的分支运行 Workflow 等价命令，检查 Artifact、SBOM、Notices 和日志，不上传 Release。

- [ ] **Step 5: 验证**

```powershell
pnpm check
powershell -NoProfile -File scripts/build-release.ps1
```

Expected: PASS，且未创建公开 Release。Suggested commit when authorized: `chore: add open-source governance and release pipeline`。

---

### Task 7.5: 迁移、恢复、性能与耐久

**Requirements:** `NFR-REL-001~004`、`NFR-PERF-001~004`、`AC-005`。

**Files:**
- Create: `tests/release/migrations/`
- Create: `tests/release/recovery.ps1`
- Create: `tests/release/performance.ps1`
- Create: `tests/release/soak.ps1`
- Create: Go benchmark files in affected Core packages。
- Modify: `docs/09-testing-strategy.md`

**Interfaces:** Produces migration, recovery, performance and soak receipts。

- [ ] **Step 1: 验证所有已发布 Schema**

为每个已发布版本保留去敏 Fixture；升级到当前版本并验证数据、Foreign Key、索引和迁移校验和。

- [ ] **Step 2: 注入恢复故障**

覆盖迁移 SQL 失败、磁盘满和权限失败；Core 必须停止而不 reset，旧数据库和备份保持可恢复。

- [ ] **Step 3: 执行并发与耐久测试**

运行 100 个并发流、慢客户端、长 SSE、队列饱和、重复 Core 重启和 24 小时 Soak；记录终态一致性、丢弃计数和资源曲线。

- [ ] **Step 4: 测量性能预算**

排除上游网络后测量代理新增首 Token 延迟；使用 Release Build 记录 CPU、Desktop/Core 内存和机器规格。

- [ ] **Step 5: 验证数据生命周期**

在大数据集上验证请求保留批处理、日汇总和备份恢复；结果对照 NFR，未达标时修复或在发布前取得用户批准。

- [ ] **Step 6: 验证**

```powershell
powershell -NoProfile -File tests/release/recovery.ps1
powershell -NoProfile -File tests/release/performance.ps1
powershell -NoProfile -File tests/release/soak.ps1
```

Expected: PASS 或形成带实测数据的发布阻塞报告。Suggested commit when authorized: `test: add release migration and performance gates`。

---

### Task 7.6: 干净 Windows VM 与 V1 候选验收

**Requirements:** `AC-001~006` and all V1 release gates。

**Files:**
- Create: `tests/release/windows-clean-vm-checklist.md`
- Create: `docs/release/compatibility-matrix.md`
- Create: `docs/release/release-notes-template.md`
- Modify: `CHANGELOG.md`

**Interfaces:** Produces final V1 release receipt。

- [ ] **Step 1: 建立干净 VM**

使用 Windows 11 x64 VM，只有系统 WebView2，不安装 Go/Rust/Node；安装候选 Artifact。

- [ ] **Step 2: 执行完整用户流程**

覆盖首次启动、Local Key、Provider、模型、Claude Code L4、Codex L4、OAuth L5 或批准的限制、托盘、重启、备份、诊断和卸载数据提示。

- [ ] **Step 3: 验证监听与进程**

使用 `Get-NetTCPConnection` 确认应用只监听回环地址；核对 owning PID、父子进程和命令行无 Secret。

- [ ] **Step 4: 扫描安装与用户数据**

扫描安装目录、用户数据、日志、SQLite、诊断包和安装器，确认无 Sentinel/Real Secret；验证从前一 Release Candidate 升级保留数据库，失败升级恢复旧可执行文件和备份。

- [ ] **Step 5: 完成发布说明**

记录精确兼容客户端、Provider、模型、证据等级、已知限制、签名状态和迁移说明；私密保留 L4/L5 收据，公开内容只使用脱敏摘要。

- [ ] **Step 6: 最终命令**

```powershell
pnpm install --frozen-lockfile
powershell -NoProfile -File scripts/test-all.ps1
powershell -NoProfile -File scripts/build-release.ps1
```

Expected: 全部 PASS；否则停止发布。Suggested commit when authorized: `release: prepare Aggregation Hub v1.0.0`。

## Phase 7 / V1 Gate

- [ ] Security control-to-test matrix complete，高危和中危未处理项为零。
- [ ] Windows Installer 在干净 VM 正常工作。
- [ ] Data Plane 只监听回环地址且除 `/health` 外全部鉴权。
- [ ] Claude Code L4 和 Codex L4 完成。
- [ ] 至少一个官方 OAuth Adapter 达到 L5，或用户批准正式限制报告。
- [ ] Migration、Recovery、Performance 和 Soak 目标通过，或存在经批准的例外。
- [ ] SBOM、Checksums、Third-party Notices、License 和 Release Notes 完整。
- [ ] Source、Log、SQLite、Diagnostics、Artifact 和 CI 无真实 Secret。
- [ ] 没有用户明确授权时不 Commit、Tag、Push 或公开发布。
