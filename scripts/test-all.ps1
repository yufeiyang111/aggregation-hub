[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot

function Invoke-GateStep {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name,
        [Parameter(Mandatory = $true)]
        [string]$Command,
        [Parameter()]
        [string[]]$Arguments = @()
    )

    Write-Host "[gate] $Name"
    & $Command @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "[gate] $Name 失败，退出码: $LASTEXITCODE"
    }
}

Push-Location $repoRoot
try {
    # 按文档、Web、Core、Rust、契约的顺序串行执行；不注入任何 Provider 凭据。
    Invoke-GateStep -Name '文档检查' -Command 'pnpm' -Arguments @('docs:check')
    Invoke-GateStep -Name 'Web 类型检查' -Command 'pnpm' -Arguments @('web:typecheck')
    Invoke-GateStep -Name 'Web lint' -Command 'pnpm' -Arguments @('web:lint')
    Invoke-GateStep -Name 'Web 测试' -Command 'pnpm' -Arguments @('web:test')
    Invoke-GateStep -Name 'Core vet' -Command 'pnpm' -Arguments @('core:vet')
    Invoke-GateStep -Name 'Core 测试' -Command 'pnpm' -Arguments @('core:test')
    Invoke-GateStep -Name 'Rust 测试' -Command 'pnpm' -Arguments @('rust:test')
    Invoke-GateStep -Name '契约 Node 自测和 fixture 校验' -Command 'node' -Arguments @('scripts/check-generated.mjs', '--self-test')
    Invoke-GateStep -Name 'OpenAPI Redocly lint' -Command 'pnpm' -Arguments @('--dir', 'apps/desktop', 'exec', 'redocly', 'lint', (Join-Path $repoRoot 'contracts\control-plane.openapi.yaml'))

    Write-Host '[gate] Phase 0 基础检查全部通过'
}
finally {
    Pop-Location
}