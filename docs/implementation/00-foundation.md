# Phase 0: Engineering Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 建立可在 Windows 干净环境构建和测试的 pnpm + Go + Rust/Tauri 工程基线，并让 Desktop 能启动一个只提供 `/health` 的 Core Sidecar。

**Architecture:** 根目录负责统一命令和文档检查；`apps/core` 是独立 Go 模块；`apps/desktop` 是 Tauri + React 应用。Phase 0 只建立生命周期与契约骨架，不实现 Provider 和模型代理。

**Tech Stack:** Node 24.13.0、pnpm 11.18.0、Go 1.26.5、Rust stable、Tauri 2.11、React 19.2、TypeScript 5.9.3、Vitest 4。

## Global Constraints

- 只创建工程基线，不实现真实上游请求。
- Core 默认绑定 `127.0.0.1`，健康端点不得泄露路径和配置。
- Desktop WebView 不允许任意 Shell、文件或 URL 能力。
- 所有脚本兼容 Windows PowerShell；禁止远程脚本管道安装。
- 未获得明确授权时不 Commit/Push；任务末尾只检查 diff。

---

### Task 0.1: 工具链检查与根 Workspace

**Files:**
- Create: `package.json`
- Create: `pnpm-workspace.yaml`
- Create: `.npmrc`
- Create: `.editorconfig`
- Create: `.gitignore`
- Create: `scripts/validate-docs.mjs`
- Modify: `docs/README.md`

**Interfaces:**
- Produces: 根命令 `docs:check`、`web:*`、`core:*`、`rust:*`、`check`、`build:*`。
- Consumes: 现有 `docs/` 设计文件。

- [ ] **Step 1: 检查本机工具链**

Run:

```powershell
node --version
npm --version
pnpm --version
go version
rustc --version
cargo --version
winget list --id Microsoft.EdgeWebView2Runtime
```

Expected: Node 为 `v24.13.0` 或兼容 Node 24；pnpm 为 `11.18.0`；Go 为 `go1.26.5`；Rust/Cargo 可用；WebView2 已安装。当前已知环境缺少 Go 与 Rust，因此执行阶段先向用户请求安装授权。

- [ ] **Step 2: 在取得安装授权后补齐缺失工具**

Run only when missing and explicitly approved:

```powershell
winget install --id GoLang.Go --exact --accept-package-agreements --accept-source-agreements
winget install --id Rustlang.Rustup --exact --accept-package-agreements --accept-source-agreements
winget install --id Microsoft.VisualStudio.2022.BuildTools --exact --accept-package-agreements --accept-source-agreements --override "--wait --passive --add Microsoft.VisualStudio.Workload.VCTools --includeRecommended"
rustup default stable
```

Expected: 命令退出码 0。重新打开终端后 `go version`、`rustc --version`、`cargo --version` 成功。

- [ ] **Step 3: 创建根 package.json**

```json
{
  "name": "aggregation-hub",
  "private": true,
  "packageManager": "pnpm@11.18.0",
  "scripts": {
    "docs:check": "node scripts/validate-docs.mjs",
    "web:typecheck": "pnpm --dir apps/desktop typecheck",
    "web:lint": "pnpm --dir apps/desktop lint",
    "web:test": "pnpm --dir apps/desktop test --run",
    "core:test": "cd apps/core && go test ./...",
    "core:vet": "cd apps/core && go vet ./...",
    "rust:test": "cargo test --manifest-path apps/desktop/src-tauri/Cargo.toml",
    "check": "pnpm docs:check && pnpm web:typecheck && pnpm web:lint && pnpm web:test && pnpm core:vet && pnpm core:test && pnpm rust:test",
    "build:core": "powershell -NoProfile -ExecutionPolicy Bypass -File scripts/build-core-sidecar.ps1",
    "build:desktop": "pnpm build:core && pnpm --dir apps/desktop tauri build"
  }
}
```

