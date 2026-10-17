# Download the complete, unmodified NirSoft SoundVolumeCommandLine 1.28 x64 distribution.
# Pin both the official archive and the executable by SHA-256.
# Usage: powershell -ExecutionPolicy Bypass -File .\install_svcl.ps1 [-Destination .\dist]
param(
    [string]$Destination = $PSScriptRoot
)
$ErrorActionPreference = 'Stop'
$version = '1.28'
$url = 'https://www.nirsoft.net/utils/svcl-x64.zip'
$expectedZipSHA256 = '7BA008E9ECE8B3EDA323EF01711E4647EB7F40B28DC25F98B2ED6A738810BFCD'
$expectedExeSHA256 = '0C4738296D253495BC995DA7B5938E27B306C8F204DC9F7D4D5D455F1D8D3E38'
$dest = [System.IO.Path]::GetFullPath($Destination)
New-Item -ItemType Directory -Force -Path $dest | Out-Null
$temp = Join-Path ([System.IO.Path]::GetTempPath()) ('djmax-svcl-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $temp | Out-Null
try {
    $archive = Join-Path $temp 'svcl-x64.zip'
    Write-Host "Downloading official SoundVolumeCommandLine $version x64..."
    Invoke-WebRequest -Uri $url -OutFile $archive -UseBasicParsing
    $zipHash = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash
    if ($zipHash -ne $expectedZipSHA256) {
        throw "SVCL archive SHA256 mismatch. Expected $expectedZipSHA256, got $zipHash. Refusing to install."
    }
    $expanded = Join-Path $temp 'unpacked'
    Expand-Archive -LiteralPath $archive -DestinationPath $expanded -Force
    $exe = Join-Path $expanded 'svcl.exe'
    if (-not (Test-Path -LiteralPath $exe)) { throw 'Official archive is missing svcl.exe' }
    $exeHash = (Get-FileHash -LiteralPath $exe -Algorithm SHA256).Hash
    if ($exeHash -ne $expectedExeSHA256) {
        throw "SVCL executable SHA256 mismatch. Expected $expectedExeSHA256, got $exeHash. Refusing to install."
    }
    # Keep *all* upstream distribution files, unmodified, as required by NirSoft's license.
    # Avoid overwriting a different SVCL version or any unrelated existing files.
    Get-ChildItem -LiteralPath $expanded -Force | ForEach-Object {
        $target = Join-Path $dest $_.Name
        if (Test-Path -LiteralPath $target) {
            if ($_.PSIsContainer) { throw "Destination already contains a directory: $target" }
            $existingHash = (Get-FileHash -LiteralPath $target -Algorithm SHA256).Hash
            $incomingHash = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash
            if ($existingHash -ne $incomingHash) { throw "Refusing to overwrite a different file: $target" }
        }
    }
    Get-ChildItem -LiteralPath $expanded -Force | ForEach-Object { Copy-Item -LiteralPath $_.FullName -Destination $dest -Recurse -Force -ErrorAction Stop }
    # Preserve the exact official ZIP as an additional verifiable upstream artifact.
    Copy-Item -LiteralPath $archive -Destination (Join-Path $dest 'svcl-x64.zip') -Force
    Write-Host "Installed complete, verified SVCL $version distribution in $dest"
} finally {
    Remove-Item -LiteralPath $temp -Recurse -Force -ErrorAction SilentlyContinue
}
