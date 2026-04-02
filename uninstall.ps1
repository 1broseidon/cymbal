#!/usr/bin/env pwsh
# Uninstall cymbal on Windows.
# Usage: irm https://raw.githubusercontent.com/1broseidon/cymbal/main/uninstall.ps1 | iex
#
# By default removes the binary and PATH entry but keeps index data.
# Pass -Purge to also remove all SQLite indexes (~/.cymbal/repos/).

param(
    [switch]$Purge
)

$ErrorActionPreference = "Stop"

$installDir = "$env:LOCALAPPDATA\cymbal"

# Remove from user PATH
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -like "*$installDir*") {
    $newPath = ($userPath -split ";" | Where-Object { $_ -ne $installDir }) -join ";"
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
    Write-Host "Removed $installDir from user PATH." -ForegroundColor Yellow
}

# Remove binary
$bin = Join-Path $installDir "cymbal.exe"
if (Test-Path $bin) {
    Remove-Item -Force $bin
    Write-Host "Removed cymbal.exe." -ForegroundColor Yellow
} else {
    Write-Host "cymbal.exe not found — may already be uninstalled." -ForegroundColor Gray
}

# Remove index data if -Purge is set
if ($Purge) {
    $reposDir = Join-Path $installDir "repos"
    if (Test-Path $reposDir) {
        Remove-Item -Recurse -Force $reposDir
        Write-Host "Removed index data at $reposDir." -ForegroundColor Yellow
    }

    # Remove install dir if now empty
    if (Test-Path $installDir) {
        $remaining = Get-ChildItem $installDir -ErrorAction SilentlyContinue
        if (-not $remaining) {
            Remove-Item -Force $installDir
        }
    }
} else {
    Write-Host "Index data kept at $installDir\repos (run with -Purge to remove)." -ForegroundColor Gray
}

Write-Host "cymbal uninstalled." -ForegroundColor Green
