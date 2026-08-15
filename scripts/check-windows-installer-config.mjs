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

requireValue(existsSync(workflowPath), "Windows Release Build workflow 不存在。");
const workflow = readFileSync(workflowPath, "utf8");
requireValue(/^on:\s*\r?\n\s*workflow_dispatch:\s*\r?\n\s*inputs:\s*\r?\n\s*version:/m.test(workflow), "Release workflow 必须要求手动输入版本号。");
requireValue(/version:\s*\r?\n\s*description:.*\r?\n\s*required:\s*true\s*\r?\n\s*type:\s*string/m.test(workflow), "Release workflow 的版本输入必须为必填字符串。");
requireValue(workflow.includes('RELEASE_VERSION: ${{ inputs.version }}'), "Release workflow 必须通过环境变量传递版本输入。");
requireValue(workflow.includes('pnpm release:windows -- -Version "$env:RELEASE_VERSION"'), "Release workflow 必须把版本输入传给受控发布构建脚本。");
requireValue(workflow.includes("actions/upload-artifact@v4"), "Release workflow 必须上传构建工件。");
requireValue(!/\bgh\s+release\b|softprops\/action-gh-release|ncipollo\/release-action/u.test(workflow), "Release workflow 不得创建或发布 GitHub Release。");

console.log("Windows NSIS 安装器配置校验通过。");