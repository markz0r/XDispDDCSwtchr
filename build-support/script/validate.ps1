param(
    [ValidateSet('quick', 'full', 'platform', 'hardware-read', 'hardware-write')]
    [string]$Mode = 'quick',
    [string]$MonitorId,
    [string]$ExpectedModel,
    [string]$ExpectedEdidSha256,
    [string]$SourceInput,
    [string]$TargetInput,
    [switch]$AcknowledgeSwitchAway,
    [string]$RecoveryMethod
)

$ErrorActionPreference = 'Stop'
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
Set-Location $RepoRoot
$Stamp = (Get-Date).ToUniversalTime().ToString('yyyyMMddTHHmmssZ')
$ArtifactDir = Join-Path $RepoRoot "test-artifacts/$Stamp-$Mode"
New-Item -ItemType Directory -Force -Path $ArtifactDir | Out-Null
if (-not $env:GOTOOLCHAIN) { $env:GOTOOLCHAIN = 'go1.26.5' }
if (-not $env:GOPATH) { $env:GOPATH = Join-Path $RepoRoot 'test-artifacts/.gopath' }
if (-not $env:GOCACHE) { $env:GOCACHE = Join-Path $RepoRoot 'test-artifacts/.gocache' }
$env:GOFLAGS = '-mod=readonly'
$Status = 'failed'
$ExitCode = 1

function Invoke-Checked([scriptblock]$Command) {
    & $Command
    if ($LASTEXITCODE -ne 0) { throw "command failed with exit code $LASTEXITCODE" }
}

function Invoke-Quick {
    go version | Set-Content (Join-Path $ArtifactDir 'go-version.txt')
    $GoFiles = @(git ls-files --cached --others --exclude-standard -- '*.go' | Where-Object { Test-Path -LiteralPath $_ -PathType Leaf })
    $FormatDiff = Join-Path $ArtifactDir 'gofmt.diff'
    if ($GoFiles.Count -gt 0) { gofmt -d $GoFiles | Set-Content $FormatDiff }
    if ((Test-Path $FormatDiff) -and (Get-Item $FormatDiff).Length -gt 0) { throw 'gofmt diff is non-empty' }
    $oldFlags = $env:GOFLAGS
    $env:GOFLAGS = ''
    Invoke-Checked { go mod tidy -diff }
    $env:GOFLAGS = $oldFlags
    Invoke-Checked { go mod verify }
    Invoke-Checked { go vet ./... }
    Invoke-Checked { go test -short -shuffle=on -count=1 ./... }
    Invoke-Checked { go test -short -shuffle=on -count=1 -tags qualification ./cmd/xdispddcswtchr-qualify }
}

function Invoke-Full {
    Invoke-Quick
    Invoke-Checked { go test -shuffle=on -count=1 ./... }
    Invoke-Checked { go test -race -shuffle=on -count=1 ./... }
    Invoke-Checked { go test -count=10 ./internal/service ./internal/tui }
    $Coverage = Join-Path $ArtifactDir 'coverage.out'
    Invoke-Checked { go test -covermode=atomic "-coverprofile=$Coverage" ./... }
    go tool cover "-func=$Coverage" | Set-Content (Join-Path $ArtifactDir 'coverage.txt')
    foreach ($Package in @('capabilities', 'ddc', 'edid', 'profiles', 'service')) {
        $PackageCoverage = Join-Path $ArtifactDir "coverage-$Package.out"
        Invoke-Checked { go test -covermode=atomic "-coverprofile=$PackageCoverage" "./internal/$Package" }
        $CoverageLines = @(go tool cover "-func=$PackageCoverage")
        $CoverageLines | Set-Content (Join-Path $ArtifactDir "coverage-$Package.txt")
        if ($CoverageLines[-1] -notmatch '([0-9]+(?:\.[0-9]+)?)%') { throw "could not parse $Package coverage" }
        if ([double]$Matches[1] -lt 90) { throw "$Package coverage $($Matches[1])% is below 90%" }
    }
    $CommonPackages = @('./internal/capabilities', './internal/cli', './internal/config', './internal/ddc', './internal/diagnostics', './internal/edid', './internal/monitor', './internal/platform', './internal/profiles', './internal/service', './internal/tui', './internal/verification')
    $CommonPattern = $CommonPackages -join ','
    $CommonCoverage = Join-Path $ArtifactDir 'coverage-common.out'
    Invoke-Checked { go test -covermode=atomic "-coverpkg=$CommonPattern" "-coverprofile=$CommonCoverage" $CommonPackages }
    $CommonCoverageLines = @(go tool cover "-func=$CommonCoverage")
    $CommonCoverageLines | Set-Content (Join-Path $ArtifactDir 'coverage-common.txt')
    if ($CommonCoverageLines[-1] -notmatch '([0-9]+(?:\.[0-9]+)?)%') { throw 'could not parse common package coverage' }
    if ([double]$Matches[1] -lt 80) { throw "common package coverage $($Matches[1])% is below 80%" }
    go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./... | Set-Content (Join-Path $ArtifactDir 'govulncheck.log')
    if ($LASTEXITCODE -ne 0) { throw "govulncheck failed with exit code $LASTEXITCODE" }
}

