[CmdletBinding()]
param(
    [Parameter()]
    [switch]$RunLive,

    [Parameter()]
    [string]$LocalBaseUrl = 'http://127.0.0.1:18443/v1',

    [Parameter()]
    [string]$Model = 'provider-slug/upstream-model-id',

    [Parameter()]
    [string]$LocalAccessKeyEnvVar = 'AGGREGATION_HUB_LOCAL_KEY'
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$script:MaximumOutputBytes = 2MB
$script:SuccessMarker = 'AGGREGATION_HUB_CODEX_SMOKE_OK'

function Assert-SafeConfiguration {
    param(
        [Parameter(Mandatory = $true)]
        [string]$BaseUrl,
        [Parameter(Mandatory = $true)]
        [string]$PublicModelId,
        [Parameter(Mandatory = $true)]
        [string]$EnvironmentVariableName
    )

    $uri = $null
    if (-not [System.Uri]::TryCreate($BaseUrl, [System.UriKind]::Absolute, [ref]$uri)) {
        throw 'LocalBaseUrl 必须是绝对 HTTP URL。'
    }

    if ($uri.Scheme -ne 'http' -or $uri.UserInfo.Length -ne 0 -or $uri.Query.Length -ne 0 -or $uri.Fragment.Length -ne 0) {
        throw 'LocalBaseUrl 只能是无用户信息、Query 或 Fragment 的本地 HTTP URL。'
    }

    if ($uri.Host -notin @('127.0.0.1', 'localhost', '::1')) {
        throw 'Live smoke 只能访问回环地址，拒绝局域网和公网目标。'
    }

    if ($uri.AbsolutePath -ne '/v1') {
        throw 'LocalBaseUrl 必须指向 Aggregation Hub 的 /v1 根路径。'
    }

    if ($PublicModelId -notmatch '^[A-Za-z0-9][A-Za-z0-9._/-]{0,255}$') {
        throw 'Model 只能使用 Public Model ID 允许的字符。'
    }

    if ($EnvironmentVariableName -notmatch '^[A-Za-z_][A-Za-z0-9_]*$') {
        throw 'LocalAccessKeyEnvVar 不是有效的环境变量名称。'
    }
}

function Get-RequiredProcessEnvironmentVariable {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name
    )

    $value = [System.Environment]::GetEnvironmentVariable($Name, 'Process')
    if ([string]::IsNullOrWhiteSpace($value)) {
        throw "未检测到进程环境变量 $Name。请仅在当前终端会话设置本地访问密钥后重试；脚本不会读取 .env、Credential Manager 或 cc-switch。"
    }

    return $value
}

function Read-BoundedTextFile {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,
        [Parameter(Mandatory = $true)]
        [string]$Label
    )

    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "$Label 未生成。"
    }

    $length = (Get-Item -LiteralPath $Path).Length
    if ($length -gt $script:MaximumOutputBytes) {
        throw "$Label 超过 $script:MaximumOutputBytes 字节上限，已拒绝读取。"
    }

    return [System.IO.File]::ReadAllText($Path, [System.Text.Encoding]::UTF8)
}

function Remove-TemporaryDirectory {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )

    if (-not (Test-Path -LiteralPath $Path -PathType Container)) {
        return
    }

    $tempRoot = [System.IO.Path]::GetFullPath([System.IO.Path]::GetTempPath()).TrimEnd([System.IO.Path]::DirectorySeparatorChar)
    $resolvedPath = (Resolve-Path -LiteralPath $Path).Path
    $expectedPrefix = "$tempRoot$([System.IO.Path]::DirectorySeparatorChar)aggregation-hub-codex-smoke-"
    if (-not $resolvedPath.StartsWith($expectedPrefix, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw '拒绝清理未由 smoke 脚本创建的目录。'
    }

    Remove-Item -LiteralPath $resolvedPath -Recurse -Force
}

Assert-SafeConfiguration -BaseUrl $LocalBaseUrl -PublicModelId $Model -EnvironmentVariableName $LocalAccessKeyEnvVar

$codexCommand = @(Get-Command 'codex' -CommandType Application -ErrorAction Stop)[0]
$versionOutput = @(& $codexCommand.Source '--version' 2>&1)
if ($LASTEXITCODE -ne 0) {
    throw "Codex 版本检查失败，退出码：$LASTEXITCODE"
}

$codexVersion = ($versionOutput | ForEach-Object { $_.ToString().Trim() } | Where-Object { $_.Length -gt 0 } | Select-Object -First 1)
if ([string]::IsNullOrWhiteSpace($codexVersion)) {
    throw 'Codex 未返回可识别的版本信息。'
}

