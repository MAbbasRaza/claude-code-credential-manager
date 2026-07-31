<#
.SYNOPSIS
    Builds the Windows installer from packaging/windows/ccm.nsi.

.DESCRIPTION
    The only place makensis is invoked. The release workflow calls this script
    with binaries it downloaded from the release jobs; a developer calls it with
    -Build to compile them first. Because both go through here, the installer CI
    publishes and the one built locally cannot drift.

    Without -Build the script packages binaries that already exist. That is what
    makes it usable on a machine with CGO_ENABLED=0 and no C toolchain, where
    ccm-tray and ccm-gui cannot be compiled at all but a downloaded copy can
    still be packaged and the wizard exercised.

.PARAMETER Version
    Release tag. Defaults to `git describe --tags --always --dirty`.

.PARAMETER SrcDir
    Directory holding ccm.exe, ccm-tray.exe and ccm-gui.exe.

.PARAMETER OutDir
    Where the installer is written.

.PARAMETER OutFile
    Installer filename. Deliberately carries no version: GitHub's
    /releases/latest/download/<name> redirect only resolves a fixed filename.

.PARAMETER Build
    Compile the three executables first. Needs a C toolchain for ccm-tray and
    ccm-gui.

.PARAMETER MakeNsis
    Path to makensis.exe, when it is not on PATH or in a default location.

.EXAMPLE
    # Package binaries downloaded from a release (no compiler needed):
    .\scripts\build-installer.ps1 -Version v0.3.0 -SrcDir .\staging

.EXAMPLE
    # Full local build:
    .\scripts\build-installer.ps1 -Build
#>
[CmdletBinding()]
param(
    [string]$Version  = '',
    [string]$SrcDir   = 'bin',
    [string]$OutDir   = 'dist',
    [string]$OutFile  = 'ccm-setup-windows.exe',
    [switch]$Build,
    [string]$MakeNsis = ''
)

$ErrorActionPreference = 'Stop'

# Every relative path below is repo-root relative, so the script behaves the
# same whether it is invoked from the root or from scripts/.
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $repoRoot

function Fail { param([string]$Message) throw $Message }

# ---------------------------------------------------------------------------
# Version
# ---------------------------------------------------------------------------

if (-not $Version) {
    $Version = (& git describe --tags --always --dirty 2>$null)
    if (-not $Version) { $Version = 'dev' }
}
$Version = $Version.Trim()

# relver is the shared normaliser; the macOS package build uses it too. Run it
# before anything expensive, so an unusable tag fails in a second rather than
# after a full build.
$relver = @{}
foreach ($line in (& go run ./scripts/relver $Version)) {
    if ($line -match '^([a-z]+)=(.*)$') { $relver[$matches[1]] = $matches[2] }
}
if ($LASTEXITCODE -ne 0) { Fail "scripts/relver rejected version '$Version'" }
foreach ($k in 'display', 'short', 'quad') {
    if (-not $relver.ContainsKey($k)) { Fail "scripts/relver did not report '$k'" }
}
Write-Host "version  $($relver.display)  (VIProductVersion $($relver.quad))"

# ---------------------------------------------------------------------------
# Binaries
# ---------------------------------------------------------------------------

if ($Build) {
    New-Item -ItemType Directory -Force -Path $SrcDir | Out-Null
    $ldflags = "-s -w -X main.version=$Version"

    # These flags must match .github/workflows/ci.yml's release jobs. In
    # particular -H=windowsgui, without which the desktop app opens a console
    # window behind itself.
    Write-Host 'building ccm.exe'
    & go build -trimpath -ldflags $ldflags -o (Join-Path $SrcDir 'ccm.exe') ./cmd/ccm
    if ($LASTEXITCODE -ne 0) { Fail 'go build ./cmd/ccm failed' }

    Write-Host 'building ccm-tray.exe'
    & go build -trimpath -ldflags $ldflags -o (Join-Path $SrcDir 'ccm-tray.exe') ./cmd/ccm-tray
    if ($LASTEXITCODE -ne 0) { Fail 'go build ./cmd/ccm-tray failed' }

    Write-Host 'building ccm-gui.exe'
    & go build -trimpath -ldflags "$ldflags -H=windowsgui" -o (Join-Path $SrcDir 'ccm-gui.exe') ./cmd/ccm-gui
    if ($LASTEXITCODE -ne 0) { Fail 'go build ./cmd/ccm-gui failed (it needs cgo and a C toolchain)' }
}

