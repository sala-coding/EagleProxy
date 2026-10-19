# Release build: stamps the build-time values the program carries — its own
# version number (the About card shows it), the build date, and the repository
# the update check reads — then runs the packaged build.
#
# Usage:
#   .\build\release.ps1 -Version v1.0.0
#   .\build\release.ps1                      # a local build: the commit it was made from
#
# Without -Version the build is stamped with the revision it was made from
# ("0.0.0-dev+9988017"). The number is written so it can be COMPARED, not just
# shown: the update check reads it against the release tags, and a version that
# sorts below every release ("0.0.0-dev") is exactly what a working copy is.
# A build that carries no version at all leaves the About card saying
# "unknown" and gives the update check nothing to compare, so a stamped local
# build is the better shape.
param(
    [string]$Version = "",
    [string]$Repo = "sala-coding/EagleProxy"
)

$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")

if (-not $Version) {
    $sha = (& git rev-parse --short HEAD 2>$null)
    if ($LASTEXITCODE -eq 0 -and $sha) {
        $Version = "0.0.0-dev+$sha"
    } else {
        $Version = "0.0.0-dev"
    }
}

# No spaces: the -ldflags list is split on them, so a date written the readable
# way ("2026-10-20 03:08:47") would be read as two arguments and the build
# would fail on the second one.
$Built = (Get-Date).ToString("yyyy-MM-ddTHH:mm:sszzz")

wails build -ldflags "-X main.appVersion=$Version -X main.appBuilt=$Built -X main.updateRepo=$Repo"
if ($LASTEXITCODE -ne 0) {
    Write-Error "wails build failed (exit $LASTEXITCODE)"
}
Write-Output ""
Write-Output "Done: build\bin\Eagle.exe  ($Version, built $Built, repo $Repo)"
