# Windows 一键安装包与开源发布设计

> 文档编号：REL-002  
> 状态：已确认；实现中（NSIS 外部依赖下载阻塞）  
> 日期：2026-08-15  
> 关联文档：[开源与发布设计](../../12-open-source-and-release.md)、[实施计划](../../13-implementation-plan.md)、[Phase 7 发布加固](../../implementation/07-release-hardening.md)

## 1. 目标与范围

为 Windows x64 用户提供一个可下载、双击安装、可卸载的 Aggregation Hub 安装程序。第一版面向个人开发者和开源用户：不要求管理员权限，不要求预装开发工具链，不要求用户手工复制 Core Sidecar。

本设计只覆盖安装包和发布构建链，不扩展 Provider、模型、OAuth、请求转发或桌面控制面功能。

## 2. 已确认的产品决策

- 安装器格式：NSIS `Setup.exe`。
- 首个目标平台：Windows x64。
- 安装模式：当前用户（per-user），默认不触发 UAC 管理员权限请求。
- WebView2：在线 Bootstrapper。只有目标机器缺少 WebView2 Runtime 时才联网获取官方运行时。
- 安装内容：Tauri Desktop 应用和 Go Core Sidecar 一起进入安装包；安装过程中不下载 Aggregation Hub 自身的业务二进制文件。
- 用户数据：继续存放在 `%LOCALAPPDATA%\AggregationHub`；卸载默认不删除数据库、日志、诊断信息或 CredentialStore 中的凭据，避免误删用户数据。
- 发布渠道：先生成可上传 GitHub Release 的构建产物和校验文件；本任务不创建 Release、不上传文件、不推送 Git Tag。
- 自动更新：不在本任务实现。没有代码签名和可信更新清单之前，不提供自动更新。

## 3. 用户体验

```text
GitHub Release
  └─ Aggregation-Hub_<version>_x64-setup.exe
       └─ 用户双击
           ├─ 缺少 WebView2：下载并安装官方 Runtime
           ├─ 安装 Aggregation Hub Desktop + Core Sidecar
           ├─ 创建开始菜单和桌面快捷方式
           └─ 用户从快捷方式启动应用

Windows 设置 > 已安装的应用
  └─ Aggregation Hub > 卸载
       └─ 删除程序文件；保留用户数据和系统凭据
```

安装完成后不会自动导入 API Key、访问用户浏览器 Cookie、开启公网监听、修改 Claude Code/Codex 配置，或发送遥测数据。

## 4. 技术设计

### 4.1 Tauri Bundle

将 `apps/desktop/src-tauri/tauri.conf.json` 的 `bundle.active` 设为 `true`，保留 `binaries/aggregation-hub-core` 作为 `externalBin`。Windows Bundle 目标仅启用 `nsis`，并明确配置：

- `installMode`：`currentUser`；
- `webviewInstallMode`：在线 Bootstrapper；
- 快捷方式：开始菜单和桌面快捷方式；
- 升级标识：沿用稳定的 `identifier` `local.aggregationhub.desktop`；
- 构建产物文件名包含产品名、版本号、架构和 `setup` 后缀。

不加入 MSI、便携版、静默管理员安装、驱动程序或服务安装逻辑。以后若有企业部署需求，再单独设计 MSI 和静默安装支持。

### 4.2 构建入口和工件

保留 `pnpm build:desktop` 作为本地完整构建入口：先构建 Go Core Sidecar，再执行 Tauri production build。新增一个只处理发布工件的脚本，负责：

1. 清晰定位 NSIS 安装器输出；
2. 复制到约定的发布工件目录；
3. 计算 SHA-256；
4. 生成不含秘密的工件清单；
5. 失败时返回非零退出码，禁止产出“成功”标记。

发布工件目录和所有构建缓存继续遵循仓库的 D 盘工具链配置；不得写入用户的 API Key 或应用数据目录。

### 4.3 GitHub Actions

新增独立的 Windows Release Build workflow，只允许 `workflow_dispatch` 手动触发。触发者必须输入版本号，构建脚本会验证其与 `tauri.conf.json` 的单一版本源完全一致，不会由 CI 改写版本。它应当：

