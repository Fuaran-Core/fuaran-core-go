#Requires -Version 7.0
# fuaran-core-go: the one-command entry point.
# gofmt check -> go vet -> go build -> go test.
# Switches: -SkipFormat / -SkipBuild / -SkipTests for fast iteration.
[CmdletBinding()]
param(
    [switch]$SkipFormat,
    [switch]$SkipBuild,
    [switch]$SkipTests
)

$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

function Resolve-Tool {
    param([string]$Name)
    $cmd = Get-Command $Name -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($cmd) { return $cmd.Source }
    $root = if ($env:GOROOT) { $env:GOROOT } elseif ($IsWindows) { Join-Path $env:ProgramFiles "Go" } else { "/usr/local/go" }
    $candidate = Join-Path (Join-Path $root "bin") ($Name + $(if ($IsWindows) { ".exe" } else { "" }))
    if (Test-Path $candidate) { return $candidate }
    throw "The Go toolchain is required but '$Name' was not found on PATH or under GOROOT."
}

function Invoke-Stage {
    param([string]$Name, [scriptblock]$Body)
    Write-Host "==> $Name"
    $global:LASTEXITCODE = 0
    & $Body
    if ($LASTEXITCODE -ne 0) {
        Write-Host "FAILED: $Name (exit $LASTEXITCODE)"
        exit $LASTEXITCODE
    }
}

$go = Resolve-Tool "go"
$gofmt = Resolve-Tool "gofmt"

if (-not $SkipFormat) {
    Invoke-Stage "gofmt -l" {
        $unformatted = & $gofmt -l .
        if ($LASTEXITCODE -eq 0 -and $unformatted) {
            Write-Host "not gofmt-formatted:"
            $unformatted | ForEach-Object { Write-Host "  $_" }
            $global:LASTEXITCODE = 1
        }
    }
}
if (-not $SkipBuild) {
    Invoke-Stage "go vet" { & $go vet ./... }
    Invoke-Stage "go build" { & $go build ./... }
}
if (-not $SkipTests) { Invoke-Stage "go test" { & $go test -count=1 ./... } }

Write-Host "fuaran-core-go: green"
exit 0
