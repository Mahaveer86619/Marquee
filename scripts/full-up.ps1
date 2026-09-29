<#
.SYNOPSIS
    Builds the Marquee executables and container images, then starts and verifies the stack.

.DESCRIPTION
    Docker is the only prerequisite. If Go is installed, the executables are
    built with it; otherwise they are built inside a Go container. The script
    then runs "marquee setup", which starts Docker Desktop if needed, builds the
    images, starts the services and runs the health checks.

.PARAMETER MediaDir
    Host folder for the media library, for example D:\Media. Saved for later runs.

.PARAMETER WithIndexers
    Also run Prowlarr, so you can add your own indexers. Saved for later runs.

.PARAMETER NoCache
    Rebuild the container images without the Docker build cache.

.PARAMETER BuildInDocker
    Build the executables inside a Go container even if Go is installed.

.EXAMPLE
    powershell -ExecutionPolicy Bypass -File scripts\full-up.ps1

.EXAMPLE
    powershell -ExecutionPolicy Bypass -File scripts\full-up.ps1 -MediaDir D:\Media -WithIndexers
#>
[CmdletBinding()]
param(
    [string]$MediaDir,
    [switch]$WithIndexers,
    [switch]$NoCache,
    [switch]$BuildInDocker
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

Write-Host 'Checking prerequisites'
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    Write-Host 'Missing prerequisite: Docker. Install Docker Desktop with: winget install Docker.DockerDesktop' -ForegroundColor Red
    exit 1
}
$useDockerGo = $BuildInDocker -or -not (Get-Command go -ErrorAction SilentlyContinue)

$version = if ($env:MARQUEE_VERSION) { $env:MARQUEE_VERSION } else { '0.0.0-dev' }
$commit = 'unknown'
if (Get-Command git -ErrorAction SilentlyContinue) {
    $ErrorActionPreference = 'Continue'
    $rev = git rev-parse --short HEAD 2>$null
    if ($LASTEXITCODE -eq 0 -and $rev) { $commit = $rev.Trim() }
    $ErrorActionPreference = 'Stop'
}
$ldflags = "-s -w -X marquee/internal/version.Version=$version -X marquee/internal/version.Commit=$commit"
$goarch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }

if ($useDockerGo) {
    Write-Host "Building executables in a Go container ($version, $commit)"
    $ErrorActionPreference = 'Continue'
    docker info *> $null
    $dockerUp = ($LASTEXITCODE -eq 0)
    $ErrorActionPreference = 'Stop'
    if (-not $dockerUp) {
        Write-Host 'Docker is not running. Start Docker Desktop and run this script again.' -ForegroundColor Red
        exit 1
    }
} else {
    Write-Host "Building executables ($version, $commit)"
}

# Build to a temporary name, then swap it in. Windows cannot overwrite a
# running program but can rename it, so an open TUI keeps working and the next
# launch uses the new build.
Get-ChildItem bin -Filter '*.old' -ErrorAction SilentlyContinue | Remove-Item -ErrorAction SilentlyContinue
foreach ($target in @(@('marquee', './cmd/marquee'), @('marquee-agent', './cmd/agent'))) {
    $final = "bin/$($target[0]).exe"
    $out = "bin/$($target[0]).new.exe"
    if ($useDockerGo) {
        docker run --rm -v "${root}:/src" -w /src `
            -e CGO_ENABLED=0 -e GOOS=windows -e GOARCH=$goarch `
            -e GOCACHE=/src/.cache/go-build -e GOMODCACHE=/src/.cache/gomod `
            golang:1.27 go build -trimpath -ldflags $ldflags -o $out $target[1]
    } else {
        go build -trimpath -ldflags $ldflags -o $out $target[1]
    }
    if ($LASTEXITCODE -ne 0) { Write-Host "Build failed: $($target[0])" -ForegroundColor Red; exit $LASTEXITCODE }
    if (Test-Path $final) {
        $old = "$final.$([DateTime]::Now.ToString('yyyyMMddHHmmss')).old"
        Move-Item $final $old -Force   # works even while the program is running
    }
    Move-Item $out $final -Force
}

$setupArgs = @('setup')
if ($MediaDir)     { $setupArgs += @('-media', $MediaDir) }
if ($WithIndexers) { $setupArgs += '-with-indexers' }
if ($NoCache)      { $setupArgs += '-no-cache' }

& "$root\bin\marquee.exe" @setupArgs
exit $LASTEXITCODE
