$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0

$root = Split-Path -Parent $PSScriptRoot
$targetTriple = 'x86_64-pc-windows-msvc'
$coreDirectory = Join-Path $root 'apps/core'
$outputDirectory = Join-Path $root 'apps/desktop/src-tauri/binaries'
$outputPath = Join-Path $outputDirectory ("aggregation-hub-core-{0}.exe" -f $targetTriple)
$goCommand = @(Get-Command 'go' -CommandType Application -ErrorAction Stop)[0]

$previousGoOs = $env:GOOS
$previousGoArch = $env:GOARCH
$previousCgoEnabled = $env:CGO_ENABLED

New-Item -ItemType Directory -Force -Path $outputDirectory | Out-Null

try {
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    $env:CGO_ENABLED = '0'

    Push-Location $coreDirectory
    try {
        $buildArguments = @(
            'build',
            '-buildvcs=false',
            '-trimpath',
            '-ldflags=-s -w',
            '-o',
            $outputPath,
            './cmd/aggregation-hub-core'
        )
        & $goCommand.Source @buildArguments
        if ($LASTEXITCODE -ne 0) {
            throw "go build failed with exit code $LASTEXITCODE"
        }
    }
    finally {
        Pop-Location
    }
}
finally {
    if ($null -eq $previousGoOs) { Remove-Item Env:GOOS -ErrorAction SilentlyContinue } else { $env:GOOS = $previousGoOs }
    if ($null -eq $previousGoArch) { Remove-Item Env:GOARCH -ErrorAction SilentlyContinue } else { $env:GOARCH = $previousGoArch }
    if ($null -eq $previousCgoEnabled) { Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue } else { $env:CGO_ENABLED = $previousCgoEnabled }
}

Write-Output $outputPath