Create `pnpm-workspace.yaml`:

```yaml
packages:
  - apps/desktop
```

Create `.npmrc`:

```ini
engine-strict=true
save-exact=true
strict-peer-dependencies=true
```

- [ ] **Step 4: 创建编码和忽略规则**

Create `.editorconfig`:

```ini
root = true

[*]
charset = utf-8
end_of_line = lf
insert_final_newline = true
indent_style = space
indent_size = 2

[*.go]
indent_style = tab

[*.rs]
indent_size = 4

[*.md]
trim_trailing_whitespace = false
```

Create `.gitignore`:

```gitignore
node_modules/
dist/
target/
coverage/
*.log
*.tmp
.env
.env.*
!.env.example
.superpowers/
apps/desktop/src-tauri/binaries/*.exe
apps/desktop/src-tauri/binaries/*-x86_64-pc-windows-msvc
```

- [ ] **Step 5: 创建根 AGENTS.md**

根文件必须保留仓库级工程规则，并引用：

```markdown
# AGENTS.md

## Project Context

- Product and architecture source of truth: `docs/README.md`
- Stable AI context: `docs/ai/AI_CONTEXT.md`
- Task template: `docs/ai/TASK_TEMPLATE.md`
- Review checklist: `docs/ai/REVIEW_CHECKLIST.md`
- Executable V1 plan: `docs/13-implementation-plan.md`

Before implementation, read the current Phase document under `docs/implementation/`.
Do not read, print, modify, or commit real secrets. Do not commit or push without explicit user approval.
```

在上述项目上下文后保留用户提供的工程、安全、测试、Git 和中文回复规则；不得把 `AGENTS.md` 当作产品设计事实源。

- [ ] **Step 6: 先写文档校验器的失败夹具测试**

Create temporary file `docs/_invalid-plan-fixture.md` with a deliberately broken link and an incomplete marker assembled at runtime:

```powershell
$marker = 'T' + 'BD'
$brokenLink = '[broken]' + '(./does-not-exist.md)'
$content = "# Invalid`n`n$marker`n$brokenLink`n"
[System.IO.File]::WriteAllText('docs/_invalid-plan-fixture.md', $content, [System.Text.UTF8Encoding]::new($false))
```

Create `scripts/validate-docs.mjs`:

