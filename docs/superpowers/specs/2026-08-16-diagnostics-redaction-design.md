# Aggregation Hub 脱敏日志与诊断导出设计

**日期：** 2026-08-16  
**状态：** 已获方案确认，待用户审阅文档后实现  
**关联需求：** `FR-OBS-004`、`FR-OBS-006`、`NFR-SEC-003`、`NFR-SEC-005`、`AC-004`  
**实施任务：** Phase 5 / Task 5.3

## 1. 背景与目标

Aggregation Hub 是本地优先的 LLM 聚合网关。用户需要在出现网关、Provider 或运行时问题时导出足够的诊断证据，但 API Key、OAuth Token、Local Access Key、请求正文、上游响应、Header、URL Query 和 Tool 参数均不得进入日志、桌面端状态、SQLite 或诊断包。

本切片提供：

1. 结构化、字段受限的 `SafeLogger`；
2. 最近 100 条已脱敏安全错误摘要的内存环形缓冲；
3. 手动触发的、固定 allowlist 的 ZIP 诊断导出；
4. 受控诊断目录打开能力；
5. 对 Sentinel、路径与压缩包内容的负向测试。

诊断包仅在用户主动点击导出后生成，不自动上传、发送或同步到外部服务。

## 2. 明确不做

- 不读取、复制或修改 `.env`、系统凭据、CredentialStore、cc-switch 配置、浏览器 Cookie 或真实 Provider 配置。
- 不导出 SQLite 数据库、数据库备份、完整日志、完整配置文件、请求正文、上游响应、Header 值、URL Query、Tool 参数或任意用户路径。
- 不实现遥测、崩溃上报、自动上传、云端诊断、任意 Shell 执行或任意文件打开。
- 不验证真实 Provider、Claude Code、Codex 或 OAuth；本切片最高证据等级为 L1。
- 不改动 Data Plane 监听地址、Local Access Key 鉴权或 Provider 路由行为。

## 3. 方案与数据流

采用“结构化 `SafeLogger` + 内存环形缓冲 + ZIP allowlist”方案。

```mermaid
flowchart LR
  A["Core 受控错误事件"] --> B["字段验证与类型约束"]
  B --> C["字段级脱敏"]
  C --> D["Sink Sentinel 二次扫描"]
  D --> E["最近 100 条安全摘要（内存）"]
  E --> F["用户点击导出"]
  F --> G["固定 allowlist 内容"]
  G --> H["ZIP 临时文件"]
  H --> I["ZIP 内容二次扫描"]
  I --> J["受控 diagnostics 目录"]
```

### 3.1 结构化安全日志

`SafeLogger` 只接收预定义安全字段：事件名、稳定错误码、HTTP 状态、脱敏 Provider Slug、公开模型 ID、协议、耗时、时间戳和短错误类别。它不提供“任意 map”或“任意文本”记录入口，避免调用方无意把秘密混入结构化字段。

允许导出的错误摘要不包含 Go error 的原始文本；内部错误首先映射为稳定安全错误类别。若需要保留有诊断价值的 URL，只能经过安全 URL 格式化后输出协议、主机和路径。

### 3.2 脱敏规则与纵深防御

- URL：移除用户名、密码、Query 和 Fragment；不记录原始 URL 字符串。
- Header：默认不记录值；诊断需要时仅记录 allowlist 内 Header 名称。
- 文本：替换 Bearer、`x-api-key`、OAuth `code`、常见 API Key 格式及其他已知 Sentinel。
- Prompt、回复正文、Tool 参数和秘密包装类型：在 API 类型层不允许送入 `SafeLogger`。
- Sink 层：导出 ZIP 前再次按 Sentinel 与秘密正则扫描；命中则拒绝导出、删除临时文件并仅记录安全错误类别。

字段级保护是主防线；正则扫描只是纵深防御，不能被表述为完整的秘密发现能力。

### 3.3 内存环形缓冲

Core 在内存中保留最近 100 条安全错误摘要，按时间顺序先进先出。它不写入完整日志文件，重启后缓冲清空。这样避免了历史文件包含旧版本敏感数据或需要额外处理轮转、权限、保留期和损坏恢复。

### 3.4 诊断 ZIP allowlist

`POST /internal/v1/diagnostics/export` 只能生成到应用数据目录下固定的 `diagnostics` 子目录。ZIP 条目固定为：

1. `runtime.json`：应用/Core 版本、回环监听状态、启动状态与时间；
2. `migration.json`：已应用迁移版本与校验结果摘要；
3. `credential-store.json`：CredentialStore 可用性 Probe 结果，不含凭据名称、引用或内容；
4. `provider-health.json`：Provider 健康摘要，仅含脱敏 slug、状态、最近检查时间和安全错误码；
5. `recent-errors.json`：最多 100 条 `SafeLogger` 安全错误摘要；
6. `manifest.json`：格式版本、生成时间、固定条目清单和摘要校验值。

