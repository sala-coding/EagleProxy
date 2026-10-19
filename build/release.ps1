# Release build: stamps the two build-time values the program carries — its own
# version number (the About card shows it) and the repository the update check
# reads — then runs the packaged build.
#
# Usage:
#   .\build\release.ps1 -Version v1.0.0
param(
    [Parameter(Mandatory = $true)][string]$Version,
    [string]$Repo = "sala-coding/EagleProxy"
)

$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")

wails build -ldflags "-X main.appVersion=$Version -X main.updateRepo=$Repo"
if ($LASTEXITCODE -ne 0) {
    Write-Error "wails build failed (exit $LASTEXITCODE)"
}
Write-Output ""
Write-Output "Done: build\bin\Eagle.exe  ($Version, repo $Repo)"