```javascript
import { readdir, readFile, stat } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";

const root = resolve("docs");
const errors = [];

async function walk(dir) {
  const out = [];
  for (const name of await readdir(dir)) {
    const path = join(dir, name);
    const info = await stat(path);
    if (info.isDirectory()) out.push(...(await walk(path)));
    else if (name.endsWith(".md")) out.push(path);
  }
  return out;
}

for (const file of await walk(root)) {
  const text = await readFile(file, "utf8");
  const fence = "`".repeat(3);
  if (((text.split(fence).length - 1) % 2) !== 0) errors.push(`${file}: unbalanced fences`);
  const incompleteTerms = ["T" + "ODO", "T" + "BD", "FIX" + "ME", "PLACE" + "HOLDER", "待" + "定", "待" + "补充"];
  if (incompleteTerms.some((term) => new RegExp(`\\b${term}\\b`, "i").test(text) || text.includes(term))) errors.push(`${file}: incomplete marker`);
  for (const match of text.matchAll(/\]\(([^)#]+)(?:#[^)]+)?\)/g)) {
    if (/^(https?:|mailto:)/.test(match[1])) continue;
    try { await stat(resolve(dirname(file), match[1])); }
    catch { errors.push(`${file}: broken link ${match[1]}`); }
  }
  if (/\bsk-[A-Za-z0-9_-]{20,}\b/.test(text)) errors.push(`${file}: possible secret`);
}

if (errors.length) {
  console.error(errors.join("\n"));
  process.exit(1);
}
console.log(`docs ok`);
```

- [ ] **Step 7: 运行校验器确认失败，再删除失败夹具**

Run:

```powershell
node scripts/validate-docs.mjs
```

Expected: FAIL，报告 incomplete marker 和 broken link。

Then remove only the exact fixture path and rerun:

```powershell
[System.IO.File]::Delete((Resolve-Path 'docs/_invalid-plan-fixture.md'))
node scripts/validate-docs.mjs
```

Expected: `docs ok`。

- [ ] **Step 8: 锁定 pnpm 并检查 diff**

Run:

```powershell
corepack enable
corepack prepare pnpm@11.18.0 --activate
pnpm install
pnpm docs:check
git diff --check
```

Expected: 生成 `pnpm-lock.yaml`；docs check 通过；diff check 无空白错误。若未初始化 Git，记录“Git gate 尚未建立”，不要自动 `git init`。

Suggested commit when explicitly authorized:

```powershell
git add package.json pnpm-workspace.yaml pnpm-lock.yaml .npmrc .editorconfig .gitignore AGENTS.md scripts/validate-docs.mjs docs/README.md
git commit -m "chore: establish workspace and documentation checks"
```

---

### Task 0.2: Go Core 健康端点骨架

**Files:**
- Create: `apps/core/go.mod`
- Create: `apps/core/cmd/aggregation-hub-core/main.go`
- Create: `apps/core/internal/config/runtime.go`
- Create: `apps/core/internal/health/handler.go`
- Create: `apps/core/internal/health/handler_test.go`
- Create: `apps/core/internal/dataplane/server.go`
- Create: `apps/core/internal/dataplane/server_test.go`

**Interfaces:**
- Produces: `config.LoopbackHost`, `config.Runtime`, `health.NewHandler(version string) http.Handler`, `dataplane.NewServer(Runtime, http.Handler) *http.Server`。
- Consumes: 仅 Go 标准库。

- [ ] **Step 1: 初始化 Go 模块**

Create `apps/core/go.mod`:

```go
module aggregationhub.local/core

go 1.26
```

Run:

```powershell
cd apps/core
go mod tidy
```

Expected: 命令成功；暂时没有外部依赖。

- [ ] **Step 2: 写健康响应失败测试**

Create `apps/core/internal/health/handler_test.go`:

```go
package health_test

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "aggregationhub.local/core/internal/health"
)

func TestHandlerReturnsMinimalHealth(t *testing.T) {
    req := httptest.NewRequest(http.MethodGet, "/health", nil)
    rec := httptest.NewRecorder()

    health.NewHandler("0.1.0-rc.2").ServeHTTP(rec, req)

    if rec.Code != http.StatusOK { t.Fatalf("status=%d", rec.Code) }
    var body map[string]string
    if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil { t.Fatal(err) }
    if body["status"] != "ok" || body["version"] != "0.1.0-rc.2" || body["data_plane"] != "ready" {
        t.Fatalf("unexpected body: %#v", body)
    }
    if len(body) != 3 { t.Fatalf("health leaked fields: %#v", body) }
}
```

- [ ] **Step 3: 运行测试确认失败**

Run:

```powershell
cd apps/core
go test ./internal/health -run TestHandlerReturnsMinimalHealth -v
```

Expected: FAIL，`internal/health` 或 `NewHandler` 不存在。

- [ ] **Step 4: 实现最小健康 Handler**

Create `apps/core/internal/health/handler.go`:

```go
package health

import (
    "encoding/json"
    "net/http"
)

func NewHandler(version string) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
            http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
            return
        }
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(map[string]string{
            "status": "ok", "version": version, "data_plane": "ready",
        })
    })
}
```

- [ ] **Step 5: 添加 Runtime 与仅回环 Server**

Create `apps/core/internal/config/runtime.go`:

```go
package config

// LoopbackHost 是 Data Plane 唯一允许绑定的回环地址。
const LoopbackHost = "127.0.0.1"

