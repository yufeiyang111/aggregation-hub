import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";

const root = resolve(import.meta.dirname, "..");
const configPath = resolve(root, "apps/desktop/src-tauri/tauri.conf.json");
const workflowPath = resolve(root, ".github/workflows/windows-release-build.yml");

function readJson(filePath) {
  return JSON.parse(readFileSync(filePath, "utf8"));
}

function requireValue(condition, message) {
  assert.ok(condition, message);
}

const config = readJson(configPath);
const bundle = config.bundle;
requireValue(bundle?.active === true, "Tauri bundle.active 必须启用。");
requireValue(Array.isArray(bundle.targets) && bundle.targets.length === 1 && bundle.targets[0] === "nsis", "Windows 发布只能构建 NSIS 安装包。");
requireValue(Array.isArray(bundle.externalBin) && bundle.externalBin.includes("binaries/aggregation-hub-core"), "NSIS 安装包必须包含 Core Sidecar。");
requireValue(bundle.windows?.webviewInstallMode?.type === "downloadBootstrapper", "WebView2 必须使用在线 Bootstrapper。");
requireValue(bundle.windows?.webviewInstallMode?.silent === true, "WebView2 Bootstrapper 必须静默运行。");
requireValue(bundle.windows?.allowDowngrades === false, "Windows 安装器必须拒绝降级安装。");
requireValue(bundle.windows?.nsis?.installMode === "currentUser", "NSIS 必须使用当前用户安装模式。");
requireValue(bundle.windows?.nsis?.startMenuFolder === "Aggregation Hub", "NSIS 开始菜单目录必须固定为 Aggregation Hub。");

const hooksPath = resolve(resolve(configPath, ".."), bundle.windows.nsis.installerHooks);
requireValue(existsSync(hooksPath), "NSIS installerHooks 文件不存在。");
const hooks = readFileSync(hooksPath, "utf8");
requireValue(hooks.includes("NSIS_HOOK_POSTINSTALL") && hooks.includes("CreateShortcut"), "NSIS 安装后必须创建桌面快捷方式。");
requireValue(hooks.includes("NSIS_HOOK_POSTUNINSTALL") && hooks.includes("Delete"), "NSIS 卸载后必须清理桌面快捷方式。");

requireValue(existsSync(workflowPath), "Windows Pre-release workflow 不存在。");
const workflow = readFileSync(workflowPath, "utf8");
requireValue(/^on:\s*\r?\n\s*push:\s*\r?\n\s*tags:\s*\r?\n\s*-\s*["']v\*["']/m.test(workflow), "Pre-release workflow 必须仅由 v* 版本标签触发。");
requireValue(/^permissions:\s*\r?\n\s*contents:\s*write/m.test(workflow), "Pre-release workflow 必须只授予 contents: write，以创建同仓库 Release。");
requireValue(workflow.includes("actions/checkout@v4") && workflow.includes("persist-credentials: false"), "检出步骤必须禁用持久化 GitHub 凭据。");
requireValue(workflow.includes("pnpm install --frozen-lockfile"), "Pre-release workflow 必须使用 frozen lockfile 安装依赖。");
requireValue(workflow.includes("$config = Get-Content -LiteralPath 'apps/desktop/src-tauri/tauri.conf.json' -Raw | ConvertFrom-Json") && workflow.includes("if ([string]$config.version -cne $version)"), "Pre-release workflow 必须在构建前校验标签版本与 Tauri 版本一致。");
requireValue(workflow.includes("run: pnpm release:windows"), "Pre-release workflow 必须执行受控发布构建脚本。");
requireValue(!workflow.includes("release:windows -- -Version"), "Pre-release workflow 不得向 PowerShell 发布脚本透传孤立的 -- 参数。");
requireValue(workflow.includes("actions/upload-artifact@v4"), "Pre-release workflow 必须保留构建 Artifact 以便诊断。");
requireValue(workflow.includes("actions/github-script@v7"), "Pre-release workflow 必须使用 GitHub 官方 github-script Action 创建 Release。");
requireValue(workflow.includes("github.rest.repos.createRelease") && workflow.includes("prerelease: true"), "Pre-release workflow 必须创建 GitHub 预发布版本。");
requireValue(workflow.includes("github.rest.repos.uploadReleaseAsset"), "Pre-release workflow 必须上传安装器、校验和与清单。");
requireValue(workflow.includes("github-token: ${{ github.token }}"), "Pre-release workflow 必须仅使用 GitHub Actions 自动注入的短期 Token。");
requireValue(!/softprops\/action-gh-release|ncipollo\/release-action/u.test(workflow), "Pre-release workflow 不得引入第三方 GitHub Release Action。");

console.log("Windows NSIS 预发布工作流配置校验通过。");
