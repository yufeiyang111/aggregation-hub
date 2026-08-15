[CmdletBinding()]
param(
    [switch]$SkipBuildTools
)

# 本脚本安装或复用 Aggregation Hub 开发工具链；大型下载、缓存与安装目录优先放在项目 D 盘 .toolchains 下。
$ErrorActionPreference = 'Stop'

$projectRoot = Split-Path -Parent $PSScriptRoot
$toolRoot = Join-Path $projectRoot '.toolchains'
$downloads = Join-Path $toolRoot 'downloads'
$goRoot = Join-Path $toolRoot 'go'
$vsInstallerDirectory = Join-Path $toolRoot 'vs-installer'
$vsCache = Join-Path $toolRoot 'vs-cache'
$vsShared = Join-Path $toolRoot 'vs-shared'
$vsInstall = Join-Path $toolRoot 'vs-buildtools'

. (Join-Path $PSScriptRoot 'enter-d-toolchain.ps1')
& (Join-Path $PSScriptRoot 'configure-d-toolchain.ps1')

function Require-FreeSpace {
    param([double]$MinimumGiB)

    $drive = Get-PSDrive -Name D
    $freeGiB = $drive.Free / 1GB
    if ($freeGiB -lt $MinimumGiB) {
        throw ("D 盘可用空间不足：当前 {0:N2} GiB，需要至少 {1:N2} GiB。" -f $freeGiB, $MinimumGiB)
    }
}

function Find-ReusableDownload {
    param(
        [Parameter(Mandatory = $true)][string]$Destination,
        [Parameter(Mandatory = $true)][long]$MinimumReusableBytes
    )

    $directory = Split-Path -Parent $Destination
    $baseName = [System.IO.Path]::GetFileNameWithoutExtension($Destination)
    $extension = [System.IO.Path]::GetExtension($Destination)
    $retryFilter = "{0}.retry-*{1}" -f $baseName, $extension
    $candidates = @(Get-Item -LiteralPath $Destination -ErrorAction SilentlyContinue) + @(
        Get-ChildItem -LiteralPath $directory -File -Filter $retryFilter -ErrorAction SilentlyContinue
    )

    foreach ($candidate in ($candidates | Sort-Object LastWriteTime -Descending)) {
        if ($null -ne $candidate -and $candidate.Length -ge $MinimumReusableBytes) {
            return $candidate.FullName
        }
    }

    return $null
}

function Download-File {
    param(
        [Parameter(Mandatory = $true)][string]$Uri,
        [Parameter(Mandatory = $true)][string]$Destination,
        [Parameter(Mandatory = $true)][long]$MinimumReusableBytes,
        [switch]$SkipReuse
    )

    if (-not $SkipReuse) {
        $reusable = Find-ReusableDownload -Destination $Destination -MinimumReusableBytes $MinimumReusableBytes
        if ($null -ne $reusable) {
            Write-Host "复用已下载文件：$reusable"
            return $reusable
        }
    }

    # 不覆盖或删除既有文件；每次新下载写入 D 盘的唯一重试文件。
    $directory = Split-Path -Parent $Destination
    $baseName = [System.IO.Path]::GetFileNameWithoutExtension($Destination)
    $extension = [System.IO.Path]::GetExtension($Destination)
    $output = Join-Path $directory ("{0}.retry-{1}{2}" -f $baseName, [Guid]::NewGuid().ToString('N'), $extension)

    Write-Host "下载：$Uri"
    & curl.exe --fail --location --proto '=https' --tlsv1.2 --retry 4 --retry-delay 2 --retry-all-errors --output $output $Uri
    if ($LASTEXITCODE -ne 0) {
        Write-Host 'curl 下载失败，改用 PowerShell HTTPS 下载回退。'
        try {
            Invoke-WebRequest -Uri $Uri -OutFile $output -UseBasicParsing
        }
        catch {
            throw "下载失败：$Uri"
        }
    }

    if (-not (Test-Path -LiteralPath $output) -or (Get-Item -LiteralPath $output).Length -lt $MinimumReusableBytes) {
        throw "下载结果不完整：$Uri"
    }
    return $output
}