type Runtime struct {
    Version    string
    ListenPort int
}
```

Create `apps/core/internal/dataplane/server.go`:

```go
package dataplane

import (
    "fmt"
    "net/http"
    "time"

    "aggregationhub.local/core/internal/config"
)

func NewServer(cfg config.Runtime, handler http.Handler) *http.Server {
    return &http.Server{
        Addr:              fmt.Sprintf("%s:%d", config.LoopbackHost, cfg.ListenPort),
        Handler:           handler,
        ReadHeaderTimeout: 5 * time.Second,
        IdleTimeout:       90 * time.Second,
    }
}
```

Create `apps/core/internal/dataplane/server_test.go` with two layers of assertions:

- 正向验证 `Addr == config.LoopbackHost + ":18443"`、请求头超时为 5 秒、空闲超时为 90 秒；
- 反向/不变量验证 `config.Runtime` 只有 `Version` 和 `ListenPort` 字段，不存在 `ListenHost` 或其他 Host 覆盖入口，并用不同端口确认 `NewServer` 仍始终绑定 `config.LoopbackHost`。

该字段集合是编译期边界的回归护栏；Host 不作为 Runtime 输入传入 `NewServer`。

- [ ] **Step 6: 实现 main 并运行测试**

Create `apps/core/cmd/aggregation-hub-core/main.go`:

```go
package main

import (
    "log"
    "net/http"

    "aggregationhub.local/core/internal/config"
    "aggregationhub.local/core/internal/dataplane"
    "aggregationhub.local/core/internal/health"
)

func main() {
    cfg := config.Runtime{Version: "0.1.0-rc.2", ListenPort: 18443}
    mux := http.NewServeMux()
    mux.Handle("GET /health", health.NewHandler(cfg.Version))
    server := dataplane.NewServer(cfg, mux)
    log.Printf("aggregation-hub-core listening on %s", server.Addr)
    if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed { log.Fatal(err) }
}
```

Run:

```powershell
cd apps/core
gofmt -w .
go test ./...
go vet ./...
```

Expected: PASS。

- [ ] **Step 7: 手工烟雾测试**

Run in one terminal:

```powershell
cd apps/core
go run ./cmd/aggregation-hub-core
```

Run in another:

```powershell
Invoke-RestMethod http://127.0.0.1:18443/health
```

Expected: 仅含 status/version/data_plane。停止进程后确认端口释放。

Suggested commit when authorized:

```powershell
git add apps/core
git commit -m "feat(core): add loopback health server"
```

---

### Task 0.3: React/Tauri Desktop 骨架

**Files:**
- Create: `apps/desktop/package.json`
- Create: `apps/desktop/index.html`
- Create: `apps/desktop/tsconfig.json`
- Create: `apps/desktop/vite.config.ts`
- Create: `apps/desktop/vitest.config.ts`
- Create: `apps/desktop/eslint.config.js`
- Create: `apps/desktop/src/main.tsx`
- Create: `apps/desktop/src/app/App.tsx`
- Create: `apps/desktop/src/app/App.test.tsx`
- Create: `apps/desktop/src/styles/global.css`
- Create: `apps/desktop/src-tauri/Cargo.toml`
- Create: `apps/desktop/src-tauri/build.rs`
- Create: `apps/desktop/src-tauri/src/main.rs`
- Create: `apps/desktop/src-tauri/tauri.conf.json`
- Create: `apps/desktop/src-tauri/capabilities/default.json`
- Create: `rust-toolchain.toml`

**Interfaces:**
- Produces: 可启动 Desktop、最小 `App`、Tauri capability 基线。
- Consumes: Task 0.1 Workspace。

- [ ] **Step 1: 锁定 Rust stable 精确版本**

Run:

```powershell
$rustVersion=(rustc --version).Split(' ')[1]
@"
[toolchain]
channel = "$rustVersion"
profile = "minimal"
components = ["rustfmt", "clippy"]
"@ | Set-Content -Encoding UTF8 rust-toolchain.toml
```

Expected: 文件包含具体版本号，不是 `stable` 字符串。

- [ ] **Step 2: 创建 desktop package.json**

Use exact versions from the master plan. Required scripts:

```json
{
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "typecheck": "tsc -b --pretty false",
    "lint": "eslint .",
    "test": "vitest",
    "tauri": "tauri"
  }
}
```

Dependencies include React、Router、Query、Zod 和 Tauri API/plugins；devDependencies include TypeScript、Vite、Vitest、Testing Library、ESLint、Prettier、OpenAPI tools。

- [ ] **Step 3: 写 App 失败测试**

Create `apps/desktop/src/app/App.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { App } from "./App";

