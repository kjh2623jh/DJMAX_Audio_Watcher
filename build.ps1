# Build on Windows with Go 1.23 or later: powershell -ExecutionPolicy Bypass -File .\build.ps1
param(
    [string]$Version = ''
)

$ErrorActionPreference = 'Stop'
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '0'

if ([string]::IsNullOrWhiteSpace($Version)) {
    $git = Get-Command git -ErrorAction SilentlyContinue
    if ($null -ne $git) {
        $changes = & git status --porcelain 2>$null
        if ($LASTEXITCODE -eq 0 -and [string]::IsNullOrWhiteSpace(($changes -join ''))) {
            $tag = & git describe --tags --exact-match HEAD 2>$null
            if ($LASTEXITCODE -eq 0 -and -not [string]::IsNullOrWhiteSpace($tag)) {
                $Version = $tag.Trim()
            }
        }
    }
}
if ([string]::IsNullOrWhiteSpace($Version)) {
    $Version = 'dev'
}
if ($Version -notmatch '^(dev|v[0-9A-Za-z][0-9A-Za-z._+-]*)$') {
    throw "Invalid application version: $Version"
}

New-Item -ItemType Directory -Force -Path 'dist' | Out-Null
$linkerFlags = "-H=windowsgui -s -w -X main.appVersion=$Version"
go build -trimpath -ldflags $linkerFlags -o 'dist/DJMAX_Audio_Watcher.exe' .
if ($LASTEXITCODE -ne 0) { throw 'go build failed' }
Write-Host "Built dist/DJMAX_Audio_Watcher.exe ($Version)"