function Test-RustCommand {
    param(
        [Parameter(Mandatory = $true)][string]$Executable,
        [Parameter(Mandatory = $true)][string[]]$Arguments
    )

    if (-not (Test-Path -LiteralPath $Executable)) {
        return $false
    }

    & $Executable @Arguments 1>$null 2>$null
    return $LASTEXITCODE -eq 0
}

function Ensure-RustComponent {
    param(
        [Parameter(Mandatory = $true)][string]$Name,
        [Parameter(Mandatory = $true)][string]$RustupExecutable,
        [Parameter(Mandatory = $true)][string]$Toolchain,
        [Parameter(Mandatory = $true)][string]$ProbeExecutable,
        [Parameter(Mandatory = $true)][string[]]$ProbeArguments
    )

    if (Test-RustCommand -Executable $ProbeExecutable -Arguments $ProbeArguments) {
        return
    }

    # Rustup 元数据可能在中断安装后误报“已安装”；先移除再安装能恢复实际可执行文件。
    & $RustupExecutable component remove $Name --toolchain $Toolchain
    if ($LASTEXITCODE -ne 0) {
        throw "移除异常的 Rust 组件失败：$Name，退出码：$LASTEXITCODE"
    }
    & $RustupExecutable component add $Name --toolchain $Toolchain
    if ($LASTEXITCODE -ne 0) {
        throw "安装 Rust 组件失败：$Name，退出码：$LASTEXITCODE"
    }
    if (-not (Test-RustCommand -Executable $ProbeExecutable -Arguments $ProbeArguments)) {
        throw "Rust 组件安装后仍不可用：$Name"
    }
}

Require-FreeSpace -MinimumGiB 20
New-Item -ItemType Directory -Force -Path $downloads, $vsInstallerDirectory, $vsCache, $vsShared, $vsInstall | Out-Null

$goVersion = 'go1.26.5'
$goArchiveName = "$goVersion.windows-amd64.zip"
$goArchiveDestination = Join-Path $downloads $goArchiveName
$goMetadata = Invoke-RestMethod -Uri 'https://go.dev/dl/?mode=json&include=all'
$goRelease = $goMetadata | Where-Object { $_.version -eq $goVersion } | Select-Object -First 1
if ($null -eq $goRelease) {
    throw "Go 官方下载列表中不存在 $goVersion"
}
$goFile = $goRelease.files | Where-Object { $_.filename -eq $goArchiveName } | Select-Object -First 1
if ($null -eq $goFile) {
    throw "Go 官方下载列表中不存在 $goArchiveName"
}