describe("App", () => {
  it("shows the stopped gateway state", () => {
    render(<App />);
    expect(screen.getByRole("heading", { name: "Aggregation Hub" })).toBeVisible();
    expect(screen.getByText("网关已停止")).toBeVisible();
  });
});
```

- [ ] **Step 4: 运行测试确认失败**

Run:

```powershell
pnpm install
pnpm --dir apps/desktop test --run src/app/App.test.tsx
```

Expected: FAIL，`App` 不存在。

- [ ] **Step 5: 实现最小 React UI**

Create `apps/desktop/src/app/App.tsx`:

```tsx
export function App() {
  return (
    <main className="app-shell">
      <header><h1>Aggregation Hub</h1></header>
      <section aria-labelledby="runtime-heading">
        <h2 id="runtime-heading">运行状态</h2>
        <p>网关已停止</p>
      </section>
    </main>
  );
}
```

Create `main.tsx` to render `<App />`; add semantic base CSS with system fonts and visible focus style。

- [ ] **Step 6: 创建最小 Tauri 配置**

`Cargo.toml` uses `tauri = { version = "2", features = ["tray-icon"] }` plus shell/autostart/dialog/opener plugins and serde。`main.rs` only initializes plugins and generates context。`capabilities/default.json` only permits core window and required plugin operations；不要授予任意 shell execute。

- [ ] **Step 7: 运行前端和 Rust 检查**

Run:

```powershell
pnpm --dir apps/desktop typecheck
pnpm --dir apps/desktop lint
pnpm --dir apps/desktop test --run
cargo fmt --manifest-path apps/desktop/src-tauri/Cargo.toml --check
cargo clippy --manifest-path apps/desktop/src-tauri/Cargo.toml --all-targets -- -D warnings
cargo test --manifest-path apps/desktop/src-tauri/Cargo.toml
```

Expected: 全部 PASS。

Suggested commit when authorized:

```powershell
git add apps/desktop rust-toolchain.toml pnpm-lock.yaml
git commit -m "feat(desktop): add Tauri React application shell"
```

---

### Task 0.4: Core Sidecar 构建与生命周期

**Files:**
- Create: `scripts/build-core-sidecar.ps1`
- Create: `apps/desktop/src-tauri/src/core_process.rs`
- Create: `apps/desktop/src-tauri/src/runtime_commands.rs`
- Create: `apps/desktop/src-tauri/src/core_process_test.rs`
- Modify: `apps/desktop/src-tauri/src/main.rs`
- Modify: `apps/desktop/src-tauri/tauri.conf.json`
- Modify: `apps/core/cmd/aggregation-hub-core/main.go`
- Create: `apps/core/internal/bootstrap/protocol.go`
- Create: `apps/core/internal/bootstrap/protocol_test.go`

**Interfaces:**
- Produces: `ReadyEvent { event, control_url, data_plane_url, pid }`、Tauri Commands `runtime_status/start/stop/restart`。
- Consumes: Core health server、Tauri shell plugin。

- [ ] **Step 1: 写 bootstrap ready 事件失败测试**

```go
func TestReadyEventDoesNotContainManagementToken(t *testing.T) {
    event := bootstrap.ReadyEvent{Event: "ready", ControlURL: "http://127.0.0.1:49152", DataPlaneURL: "http://127.0.0.1:18443", PID: 42}
    raw, err := json.Marshal(event)
    if err != nil { t.Fatal(err) }
    if bytes.Contains(raw, []byte("token")) { t.Fatalf("ready event leaked token: %s", raw) }
}
```

Expected initial `go test ./internal/bootstrap` FAIL。

- [ ] **Step 2: 实现 bootstrap 协议**

```go
type BootstrapSecrets struct {
    ManagementToken string `json:"management_token"`
}

