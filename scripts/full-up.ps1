<#
.SYNOPSIS
    Builds the Marquee executables and container images, then starts and verifies the stack.

.DESCRIPTION
    Use this after cloning the repository, or at any time during development to
    rebuild everything. It checks prerequisites, builds bin\marquee.exe and
    bin\marquee-agent.exe, then runs "marquee setup", which starts Docker Desktop
    if needed, builds the images, starts the services and runs the health checks.

.PARAMETER MediaDir
    Host folder for the media library, for example D:\Media. Saved for later runs.

.PARAMETER NoCache
    Rebuild the container images without the Docker build cache.

.EXAMPLE
    powershell -ExecutionPolicy Bypass -File scripts\full-up.ps1

.EXAMPLE
    powershell -ExecutionPolicy Bypass -File scripts\full-up.ps1 -MediaDir D:\Media
#>
[CmdletBinding()]
param(
    [string]$MediaDir,
    [switch]$NoCache
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

function Test-Tool([string]$Name, [string]$Hint) {
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        Write-Host "Missing prerequisite: $Name. $Hint" -ForegroundColor Red
        exit 1
    }
}

Write-Host 'Checking prerequisites'
Test-Tool 'go'     'Install it with: winget install GoLang.Go'
Test-Tool 'docker' 'Install Docker Desktop with: winget install Docker.DockerDesktop'

$version = if ($env:MARQUEE_VERSION) { $env:MARQUEE_VERSION } else { '0.0.0-dev' }
$commit = 'unknown'
if (Get-Command git -ErrorAction SilentlyContinue) {
    $ErrorActionPreference = 'Continue'
    $rev = git rev-parse --short HEAD 2>$null
    if ($LASTEXITCODE -eq 0 -and $rev) { $commit = $rev.Trim() }
    $ErrorActionPreference = 'Stop'
}
$ldflags = "-s -w -X marquee/internal/version.Version=$version -X marquee/internal/version.Commit=$commit"

Write-Host "Building executables ($version, $commit)"
foreach ($target in @(@('marquee', './cmd/marquee'), @('marquee-agent', './cmd/agent'))) {
    go build -trimpath -ldflags $ldflags -o "bin/$($target[0]).exe" $target[1]
    if ($LASTEXITCODE -ne 0) { Write-Host "Build failed: $($target[0])" -ForegroundColor Red; exit $LASTEXITCODE }
}

$setupArgs = @('setup')
if ($MediaDir) { $setupArgs += @('-media', $MediaDir) }
if ($NoCache)  { $setupArgs += '-no-cache' }

& "$root\bin\marquee.exe" @setupArgs
exit $LASTEXITCODE