# Checked before makensis rather than after, because NSIS reports a missing
# File source as a compile error mentioning only the path. A renamed CI
# artifact would otherwise produce an installer missing a component, and the
# first sign of it would be a user reporting that the tray never appeared.
foreach ($exe in 'ccm.exe', 'ccm-tray.exe', 'ccm-gui.exe') {
    $p = Join-Path $SrcDir $exe
    if (-not (Test-Path $p)) {
        Fail "missing $p`nRun with -Build, or point -SrcDir at a directory holding the three executables."
    }
    if ((Get-Item $p).Length -eq 0) { Fail "$p is empty" }
}

# ---------------------------------------------------------------------------
# Optional signing
#
# Inert until a certificate exists. Present now so adding one later is a
# secret and a thumbprint, not a change to how the installer is built.
# ---------------------------------------------------------------------------

function Invoke-Sign {
    param([string[]]$Paths)
    if (-not $env:WINDOWS_SIGN_THUMBPRINT) { return }
    $signtool = (Get-Command signtool -ErrorAction SilentlyContinue)
    if (-not $signtool) {
        Write-Warning 'WINDOWS_SIGN_THUMBPRINT is set but signtool is not on PATH; leaving unsigned'
        return
    }
    foreach ($p in $Paths) {
        & $signtool.Source sign /sha1 $env:WINDOWS_SIGN_THUMBPRINT `
            /fd SHA256 /tr http://timestamp.digicert.com /td SHA256 $p
        if ($LASTEXITCODE -ne 0) { Fail "signtool failed on $p" }
    }
}

Invoke-Sign -Paths (@('ccm.exe', 'ccm-tray.exe', 'ccm-gui.exe') | ForEach-Object { Join-Path $SrcDir $_ })

# ---------------------------------------------------------------------------
# makensis
# ---------------------------------------------------------------------------

if (-not $MakeNsis) {
    $cmd = Get-Command makensis -ErrorAction SilentlyContinue
    if ($cmd) {
        $MakeNsis = $cmd.Source
    } else {
        foreach ($candidate in @(
            "${env:ProgramFiles(x86)}\NSIS\makensis.exe",
            "$env:ProgramFiles\NSIS\makensis.exe"
        )) {
            if ($candidate -and (Test-Path $candidate)) { $MakeNsis = $candidate; break }
        }
    }
}
if (-not $MakeNsis -or -not (Test-Path $MakeNsis)) {
    Fail "makensis was not found.`nInstall NSIS (choco install nsis -y) or pass -MakeNsis <path to makensis.exe>."
}

New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
$srcAbs = (Resolve-Path $SrcDir).Path
$outAbs = Join-Path (Resolve-Path $OutDir).Path $OutFile

# Absolute paths, each define passed as a single argument so a path containing
# spaces survives NSIS's /D<name>=<value> parsing.
& $MakeNsis `
    "/DVERSION=$($relver.display)" `
    "/DVERSIONQUAD=$($relver.quad)" `
    "/DSRCDIR=$srcAbs" `
    "/DOUTFILE=$outAbs" `
    (Join-Path 'packaging\windows' 'ccm.nsi')

if ($LASTEXITCODE -ne 0) { Fail "makensis failed with exit code $LASTEXITCODE" }
if (-not (Test-Path $outAbs)) { Fail "makensis reported success but $outAbs does not exist" }

Invoke-Sign -Paths @($outAbs)

$item = Get-Item $outAbs
$hash = (Get-FileHash $outAbs -Algorithm SHA256).Hash.ToLower()
Write-Host ''
Write-Host "built    $outAbs"
Write-Host "size     $([math]::Round($item.Length / 1MB, 2)) MB"
Write-Host "sha256   $hash"