$goArchive = $null
$goCandidates = @(Get-Item -LiteralPath $goArchiveDestination -ErrorAction SilentlyContinue) + @(
    Get-ChildItem -LiteralPath $downloads -File -Filter 'go1.26.5.windows-amd64.retry-*.zip' -ErrorAction SilentlyContinue
)
foreach ($candidate in ($goCandidates | Sort-Object LastWriteTime -Descending)) {
    if ($null -eq $candidate -or $candidate.Length -lt 1048576) {
        continue
    }
    $candidateHash = (Get-FileHash -LiteralPath $candidate.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($candidateHash -eq $goFile.sha256.ToLowerInvariant()) {
        $goArchive = $candidate.FullName
        break
    }
}
if ($null -eq $goArchive) {
    $goArchive = Download-File -Uri ("https://go.dev/dl/{0}" -f $goFile.filename) -Destination $goArchiveDestination -MinimumReusableBytes 1048576 -SkipReuse
}
$actualHash = (Get-FileHash -LiteralPath $goArchive -Algorithm SHA256).Hash.ToLowerInvariant()
if ($actualHash -ne $goFile.sha256.ToLowerInvariant()) {
    throw "Go ZIP SHA-256 校验失败：$goArchive"
}
Write-Host "Go ZIP SHA-256 校验通过：$actualHash"

$goExecutable = Join-Path $goRoot 'bin\go.exe'
if (-not (Test-Path -LiteralPath $goExecutable)) {
    # Expand-Archive 与 Windows PowerShell 5.1 兼容；归档仍保留 .zip 扩展名。
    Expand-Archive -LiteralPath $goArchive -DestinationPath $toolRoot -Force
}
if (-not (Test-Path -LiteralPath $goExecutable)) {
    throw "Go 解压后未找到：$goExecutable"
}
& $goExecutable version

$vsDevCommand = Join-Path $vsInstall 'Common7\Tools\VsDevCmd.bat'
$vsCompiler = Get-ChildItem -LiteralPath $vsInstall -Filter 'cl.exe' -Recurse -ErrorAction SilentlyContinue |
    Where-Object { $_.FullName -match 'Hostx64\\x64\\cl\.exe$' } |
    Select-Object -First 1
if (-not $SkipBuildTools -and ($null -eq $vsCompiler -or -not (Test-Path -LiteralPath $vsDevCommand))) {
    $vsInstaller = Download-File -Uri 'https://aka.ms/vs/17/release/vs_buildtools.exe' -Destination (Join-Path $vsInstallerDirectory 'vs_buildtools.exe') -MinimumReusableBytes 1048576

    Write-Host "安装 Visual Studio Build Tools 到：$vsInstall"
    & $vsInstaller `
        '--quiet' `
        '--wait' `
        '--norestart' `
        '--installPath' $vsInstall `
        '--path' ("cache={0}" -f $vsCache) `
        '--path' ("shared={0}" -f $vsShared) `
        '--add' 'Microsoft.VisualStudio.Workload.VCTools' `
        '--includeRecommended'
    if ($LASTEXITCODE -ne 0) {
        throw "Visual Studio Build Tools 安装失败，退出码：$LASTEXITCODE"
    }
}
elseif (-not $SkipBuildTools) {
    Write-Host "复用现有 Visual Studio Build Tools：$vsInstall"
}

$rustupExecutable = Join-Path $env:CARGO_HOME 'bin\rustup.exe'
$rustcExecutable = Join-Path $env:CARGO_HOME 'bin\rustc.exe'
$cargoExecutable = Join-Path $env:CARGO_HOME 'bin\cargo.exe'
$rustToolchain = 'stable-x86_64-pc-windows-msvc'
if (-not (Test-Path -LiteralPath $rustupExecutable)) {
    $rustupInstaller = Download-File -Uri 'https://win.rustup.rs/x86_64' -Destination (Join-Path $downloads 'rustup-init.exe') -MinimumReusableBytes 1048576
    Write-Host "安装 Rust 到：$env:RUSTUP_HOME 和 $env:CARGO_HOME"
    & $rustupInstaller '-y' '--profile' 'minimal' '--default-toolchain' $rustToolchain '--no-modify-path'
    if ($LASTEXITCODE -ne 0) {
        throw "Rustup 安装失败，退出码：$LASTEXITCODE"
    }
}

if (-not (Test-RustCommand -Executable $rustcExecutable -Arguments @('--version'))) {
    & $rustupExecutable toolchain install $rustToolchain --profile minimal --no-self-update
    if ($LASTEXITCODE -ne 0) {
        throw "Rust toolchain 安装失败，退出码：$LASTEXITCODE"
    }
}

Ensure-RustComponent -Name 'cargo' -RustupExecutable $rustupExecutable -Toolchain $rustToolchain -ProbeExecutable $cargoExecutable -ProbeArguments @('--version')
Ensure-RustComponent -Name 'rustfmt' -RustupExecutable $rustupExecutable -Toolchain $rustToolchain -ProbeExecutable $cargoExecutable -ProbeArguments @('fmt', '--version')
Ensure-RustComponent -Name 'clippy' -RustupExecutable $rustupExecutable -Toolchain $rustToolchain -ProbeExecutable $cargoExecutable -ProbeArguments @('clippy', '--version')

& $rustcExecutable --version
& $cargoExecutable --version
& $goExecutable env GOROOT GOPATH GOMODCACHE GOCACHE GOBIN

Write-Host 'D 盘开发工具链安装完成。请重新打开终端以加载用户级环境变量。'