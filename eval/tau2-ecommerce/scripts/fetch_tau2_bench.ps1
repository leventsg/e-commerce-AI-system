param(
  [string]$RepoUrl = "https://github.com/sierra-research/tau2-bench.git",
  [string]$Target = "tau2-bench",
  [string]$ZipUrl = "https://codeload.github.com/sierra-research/tau2-bench/zip/refs/heads/main"
)

$ErrorActionPreference = "Stop"

if (Test-Path -LiteralPath $Target) {
  Write-Host "$Target already exists; leaving it untouched."
  exit 0
}

try {
  git clone --depth 1 $RepoUrl $Target
  exit 0
} catch {
  Write-Warning "git clone failed: $($_.Exception.Message)"
}

$tmpZip = Join-Path "tmp" "tau2-bench.zip"
$tmpExtract = Join-Path "tmp" "tau2-bench-src"
New-Item -ItemType Directory -Force -Path "tmp" | Out-Null
if (Test-Path -LiteralPath $tmpZip) {
  Remove-Item -LiteralPath $tmpZip -Force
}
if (Test-Path -LiteralPath $tmpExtract) {
  Remove-Item -LiteralPath $tmpExtract -Recurse -Force
}

Write-Host "Falling back to source zip download..."
Invoke-WebRequest -Uri $ZipUrl -OutFile $tmpZip
Expand-Archive -LiteralPath $tmpZip -DestinationPath $tmpExtract -Force
$expanded = Get-ChildItem -LiteralPath $tmpExtract -Directory | Select-Object -First 1
if ($null -eq $expanded) {
  throw "source zip did not contain an extracted directory"
}
Move-Item -LiteralPath $expanded.FullName -Destination $Target
Remove-Item -LiteralPath $tmpZip -Force
Remove-Item -LiteralPath $tmpExtract -Recurse -Force
