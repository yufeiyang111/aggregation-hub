[CmdletBinding()]
param(
    [string]$OutputRoot,
    [string]$Version
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
if ([string]::IsNullOrWhiteSpace($OutputRoot)) {
    $OutputRoot = Join-Path $repoRoot 'artifacts\release'
}
$configPath = Join-Path $repoRoot 'apps\desktop\src-tauri\tauri.conf.json'
$config = Get-Content -LiteralPath $configPath -Raw | ConvertFrom-Json
if ([string]::IsNullOrWhiteSpace($config.version) -or [string]::IsNullOrWhiteSpace($config.productName)) {
    throw 'Tauri 产品版本或名称无效。'
}

$releaseVersion = [string]$config.version
if (-not [string]::IsNullOrWhiteSpace($Version)) {
    $requestedVersion = $Version.Trim()
    if ($requestedVersion -notmatch '^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$') {
        throw '发布版本必须是合法的 SemVer 字符串。'
    }
    if ($requestedVersion -cne $releaseVersion) {
        throw "发布版本 $requestedVersion 与 tauri.conf.json 中的 $releaseVersion 不一致。请先更新单一版本源。"
    }
}

function Invoke-Pnpm {
    param(
        [Parameter(Mandatory = $true)]
        [string[]]$Arguments
    )

    & pnpm @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "pnpm $($Arguments -join ' ') 失败，退出码: $LASTEXITCODE"
    }
}

function Find-NsisInstallerDirectory {
    param(
        [Parameter(Mandatory = $true)]
        [string]$RepositoryRoot
    )

    $targetRoots = @(
        $env:CARGO_TARGET_DIR,
        (Join-Path $RepositoryRoot '.toolchains\cargo-target'),
        (Join-Path $RepositoryRoot 'apps\desktop\src-tauri\target')
    ) | Where-Object { -not [string]::IsNullOrWhiteSpace($_) } | Select-Object -Unique

    $directories = foreach ($targetRoot in $targetRoots) {
        $candidate = Join-Path $targetRoot 'release\bundle\nsis'
        if (Test-Path -LiteralPath $candidate -PathType Container) {
            Get-Item -LiteralPath $candidate
        }
    }

    $installerFiles = foreach ($directory in $directories) {
        Get-ChildItem -LiteralPath $directory.FullName -File -Filter '*-setup.exe'
    }

    if (@($installerFiles).Count -ne 1) {
        $found = (@($installerFiles) | ForEach-Object { $_.FullName }) -join '; '
        throw "未找到唯一的 NSIS Setup.exe。候选文件：$found"
    }

    return @($installerFiles)[0]
}

function Assert-InstallerDoesNotContainKnownSecretMarkers {
    param(
        [Parameter(Mandatory = $true)]
        [System.IO.FileInfo]$Installer
    )

    if ($Installer.Length -le 0 -or $Installer.Length -gt 1GB) {
        throw "安装器大小异常：$($Installer.Length) 字节"
    }

    # 使用 28591（ISO-8859-1）而非 .NET Core 专属的 ::Latin1，兼容 Windows PowerShell 5.1。
    $text = [System.Text.Encoding]::GetEncoding(28591).GetString([System.IO.File]::ReadAllBytes($Installer.FullName))
    $patterns = @(
        @{ Name = 'Local Access Key'; Pattern = '\bah_local_[A-Za-z0-9_-]{24,}\b' },
        @{ Name = 'OpenAI 风格 API Key'; Pattern = '\bsk-[A-Za-z0-9_-]{20,}\b' },
        @{ Name = 'GitHub Token'; Pattern = '\bgh[pousr]_[A-Za-z0-9_]{20,}\b' },
        @{ Name = 'SQLite 数据库头'; Pattern = 'SQLite format 3\x00' }
    )

    foreach ($entry in $patterns) {
        if ($text -match $entry.Pattern) {
            throw "安装器包含禁止的 $($entry.Name) 标记。"
        }
    }
}

Push-Location $repoRoot
try {
    Invoke-Pnpm -Arguments @('check')
    Invoke-Pnpm -Arguments @('build:desktop')


    $installer = Find-NsisInstallerDirectory -RepositoryRoot $repoRoot
    Assert-InstallerDoesNotContainKnownSecretMarkers -Installer $installer

    $normalizedProductName = ($config.productName -replace '[^A-Za-z0-9]+', '-').Trim('-')
    $versionDirectory = "v{0}" -f $releaseVersion
    $artifactDirectory = Join-Path $OutputRoot (Join-Path $versionDirectory 'windows-x64')
    if (Test-Path -LiteralPath $artifactDirectory) {
        throw "发布工件目录已存在，为避免覆盖已有文件而停止：$artifactDirectory"
    }
    New-Item -ItemType Directory -Path $artifactDirectory | Out-Null

    $installerName = '{0}_{1}_x64-setup.exe' -f $normalizedProductName, $releaseVersion
    $installerDestination = Join-Path $artifactDirectory $installerName
    Copy-Item -LiteralPath $installer.FullName -Destination $installerDestination

    $hash = (Get-FileHash -LiteralPath $installerDestination -Algorithm SHA256).Hash.ToLowerInvariant()
    $hashPath = "$installerDestination.sha256"
    [System.IO.File]::WriteAllText($hashPath, "$hash *$installerName`n", (New-Object System.Text.UTF8Encoding($false)))

    $manifest = [ordered]@{
        product = $config.productName
        version = $releaseVersion
        platform = 'windows-x64'
        installer = $installerName
        sha256 = $hash
        size_bytes = (Get-Item -LiteralPath $installerDestination).Length
        generated_at_utc = [DateTime]::UtcNow.ToString('o')
        signed = $false
        webview2_install_mode = 'downloadBootstrapper'
    } | ConvertTo-Json -Depth 3
    [System.IO.File]::WriteAllText((Join-Path $artifactDirectory 'manifest.json'), $manifest + "`n", (New-Object System.Text.UTF8Encoding($false)))

    Write-Output "已生成 Windows 发布工件：$artifactDirectory"
}
finally {
    Pop-Location
}