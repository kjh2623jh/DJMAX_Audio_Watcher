# Build on Windows with Go 1.23 or later: powershell -ExecutionPolicy Bypass -File .\build.ps1
$ErrorActionPreference = 'Stop'
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '0'
New-Item -ItemType Directory -Force -Path 'dist' | Out-Null
go build -trimpath -ldflags '-H=windowsgui -s -w' -o 'dist/DJMAX_Audio_Watcher_v5.exe' .
if ($LASTEXITCODE -ne 0) { throw 'go build failed' }
Write-Host 'Built dist/DJMAX_Audio_Watcher_v5.exe'