- 使用锁定的 Node、pnpm、Go、Rust 与仓库 lockfile；
- 先执行 `pnpm check`；
- 构建 NSIS 安装包；
- 执行 SHA-256 与工件内容检查；
- 以构建产物形式上传安装包、`.sha256` 文件和工件清单。

该工作流不调用 GitHub Release 发布、上传至外部对象存储、签名或使用发布凭据。公开发布必须在后续获得用户明确授权后单独执行。

### 4.4 签名和 Windows 提示

第一版可以构建未签名安装包，但发布说明必须明确“未签名安装包可能出现 Windows 未知发布者或 SmartScreen 提示”。代码签名证书、私钥、时间戳服务和 CI Secret 不属于本任务。获得合法证书后，需要单独设计安全的签名、密钥托管与可复现发布流程。

## 5. 安全与可靠性边界

| 风险 | 设计控制 |
|---|---|
| 安装器夹带秘密 | 构建后执行已知秘密标记静态扫描；禁止 `.env`、数据库、日志、测试凭据和用户数据作为 Bundle 输入。该扫描不能替代 Phase 7 Sentinel 全链路扫描。 |
| 安装器下载未知业务二进制 | 应用和 Core Sidecar 均在本地构建后打入安装包；仅 WebView2 Runtime 允许按 Tauri 配置联网获取。 |
| 卸载误删用户数据 | 卸载只移除安装目录和快捷方式；默认保留 `%LOCALAPPDATA%\AggregationHub` 与 CredentialStore 条目。 |
| 升级破坏数据 | 保持 Tauri `identifier` 稳定；不在安装器中重置 SQLite，不执行破坏性迁移。 |
| 未签名提示被误解 | 发布说明、校验文件和 SHA-256 使用说明必须随工件提供；后续正式版引入代码签名。 |
| 发布工作流越权 | Pre-release workflow 只由 `v*` 标签触发；权限收敛为 `contents: write`，仅使用 GitHub Actions 短期 Token，且只向同仓库上传已检查的工件。 |

## 6. 验收标准

### 自动化

- `pnpm check` 通过。
- `pnpm build:desktop` 成功，并生成 NSIS `Setup.exe`。
- 发布工件脚本成功生成安装器、SHA-256 文件与清单。
- 工件静态扫描未发现已知 Local Key、常见 API Key、GitHub Token 或 SQLite 数据库头标记；其覆盖范围被明确记录。
- GitHub Actions workflow YAML 可被仓库现有文档和静态检查读取。

### 人工 Windows 验证（后续执行）

- 干净 Windows x64 虚拟机中，双击安装器可完成安装。
- 未预装 WebView2 时，在线运行时安装流程能正常完成。
- 开始菜单和桌面快捷方式可启动应用。
- Desktop 与 Core Sidecar 能启动，Data Plane 仍只监听 `127.0.0.1`。
- 卸载后程序已移除，用户数据仍保留。
- 升级安装不会删除已有 SQLite 数据或 CredentialStore 凭据。

真实干净 VM、真实网络、真实 Provider、Claude Code、Codex 和 OAuth 验证不属于本任务的自动化证据。

**实施验证记录（2026-08-15）**：已完成 NSIS 配置、发布工件脚本、配置静态校验和手动 Actions workflow。`pnpm check` 与 `tauri build --no-bundle` 通过；完整 NSIS 打包在下载外部依赖阶段无进展，12 分钟内未生成 Setup 文件，因此当前不把安装包描述为已产出。

## 7. 实施切片

1. 更新 Tauri NSIS Bundle 配置，并补充最小配置校验。
2. 增加发布工件整理、哈希和秘密扫描脚本及测试夹具。
3. 增加仅手动触发的 Windows Release Build workflow。
4. 更新开源发布文档、发布说明模板和实施计划追踪。
5. 运行本地完整构建和静态校验；不发布、不签名、不创建 GitHub Release。

## 8. 非目标与后续工作

- 不实现自动更新、MSI、macOS/Linux 安装器或企业集中部署。
- 不在安装器中自动写入 Claude Code/Codex 的 API 配置。
- 不实现代码签名；该项必须在持有合法证书和安全 CI Secret 方案后单独评审。
- 不把“构建成功”描述为干净 VM 安装、真实 Provider、Claude Code、Codex 或 OAuth 已验证。
