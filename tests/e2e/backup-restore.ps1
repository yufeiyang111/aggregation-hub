# Task 5.6：备份恢复 L1 验证入口。
# 该脚本只运行真实临时 SQLite 与 Core 启动单测，不代表干净虚拟机安装验证或真实客户端联调。
$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
Push-Location (Join-Path $repoRoot 'apps\core')
try {
  go test ./internal/storage ./internal/maintenance ./cmd/aggregation-hub-core -run 'Test(Backup|Retention|OpenRuntimeDatabaseAppliesScheduledRestore)' -count=1
} finally {
  Pop-Location
}