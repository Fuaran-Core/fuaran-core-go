#Requires -Version 7.0
# fuaran-core-go: the one-command entry point.
# gofmt check -> go vet -> go build -> go test -> a bounded run of the oracle fuzz targets.
#
# `go test ./...` includes the conformance legs (conformance/), which read the reference's vectors
# from a Core checkout: FUARAN_CORE_DIR, or `fuaran-core/` beside this repository, at the commit
# core-pin.json names. They FAIL when no checkout is found rather than skip, because a skipped leg
# certifies nothing. The oracle differential in the same package drives the reference's oracle
# process, built here from that checkout (tools/Fuaran.Core.Oracle.Cli) unless FUARAN_CORE_ORACLE
# names a command line that serves the protocol; it too fails rather than skips.
#
# Switches: -SkipFormat / -SkipBuild / -SkipTests / -SkipFuzz for fast iteration; -FuzzSeconds sets
# the bounded fuzz run per target (default 20).
[CmdletBinding()]
param(
    [switch]$SkipFormat,
    [switch]$SkipBuild,
    [switch]$SkipTests,
    [switch]$SkipFuzz,
    [int]$FuzzSeconds = 20
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

# The Core checkout the legs read: FUARAN_CORE_DIR, or fuaran-core/ beside this repository or an
# ancestor of it. Resolved here so the oracle can be built from it; the legs resolve it the same way.
function Find-Core {
    if ($env:FUARAN_CORE_DIR) { return $env:FUARAN_CORE_DIR }
    $dir = Split-Path -Parent $PSScriptRoot
    while ($dir) {
        $candidate = Join-Path $dir "fuaran-core"
        if (Test-Path (Join-Path $candidate "conformance/apply/manifest.json")) { return $candidate }
        $parent = Split-Path -Parent $dir
        if ($parent -eq $dir) { break }
        $dir = $parent
    }
    return $null
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
if (-not $SkipTests -or -not $SkipFuzz) {
    $core = Find-Core
    if (-not $core) {
        Write-Host "FAILED: no Core checkout: clone https://github.com/Fuaran-Core/fuaran-core beside this repository (as fuaran-core/) at the commit core-pin.json names, or set FUARAN_CORE_DIR"
        exit 1
    }
    if (-not $env:FUARAN_CORE_ORACLE) {
        # The oracle process the differential drives, built from the checkout the vectors are read from,
        # so both halves of the certification name one Core commit.
        Invoke-Stage "dotnet build (the reference's oracle, from $core)" {
            $dotnet = Get-Command dotnet -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
            if (-not $dotnet) {
                Write-Host "the .NET SDK is required to build the reference's oracle; or set FUARAN_CORE_ORACLE to a command line that serves the protocol"
                $global:LASTEXITCODE = 1
                return
            }
            & $dotnet.Source build (Join-Path $core "tools/Fuaran.Core.Oracle.Cli/Fuaran.Core.Oracle.Cli.fsproj") -c Release --nologo -v q
        }
    }
}
if (-not $SkipTests) { Invoke-Stage "go test" { & $go test -count=1 ./... } }
if (-not $SkipFuzz) {
    # One bounded coverage-guided run per oracle fuzz target. `go test -fuzz` takes one target per
    # invocation; the seeds are the deterministic stream and the family's vectors (conformance/oracle_test.go).
    foreach ($target in @("FuzzOracleJSONParse", "FuzzOracleTreeApply", "FuzzOracleChainVerify")) {
        Invoke-Stage "go test -fuzz=$target -fuzztime=${FuzzSeconds}s" {
            & $go test -run '^$' -fuzz "^$target`$" -fuzztime "${FuzzSeconds}s" ./conformance
        }
    }
}

Write-Host "fuaran-core-go: green"
exit 0