type ReadyEvent struct {
    Event        string `json:"event"`
    ControlURL   string `json:"control_url"`
    DataPlaneURL string `json:"data_plane_url"`
    PID          int    `json:"pid"`
}
```

Core 接受 `--bootstrap-stdin`，从 stdin 读取一行 JSON；未提供或 Token 少于 32 bytes 时退出。ready 事件只写 stdout 一行 JSON，普通日志写 stderr。

- [ ] **Step 3: 创建 sidecar 构建脚本**

`scripts/build-core-sidecar.ps1`：

```powershell
$ErrorActionPreference='Stop'
$root=Split-Path -Parent $PSScriptRoot
$out=Join-Path $root 'apps/desktop/src-tauri/binaries/aggregation-hub-core-x86_64-pc-windows-msvc.exe'
New-Item -ItemType Directory -Force -Path (Split-Path $out) | Out-Null
Push-Location (Join-Path $root 'apps/core')
try { go build -trimpath -ldflags '-s -w' -o $out ./cmd/aggregation-hub-core }
finally { Pop-Location }
Write-Output $out
```

- [ ] **Step 4: 配置 Tauri externalBin**

In `tauri.conf.json`:

```json
{
  "bundle": {
    "externalBin": ["binaries/aggregation-hub-core"]
  }
}
```

- [ ] **Step 5: 写 Rust 生命周期单元测试与实现**

Define:

```rust
#[derive(Clone, Debug, serde::Serialize, PartialEq)]
#[serde(rename_all = "snake_case")]
pub enum RuntimeState { Stopped, Starting, Running, Failed }

