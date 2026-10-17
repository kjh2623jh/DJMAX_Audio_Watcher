# Build a complete free distribution ZIP with the pinned, original NirSoft SVCL 1.28 files.
# Requires internet access to https://www.nirsoft.net/ on the Windows build machine.
$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot
& (Join-Path $root 'build.ps1')
if ($LASTEXITCODE -ne 0) { throw 'Watcher build failed' }
$dist = Join-Path $root 'dist'
& (Join-Path $root 'install_svcl.ps1') -Destination $dist
if ($LASTEXITCODE -ne 0) { throw 'SVCL install failed' }
Copy-Item -LiteralPath (Join-Path $root 'README.md') -Destination $dist -Force
Copy-Item -LiteralPath (Join-Path $root 'install_svcl.ps1') -Destination $dist -Force
Copy-Item -LiteralPath (Join-Path $root 'THIRD_PARTY_NOTICES.md') -Destination $dist -Force
$releaseDir = Join-Path $root 'release'
New-Item -ItemType Directory -Force -Path $releaseDir | Out-Null
$out = Join-Path $releaseDir 'DJMAX_Audio_Watcher_Windows_x64.zip'
if (Test-Path -LiteralPath $out) { Remove-Item -LiteralPath $out -Force }
Compress-Archive -Path (Join-Path $dist '*') -DestinationPath $out -CompressionLevel Optimal
Write-Host "Release package: $out"
