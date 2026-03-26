#!/usr/bin/env pwsh
# Install cymbal on Windows.
# Usage: irm https://raw.githubusercontent.com/1broseidon/cymbal/main/install.ps1 | iex

$ErrorActionPreference = "Stop"

$repo = "1broseidon/cymbal"
$installDir = "$env:LOCALAPPDATA\cymbal"

# Get latest release tag
$release = Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/latest"
$tag = $release.tag_name
$version = $tag -replace '^v', ''

Write-Host "Installing cymbal $version ..." -ForegroundColor Cyan

# Download and extract
$asset = "cymbal_${tag}_windows_x86_64.zip"
$url = "https://github.com/$repo/releases/download/$tag/$asset"
$tmp = Join-Path $env:TEMP $asset

Invoke-WebRequest -Uri $url -OutFile $tmp

if (Test-Path $installDir) { Remove-Item -Recurse -Force $installDir }
New-Item -ItemType Directory -Path $installDir -Force | Out-Null
Expand-Archive -Path $tmp -DestinationPath $installDir -Force
Remove-Item $tmp

# Verify binary exists
$bin = Join-Path $installDir "cymbal.exe"
if (-not (Test-Path $bin)) {
    Write-Error "Failed to install: cymbal.exe not found in archive"
    exit 1
}

# Add to user PATH if not already present
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$installDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$installDir;$userPath", "User")
    Write-Host "Added $installDir to user PATH." -ForegroundColor Yellow
    Write-Host "Restart your terminal for PATH changes to take effect." -ForegroundColor Yellow
}

Write-Host "cymbal $version installed to $installDir" -ForegroundColor Green