任何数据库文件、备份、日志文件、配置文件、绝对路径、`..` 路径或不在 allowlist 的条目均不进入 ZIP。所有 ZIP 写入在临时文件完成，二次扫描通过后再原子移动到受控目录。

### 3.5 控制面与桌面端

控制面接口继续受管理令牌与回环限制保护：

- `GET /internal/v1/diagnostics` 返回可安全显示的运行时摘要、最近安全错误数量和导出能力；
- `POST /internal/v1/diagnostics/export` 返回受控生成的文件名、大小、时间和格式版本，不返回绝对路径；
- Desktop 通过现有受控 Tauri Bridge 调用上述接口；
- Rust 端仅能打开应用已知的 diagnostics 目录，前端无权传入任意路径或 Shell 参数。

诊断页面复用现有浅色黑白工具风格：展示运行摘要、错误数量、导出按钮、导出成功/失败反馈和“打开诊断文件夹”按钮。不显示原始错误文本、完整路径或凭据。

## 4. 失败模式与处理

| 场景 | 处理 |
|---|---|
| 不安全字段或 Sentinel 到达日志入口 | 拒绝记录原文，仅写安全错误类别。 |
| ZIP 内容命中 Sentinel | 删除临时文件，导出失败，不生成部分归档。 |
| ZIP 条目包含绝对路径或 `..` | 构建阶段拒绝，测试覆盖。 |
| diagnostics 目录不可写 | 返回安全错误，不输出路径或系统错误详情。 |
| 管理令牌缺失/错误 | 按现有 Control Plane 鉴权拒绝。 |
| Core 未运行 | Desktop 显示可操作的安全错误状态，不伪造导出成功。 |
| 诊断缓冲为空 | 仍可导出固定摘要文件，`recent-errors.json` 为空数组。 |

## 5. 实施边界与文件

预计新增或修改：

- `apps/core/internal/security/redact.go` 与测试：安全 URL、Header 名称、文本 Sentinel 与受限值类型；
- `apps/core/internal/observability/logger.go` 与测试：结构化 `SafeLogger`、环形缓冲和字段拒绝；
- `apps/core/internal/controlplane/diagnostics_handler.go` 与测试：鉴权后摘要、导出、allowlist 和 ZIP 扫描；
- `contracts/control-plane.openapi.yaml`：若既有诊断接口缺少响应 schema，则补齐安全 DTO；
- `apps/desktop/src/pages/DiagnosticsPage.tsx` 与测试：加载、空、错误、成功和键盘状态；
- `apps/desktop/src-tauri/src/runtime_commands.rs`：受控 diagnostics 目录打开命令；
- `tests/e2e/diagnostics-secret-scan.ps1`：对生成 ZIP 进行 Sentinel、路径和条目 allowlist 检查；
- Phase 5 文档、需求追踪与 API/安全设计：实现时同步实际契约。

不新增第三方依赖，优先使用 Go 标准库 `archive/zip`、`encoding/json`、`os` 与既有项目抽象。

## 6. 测试与验收

### 自动化验收

1. Bearer、`x-api-key`、OAuth code、Prompt、Tool 参数、带 Query URL 的 Sentinel 都不会出现在安全日志或 ZIP 中。
2. 无法将秘密包装类型或原始 Header/Body 送入安全日志 API。
3. ZIP 仅包含固定 allowlist 条目，拒绝绝对路径、`..` 和未知条目。
4. ZIP 生成后的二次扫描命中 Sentinel 时失败并不留下归档。
5. 最多导出最近 100 条错误摘要，顺序与淘汰规则正确。
6. Control Plane 未携带或携带错误管理令牌时拒绝诊断读取和导出。
7. Desktop 只可触发导出与打开受控目录，不能指定任意文件路径。

### 验证命令

```powershell
cd apps/core
go test ./internal/security ./internal/observability ./internal/controlplane -v
go test -race -ldflags=-linkmode=external ./internal/security ./internal/observability ./internal/controlplane
cd ../..
pnpm web:typecheck
pnpm web:lint
pnpm web:test
powershell -NoProfile -File tests/e2e/diagnostics-secret-scan.ps1
pnpm check
```

## 7. 风险、取舍与后续

- 内存环形缓冲在 Core 重启后清空：这是避免持久化诊断日志风险的明确取舍；后续如需要跨重启摘要，应另行设计带保留期和更严格扫描的安全日志文件。
- Regex/Sentinel 扫描无法证明发现所有秘密；类型限制、字段 allowlist 和“不记录正文/Header”才是主控制。
- ZIP 仅能证明应用导出链路没有已知 Sentinel；不能证明真实 Provider、OAuth 或外部客户端场景安全。
- 正式发布前仍需进行 Phase 7 的工件级扫描、干净系统安装验证及真实使用流程的人工安全复核。