function Build-Binaries {
    $Commit = (git rev-parse --verify HEAD).Trim()
    $Binary = Join-Path $ArtifactDir 'xdispddcswtchr.exe'
    $QualificationBinary = Join-Path $ArtifactDir 'xdispddcswtchr-qualify.exe'
    go version | Set-Content (Join-Path $ArtifactDir 'go-version.txt')
    Invoke-Checked { go build -trimpath -ldflags "-s -w -X main.version=0.1.0-dev -X main.commit=$Commit" -o $Binary ./cmd/xdispddcswtchr }
    Invoke-Checked { go build -trimpath -tags qualification -o $QualificationBinary ./cmd/xdispddcswtchr-qualify }
    go version -m $Binary | Set-Content (Join-Path $ArtifactDir 'build-metadata.txt')
}

function Require-HardwareIdentity {
    if (-not $MonitorId -or -not $ExpectedModel -or -not $ExpectedEdidSha256) {
        throw 'hardware mode requires MonitorId, ExpectedModel, and ExpectedEdidSha256'
    }
}

try {
    $ActualGo = (go env GOVERSION).Trim()
    if ($LASTEXITCODE -ne 0 -or $ActualGo -ne 'go1.26.5') { throw "Go toolchain $ActualGo is active; go1.26.5 is required" }
    switch ($Mode) {
        'quick' { Invoke-Quick }
        'full' { Invoke-Full }
        'platform' {
            Invoke-Full
            Build-Binaries
            $Binary = Join-Path $ArtifactDir 'xdispddcswtchr.exe'
            $SavedGOOS = $env:GOOS
            $SavedGOARCH = $env:GOARCH
            $SavedCGO = $env:CGO_ENABLED
            try {
                $env:CGO_ENABLED = '0'
                $env:GOOS = 'darwin'
                $env:GOARCH = 'arm64'
                $DarwinCrossBinary = Join-Path $ArtifactDir 'xdispddcswtchr-darwin-arm64-nocgo'
                Invoke-Checked { go build -trimpath -o $DarwinCrossBinary ./cmd/xdispddcswtchr }
                $env:GOOS = 'linux'
                $env:GOARCH = 'amd64'
                $LinuxCrossBinary = Join-Path $ArtifactDir 'xdispddcswtchr-linux-amd64'
                Invoke-Checked { go build -trimpath -o $LinuxCrossBinary ./cmd/xdispddcswtchr }
            }
            finally {
                $env:GOOS = $SavedGOOS
                $env:GOARCH = $SavedGOARCH
                $env:CGO_ENABLED = $SavedCGO
            }
            & $Binary version | Set-Content (Join-Path $ArtifactDir 'version.txt')
            if ($LASTEXITCODE -ne 0) { throw 'version command failed' }
            & $Binary help 1> (Join-Path $ArtifactDir 'help.stdout') 2> (Join-Path $ArtifactDir 'help.stderr')
            if ($LASTEXITCODE -ne 0) { throw 'help command failed' }
            '' | & $Binary 1> (Join-Path $ArtifactDir 'non-tty.stdout') 2> (Join-Path $ArtifactDir 'non-tty.stderr')
            if ($LASTEXITCODE -ne 2) { throw "non-TTY invocation returned $LASTEXITCODE, expected 2" }
            $LegacyConfig = Join-Path $ArtifactDir 'legacy-config.json'
            '{"backend":"cli","hotkeys":[]}' | Set-Content $LegacyConfig
            & $Binary --config $LegacyConfig list 1> (Join-Path $ArtifactDir 'legacy.stdout') 2> (Join-Path $ArtifactDir 'legacy.stderr')
            if ($LASTEXITCODE -ne 2) { throw "legacy configuration returned $LASTEXITCODE, expected 2" }
            $BinaryBytes = [IO.File]::ReadAllBytes($Binary)
            $BinaryText = [Text.Encoding]::ASCII.GetString($BinaryBytes)
            if ($BinaryText -match '(?i)controlmymonitor|ddcutil|ddcctl') { throw 'forbidden external monitor-control dependency marker found' }
        }
        'hardware-read' {
            Require-HardwareIdentity
            Build-Binaries
            $Binary = Join-Path $ArtifactDir 'xdispddcswtchr.exe'
            $DiagnosticPath = Join-Path $ArtifactDir 'read-diagnostic.json'
            & $Binary diagnose --monitor $MonitorId --output $DiagnosticPath | Set-Content (Join-Path $ArtifactDir 'diagnose.stdout')
            if ($LASTEXITCODE -ne 0) { throw 'read diagnostic command failed' }
            $CapabilityPath = Join-Path $ArtifactDir 'input-capabilities.json'
            & $Binary input detect --monitor $MonitorId --json | Set-Content $CapabilityPath
            if ($LASTEXITCODE -ne 0) { throw 'input capability detection failed' }
            $Diagnostic = Get-Content $DiagnosticPath -Raw | ConvertFrom-Json
            if ($Diagnostic.monitor.id -ne $MonitorId -or $Diagnostic.monitor.model_name -ne $ExpectedModel -or $Diagnostic.edid.sha256 -ne $ExpectedEdidSha256) {
                throw 'exact monitor identity not found'
            }
            if (-not $Diagnostic.redaction.enabled) { throw 'hardware-read diagnostic is not redacted' }
            $Capabilities = Get-Content $CapabilityPath -Raw | ConvertFrom-Json
            if (-not $Capabilities.capabilities.input_source_advertised) { throw 'VCP 0x60 is not advertised by the monitor capability string' }
        }
        'hardware-write' {
            Require-HardwareIdentity
            if (-not $SourceInput -or -not $TargetInput -or -not $AcknowledgeSwitchAway -or -not $RecoveryMethod) { throw 'hardware-write safety arguments are incomplete' }
            Build-Binaries
            & (Join-Path $ArtifactDir 'xdispddcswtchr-qualify.exe') input write --monitor $MonitorId --expected-model $ExpectedModel --expected-edid-sha256 $ExpectedEdidSha256 --source-input $SourceInput --target-input $TargetInput --unsafe-allow-input-write --acknowledge-switch-away --recovery-method $RecoveryMethod | Set-Content (Join-Path $ArtifactDir 'input-write.json')
        }
    }
    $Status = 'passed'
    $ExitCode = 0
}
finally {
    [ordered]@{schema_version=1; mode=$Mode; status=$Status; exit_code=$ExitCode; artifact_directory=$ArtifactDir} |
        ConvertTo-Json | Set-Content (Join-Path $ArtifactDir 'summary.json')
    Write-Host "artifacts: $ArtifactDir"
}

exit $ExitCode