#[derive(Clone, Debug, serde::Serialize)]
pub struct RuntimeSnapshot {
    pub state: RuntimeState,
    pub data_plane_url: Option<String>,
    pub last_error: Option<String>,
}
```

Test state transitions `Stopped -> Starting -> Running -> Stopped` and reject duplicate start。`CoreProcessManager` owns child、management token and ReadyEvent; token用 `getrandom`/Rust OS RNG 生成并通过 child stdin 发送，不放参数。

- [ ] **Step 6: 暴露显式 Tauri Commands**

```rust
#[tauri::command]
async fn runtime_status(state: State<'_, RuntimeManager>) -> Result<RuntimeSnapshot, String>;
#[tauri::command]
async fn runtime_start(state: State<'_, RuntimeManager>) -> Result<RuntimeSnapshot, String>;
#[tauri::command]
async fn runtime_stop(state: State<'_, RuntimeManager>) -> Result<RuntimeSnapshot, String>;
#[tauri::command]
async fn runtime_restart(state: State<'_, RuntimeManager>) -> Result<RuntimeSnapshot, String>;
```

Commands 不接受可执行路径、任意参数或任意环境变量。

- [ ] **Step 7: 验证 Sidecar**

Run:

```powershell
pnpm build:core
cargo test --manifest-path apps/desktop/src-tauri/Cargo.toml
pnpm --dir apps/desktop tauri dev
```

Expected: Desktop 启动 Core；UI/开发日志能看到 running 和 Data Plane URL；进程命令行不含管理 Token；退出应用后 Core 结束。

Suggested commit when authorized:

```powershell
git add scripts/build-core-sidecar.ps1 apps/core apps/desktop/src-tauri pnpm-lock.yaml
git commit -m "feat(desktop): manage gateway core sidecar"
```

---

### Task 0.5: 控制契约、CI 与工程门禁

**Files:**
- Create: `contracts/control-plane.openapi.yaml`
- Create: `contracts/fixtures/runtime.json`
- Create: `scripts/check-generated.mjs`
- Create: `scripts/test-all.ps1`
- Create: `.github/workflows/ci.yml`
- Modify: `apps/desktop/package.json`
- Modify: `package.json`
- Modify: `docs/10-requirements-traceability.md`

**Interfaces:**
- Produces: Control Plane v1 runtime schema、统一 CI gate。
- Consumes: Task 0.2/0.4 Runtime DTO。

- [ ] **Step 1: 先写契约失败夹具**

Create `contracts/fixtures/runtime.json` with intentionally invalid state `"booted"`。Create OpenAPI schema allowing only `starting/running/degraded/stopped/failed`。Run Redocly validation plus a small Node fixture validator; Expected FAIL。

- [ ] **Step 2: 修正 Runtime fixture**

```json
{
  "state": "running",
  "data_plane_url": "http://127.0.0.1:18443",
  "started_at": "2026-08-02T10:00:00Z",
  "version": "0.1.0-rc.2",
  "last_error": null
}
```

Expected contract check PASS。

- [ ] **Step 3: 创建 test-all.ps1**

```powershell
$ErrorActionPreference='Stop'
pnpm docs:check
pnpm web:typecheck
pnpm web:lint
pnpm web:test
pnpm core:vet
pnpm core:test
pnpm rust:test
node scripts/check-generated.mjs
```

`check-generated.mjs` validates OpenAPI syntax and fixtures; it must fail on schema drift, not rewrite files。

- [ ] **Step 4: 创建 Windows CI**

`.github/workflows/ci.yml` uses `windows-latest`，安装 Node 24、pnpm 11、Go 1.26、Rust from `rust-toolchain.toml`，缓存 pnpm/Go/Cargo，然后运行：

```powershell
pnpm install --frozen-lockfile
powershell -NoProfile -File scripts/test-all.ps1
pnpm build:core
pnpm --dir apps/desktop build
cargo build --manifest-path apps/desktop/src-tauri/Cargo.toml
```

CI 不注入真实 Provider 凭据。

- [ ] **Step 5: 运行完整 Phase 0 gate**

Run:

```powershell
pnpm install --frozen-lockfile
powershell -NoProfile -File scripts/test-all.ps1
pnpm build:core
pnpm --dir apps/desktop build
cargo build --manifest-path apps/desktop/src-tauri/Cargo.toml
git diff --check
```

Expected: 全部 PASS；生成的 Sidecar 可执行文件不加入 Git；锁文件已更新。

- [ ] **Step 6: 更新追踪矩阵**

将 FR-DESK-001~006、FR-API-005、NFR-SEC-001/002、NFR-MAIN-004 映射到具体计划文件和测试路径，保持设计文档无占位符。

Suggested commit when authorized:

```powershell
git add .github contracts scripts package.json apps/desktop/package.json docs/10-requirements-traceability.md pnpm-lock.yaml
git commit -m "ci: add foundation contracts and quality gates"
```

## Phase 0 Gate

- [ ] 工具链版本已记录，Rust 精确锁定。
- [ ] `pnpm install --frozen-lockfile` 可重复执行。
- [ ] Core `/health` 只在回环地址工作。
- [ ] Desktop 可启动、停止和监控 Sidecar。
- [ ] 管理令牌不在命令行、ready 事件和日志中。
- [ ] 文档、Web、Go、Rust、契约和 CI gate 全部通过。
- [ ] 尚未声称 Provider、Claude Code 或 Codex 可用；当前最高证据为 L2 生命周期烟雾。