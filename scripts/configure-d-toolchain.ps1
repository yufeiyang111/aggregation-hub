[CmdletBinding()]
param()

# 将本项目工具链及常用包管理器的用户级目录固定到 D 盘。
# 系统级 TEMP/TMP 不在这里修改，避免影响 Windows 和其他应用。
$ErrorActionPreference = 'Stop'

$projectRoot = Split-Path -Parent $PSScriptRoot
$toolRoot = Join-Path $projectRoot '.toolchains'
$rootDrive = Split-Path -Qualifier $toolRoot
if ($rootDrive -ne 'D:') {
    throw "此配置要求项目位于 D 盘，当前工具链目录为：$toolRoot"
}

$values = [ordered]@{
    GOROOT                = Join-Path $toolRoot 'go'
    GOPATH                = Join-Path $toolRoot 'go-work'
    GOMODCACHE            = Join-Path $toolRoot 'go-pkg-mod'
    GOCACHE               = Join-Path $toolRoot 'go-build-cache'
    GOBIN                 = Join-Path $toolRoot 'go-bin'
    RUSTUP_HOME           = Join-Path $toolRoot 'rustup'
    CARGO_HOME            = Join-Path $toolRoot 'cargo'
    CARGO_TARGET_DIR      = Join-Path $toolRoot 'cargo-target'
    COREPACK_HOME         = Join-Path $toolRoot 'corepack'
    PNPM_HOME             = Join-Path $toolRoot 'pnpm-home'
    pnpm_config_store_dir = Join-Path $toolRoot 'pnpm-store'
    pnpm_config_cache_dir = Join-Path $toolRoot 'pnpm-cache'
    npm_config_cache      = Join-Path $toolRoot 'npm-cache'
    YARN_CACHE_FOLDER     = Join-Path $toolRoot 'yarn-cache'
    YARN_GLOBAL_FOLDER    = Join-Path $toolRoot 'yarn-global'
    NODE_COMPILE_CACHE    = Join-Path $toolRoot 'node-compile-cache'
}

$directories = @($values.GetEnumerator() | ForEach-Object { $_.Value })
New-Item -ItemType Directory -Force -Path $directories | Out-Null
foreach ($entry in $values.GetEnumerator()) {
    [Environment]::SetEnvironmentVariable($entry.Key, $entry.Value, 'User')
}

$pathEntries = @(
    (Join-Path $toolRoot 'go\bin'),
    $values.GOBIN,
    (Join-Path $values.CARGO_HOME 'bin'),
    $values.PNPM_HOME
)

$nodeCommand = Get-Command node -ErrorAction SilentlyContinue
if ($null -ne $nodeCommand -and $nodeCommand.Source) {
    $nodeDirectory = Split-Path -Parent $nodeCommand.Source
    if ((Split-Path -Qualifier $nodeDirectory) -eq 'D:') {
        $pathEntries += $nodeDirectory
    }
    else {
        Write-Warning "检测到 Node.js 不在 D 盘，未写入用户 Path：$nodeDirectory"
    }
}
else {
    Write-Warning '未找到 node.exe；已保留后续安装 Node.js 的 D 盘缓存配置。'
}

$currentPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$currentEntries = @()
if (-not [string]::IsNullOrWhiteSpace($currentPath)) {
    $currentEntries = $currentPath -split ';' | Where-Object { $_ }
}
$mergedPath = (($currentEntries + $pathEntries) | Select-Object -Unique) -join ';'
[Environment]::SetEnvironmentVariable('Path', $mergedPath, 'User')

Write-Host "已写入用户级 D 盘开发环境变量：$toolRoot"
Write-Host '请重新打开 PowerShell、Windows Terminal 或 Codex 终端，使用户级 Path 和变量完整生效。'