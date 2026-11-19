$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")

go build -tags desktop,production -ldflags "-H windowsgui -s -w" -o ProLite.exe .
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Host "built: $($PWD.Path)\ProLite.exe"