$tempDirectory = Join-Path ([System.IO.Path]::GetTempPath()) ("aggregation-hub-codex-smoke-{0}" -f [System.Guid]::NewGuid().ToString('N'))
$temporaryCodexHome = Join-Path $tempDirectory 'codex-home'
$temporaryWorkspace = Join-Path $tempDirectory 'workspace'
$configPath = Join-Path $temporaryCodexHome 'config.toml'
$eventsPath = Join-Path $tempDirectory 'codex-events.jsonl'
$standardErrorPath = Join-Path $tempDirectory 'codex-stderr.txt'
$finalMessagePath = Join-Path $tempDirectory 'codex-final-message.txt'
$hadCodexHome = $null -ne [System.Environment]::GetEnvironmentVariable('CODEX_HOME', 'Process')
$originalCodexHome = [System.Environment]::GetEnvironmentVariable('CODEX_HOME', 'Process')

try {
    New-Item -ItemType Directory -Path $temporaryCodexHome, $temporaryWorkspace -Force | Out-Null

    # 配置只引用环境变量名；本地访问密钥不会写入磁盘。
    $configToml = @"
model_provider = "aggregation_hub"
model = "$Model"

[model_providers.aggregation_hub]
name = "Aggregation Hub live smoke"
base_url = "$LocalBaseUrl"
env_key = "$LocalAccessKeyEnvVar"
wire_api = "responses"
requires_openai_auth = false
request_max_retries = 0
stream_max_retries = 0
"@
    [System.IO.File]::WriteAllText($configPath, $configToml, (New-Object System.Text.UTF8Encoding($false)))

    Write-Host "[preflight] Codex: $codexVersion"
    Write-Host "[preflight] 临时 CODEX_HOME 已创建；目标为受限回环地址。"
    Write-Host "[preflight] 不会修改用户 Codex 配置、系统环境变量或读取任何凭据文件。"

    if (-not $RunLive) {
        Write-Host '[preflight] 预检通过。若需真实 L4 Function 场景，请显式传入 -RunLive。'
        return
    }

    $localAccessKey = Get-RequiredProcessEnvironmentVariable -Name $LocalAccessKeyEnvVar
    $smokeInputPath = Join-Path $temporaryWorkspace 'smoke-input.txt'
    [System.IO.File]::WriteAllText($smokeInputPath, 'Aggregation Hub Codex live smoke input.', (New-Object System.Text.UTF8Encoding($false)))

    [System.Environment]::SetEnvironmentVariable('CODEX_HOME', $temporaryCodexHome, 'Process')
    $prompt = "Use an available tool to read smoke-input.txt. Do not modify files and do not use the network. Then reply exactly with $script:SuccessMarker."

    Push-Location $temporaryWorkspace
    try {
        & $codexCommand.Source 'exec' '--json' '--skip-git-repo-check' '--output-last-message' $finalMessagePath $prompt 1> $eventsPath 2> $standardErrorPath
        if ($LASTEXITCODE -ne 0) {
            throw "Codex live smoke 失败，退出码：$LASTEXITCODE。详细输出仅保留在本次临时目录，并会在退出时删除。"
        }
    }
    finally {
        Pop-Location
    }

    $events = Read-BoundedTextFile -Path $eventsPath -Label 'Codex JSON 事件输出'
    $standardError = if (Test-Path -LiteralPath $standardErrorPath -PathType Leaf) { Read-BoundedTextFile -Path $standardErrorPath -Label 'Codex 标准错误输出' } else { '' }
    $finalMessage = Read-BoundedTextFile -Path $finalMessagePath -Label 'Codex 最终消息'

    if ($events -notmatch '"type"\s*:\s*"(?:command_execution|function_call)"') {
        throw 'Codex 未产生可识别的 Tool/Function 事件，不能将本次运行记为 Function 场景成功。'
    }

    if ($finalMessage -notmatch [System.Text.RegularExpressions.Regex]::Escape($script:SuccessMarker)) {
        throw 'Codex 最终消息未包含预期成功标记。'
    }

    # 只在内存中比较密钥，不把值写入控制台、文件、错误或 Git 证据。
    $escapedLocalAccessKey = [System.Text.RegularExpressions.Regex]::Escape($localAccessKey)
    if ($events -match $escapedLocalAccessKey -or $standardError -match $escapedLocalAccessKey -or $finalMessage -match $escapedLocalAccessKey) {
        throw '检测到本地访问密钥泄漏到 Codex 运行输出。'
    }

    Write-Host '[live] Codex Responses Function 场景通过；未输出或持久化本地访问密钥。'
}
finally {
    if ($hadCodexHome) {
        [System.Environment]::SetEnvironmentVariable('CODEX_HOME', $originalCodexHome, 'Process')
    }
    else {
        [System.Environment]::SetEnvironmentVariable('CODEX_HOME', $null, 'Process')
    }

    Remove-TemporaryDirectory -Path $tempDirectory
}
