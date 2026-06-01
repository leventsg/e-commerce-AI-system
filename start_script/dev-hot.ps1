$ErrorActionPreference = "Stop"

if (-not (Get-Command air -ErrorAction SilentlyContinue)) {
    Write-Host "air is not installed. Run: go install github.com/air-verse/air@latest"
    exit 1
}

$root = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $root

Start-Process powershell -ArgumentList "-NoExit", "-Command", "cd '$root'; air -c .air.services.toml"
Start-Process powershell -ArgumentList "-NoExit", "-Command", "cd '$root'; air -c .air.apis.toml"
