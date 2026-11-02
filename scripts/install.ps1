# Install the certwatch CLI on Windows from GitHub Releases.
#
#   irm https://raw.githubusercontent.com/titiabdelghafour8-lab/certwatch/main/scripts/install.ps1 | iex
#
# Environment:
#   CERTWATCH_VERSION      release tag to install (default: latest)
#   CERTWATCH_INSTALL_DIR  target directory (default: %LOCALAPPDATA%\certwatch\bin)
$ErrorActionPreference = 'Stop'

$repo = 'titiabdelghafour8-lab/certwatch'
$version = if ($env:CERTWATCH_VERSION) { $env:CERTWATCH_VERSION } else { 'latest' }
$arch = if ([Environment]::Is64BitOperatingSystem) {
    if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
} else { throw 'certwatch requires a 64-bit Windows' }

$base = if ($version -eq 'latest') { "https://github.com/$repo/releases/latest/download" } else { "https://github.com/$repo/releases/download/$version" }
$archive = "certwatch_windows_$arch.zip"
$tmp = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null

try {
    Write-Host "Downloading $archive ($version)..."
    Invoke-WebRequest "$base/$archive" -OutFile (Join-Path $tmp $archive) -UseBasicParsing
    Invoke-WebRequest "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt') -UseBasicParsing

    $line = Get-Content (Join-Path $tmp 'checksums.txt') | Where-Object { $_ -match " $([regex]::Escape($archive))$" }
    $expected = ($line -split '\s+')[0]
    $actual = (Get-FileHash (Join-Path $tmp $archive) -Algorithm SHA256).Hash.ToLower()
    if (-not $expected -or $expected -ne $actual) { throw 'checksum verification failed' }

    Expand-Archive (Join-Path $tmp $archive) -DestinationPath $tmp -Force

    $dir = if ($env:CERTWATCH_INSTALL_DIR) { $env:CERTWATCH_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'certwatch\bin' }
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    Copy-Item (Join-Path $tmp 'certwatch.exe') (Join-Path $dir 'certwatch.exe') -Force

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (($userPath -split ';') -notcontains $dir) {
        [Environment]::SetEnvironmentVariable('Path', "$userPath;$dir", 'User')
        Write-Host "Added $dir to your user PATH (open a new terminal to use it)."
    }
    Write-Host "Installed certwatch to $dir\certwatch.exe"
    & (Join-Path $dir 'certwatch.exe') version
}
finally {
    Remove-Item -Recurse -Force $tmp
}
