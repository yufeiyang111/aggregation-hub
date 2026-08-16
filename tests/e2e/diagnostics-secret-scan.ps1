[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [ValidateNotNullOrEmpty()]
    [string]$ArchivePath
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$allowedEntries = @(
    "credential-store.json",
    "manifest.json",
    "migration.json",
    "provider-health.json",
    "recent-errors.json",
    "runtime.json"
)
$maximumEntryBytes = 1MB
$forbiddenPatterns = @(
    "(?i)\bBearer\s+[^\s;,]+",
    "(?i)\bx-api-key\s*[:=]\s*[^\s;,]+",
    "(?i)\bcode\s*[:=]\s*[^\s&;,]+",
    "(?i)\b(prompt|tool_args|tool[_-]?arguments)\s*[:=]",
    "(?i)\b(sk-[a-z0-9_-]{20,}|ghp_[a-z0-9]{20,}|github_pat_[a-z0-9_]{20,})\b",
    "(?i)[?&](api[_-]?key|access[_-]?token|token|code|local[_-]?key)=",
    "(?i)(?:[a-z]:\\|/users/|/home/)"
)

if (-not (Test-Path -LiteralPath $ArchivePath -PathType Leaf)) {
    throw "诊断包不存在。"
}

Add-Type -AssemblyName System.IO.Compression.FileSystem
$archive = [System.IO.Compression.ZipFile]::OpenRead([System.IO.Path]::GetFullPath($ArchivePath))
try {
    $entryNames = @($archive.Entries | ForEach-Object { $_.FullName } | Sort-Object)
    $expectedNames = @($allowedEntries | Sort-Object)
    if ($entryNames.Count -ne $expectedNames.Count -or (Compare-Object -ReferenceObject $expectedNames -DifferenceObject $entryNames)) {
        throw "诊断包条目不符合固定 allowlist。"
    }

    foreach ($entry in $archive.Entries) {
        if ($entry.FullName -match "[\\/]" -or $entry.FullName.Contains("..")) {
            throw "诊断包包含不安全路径条目。"
        }
        if ($entry.Length -gt $maximumEntryBytes) {
            throw "诊断包条目超过大小上限。"
        }

        $stream = $entry.Open()
        try {
            $reader = [System.IO.StreamReader]::new($stream, [System.Text.Encoding]::UTF8, $true)
            try {
                $payload = $reader.ReadToEnd()
            }
            finally {
                $reader.Dispose()
            }
        }
        finally {
            $stream.Dispose()
        }

        try {
            $null = $payload | ConvertFrom-Json -ErrorAction Stop
        }
        catch {
            throw "诊断包条目不是有效 JSON。"
        }

        foreach ($pattern in $forbiddenPatterns) {
            if ([System.Text.RegularExpressions.Regex]::IsMatch($payload, $pattern)) {
                throw "诊断包包含禁止的敏感标记。"
            }
        }
    }
}
finally {
    $archive.Dispose()
}

Write-Output "PASS: 诊断包条目与敏感标记检查通过。"
