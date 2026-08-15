# 本脚本只配置当前 PowerShell 进程，使 Aggregation Hub 的工具、构建产物和依赖缓存优先使用 D 盘。
$ErrorActionPreference = 'Stop'

$projectRoot = Split-Path -Parent $PSScriptRoot
$toolRoot = Join-Path $projectRoot '.toolchains'

$pathsToCreate = @(
    (Join-Path $toolRoot 'go-work'),
    (Join-Path $toolRoot 'go-pkg-mod'),
    (Join-Path $toolRoot 'go-build-cache'),
    (Join-Path $toolRoot 'go-bin'),
    (Join-Path $toolRoot 'rustup'),
    (Join-Path $toolRoot 'cargo'),
    (Join-Path $toolRoot 'cargo-target'),
    (Join-Path $toolRoot 'corepack'),
    (Join-Path $toolRoot 'pnpm-home'),
    (Join-Path $toolRoot 'pnpm-store'),
    (Join-Path $toolRoot 'pnpm-cache'),
    (Join-Path $toolRoot 'npm-cache'),
    (Join-Path $toolRoot 'yarn-cache'),
    (Join-Path $toolRoot 'yarn-global'),
    (Join-Path $toolRoot 'node-compile-cache'),
    (Join-Path $toolRoot 'temp')
)
New-Item -ItemType Directory -Force -Path $pathsToCreate | Out-Null

$env:GOROOT = Join-Path $toolRoot 'go'
$env:GOPATH = Join-Path $toolRoot 'go-work'
$env:GOMODCACHE = Join-Path $toolRoot 'go-pkg-mod'
$env:GOCACHE = Join-Path $toolRoot 'go-build-cache'
$env:GOBIN = Join-Path $toolRoot 'go-bin'
$env:RUSTUP_HOME = Join-Path $toolRoot 'rustup'
$env:CARGO_HOME = Join-Path $toolRoot 'cargo'
$env:CARGO_TARGET_DIR = Join-Path $toolRoot 'cargo-target'
$env:COREPACK_HOME = Join-Path $toolRoot 'corepack'
$env:PNPM_HOME = Join-Path $toolRoot 'pnpm-home'
$env:pnpm_config_store_dir = Join-Path $toolRoot 'pnpm-store'
$env:pnpm_config_cache_dir = Join-Path $toolRoot 'pnpm-cache'
$env:npm_config_cache = Join-Path $toolRoot 'npm-cache'
$env:YARN_CACHE_FOLDER = Join-Path $toolRoot 'yarn-cache'
$env:YARN_GLOBAL_FOLDER = Join-Path $toolRoot 'yarn-global'
$env:NODE_COMPILE_CACHE = Join-Path $toolRoot 'node-compile-cache'

# 只让本项目相关进程的临时文件落到 D 盘，不修改系统全局 TEMP/TMP。
$env:TEMP = Join-Path $toolRoot 'temp'
$env:TMP = $env:TEMP

$toolPaths = @(
    (Join-Path $env:GOROOT 'bin'),
    $env:GOBIN,
    (Join-Path $env:CARGO_HOME 'bin'),
    $env:PNPM_HOME
)
$env:Path = (($toolPaths + ($env:Path -split ';')) | Where-Object { $_ } | Select-Object -Unique) -join ';'

Write-Host "Aggregation Hub D 盘工具链已载入：$toolRoot"