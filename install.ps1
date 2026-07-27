<#
.SYNOPSIS
    Installer for ccm, the Claude Code credential manager.

.DESCRIPTION
    irm https://raw.githubusercontent.com/MAbbasRaza/claude-code-credential-manager/main/install.ps1 | iex

    Installs into %LOCALAPPDATA%\Programs\ccm and adds it to your user PATH.
    Never requires administrator rights: everything lives under your profile and
    only the user PATH is modified, never the machine PATH.

.PARAMETER Version
    Release tag to install. Defaults to the latest release.

.PARAMETER InstallDir
    Target directory. Defaults to %LOCALAPPDATA%\Programs\ccm.

.PARAMETER Tray
    Also install the system tray app.

.PARAMETER NoVerify
    Skip SHA256 verification. Not recommended.

.EXAMPLE
    irm https://raw.githubusercontent.com/MAbbasRaza/claude-code-credential-manager/main/install.ps1 | iex

.EXAMPLE
    # With options, download first so parameters can be passed:
    irm https://raw.githubusercontent.com/MAbbasRaza/claude-code-credential-manager/main/install.ps1 -OutFile install.ps1
    .\install.ps1 -Tray
#>
[CmdletBinding()]
param(
    [string]$Version = 'latest',
    [string]$InstallDir = "$env:LOCALAPPDATA\Programs\ccm",
    [switch]$Tray,
    [switch]$NoVerify
)

$ErrorActionPreference = 'Stop'
# Invoke-WebRequest is dramatically slower with the progress bar on 5.1.
$ProgressPreference = 'SilentlyContinue'

$Repo = 'MAbbasRaza/claude-code-credential-manager'

function Write-Info { param([string]$Message) Write-Host $Message }
function Write-Ok   { param([string]$Message) Write-Host $Message -ForegroundColor Green }
function Write-Warn { param([string]$Message) Write-Host $Message -ForegroundColor Yellow }
function Write-Fail {
    param([string]$Message)
    Write-Host "error: $Message" -ForegroundColor Red
    exit 1
}

function Get-Architecture {
    # PROCESSOR_ARCHITECTURE reports the *process* architecture, so a 32-bit
    # PowerShell on 64-bit Windows would report x86. PROCESSOR_ARCHITEW6432 is
    # set only in that case and holds the real machine architecture.
    $arch = $env:PROCESSOR_ARCHITEW6432
    if (-not $arch) { $arch = $env:PROCESSOR_ARCHITECTURE }

    switch ($arch) {
        'AMD64' { return 'amd64' }
        'ARM64' { return 'arm64' }
        'x86'   { Write-Fail "32-bit Windows is not supported" }
        default { Write-Fail "unsupported architecture: $arch" }
    }
}

function Get-ReleaseUrl {
    param([string]$Asset)
    if ($Version -eq 'latest') {
        return "https://github.com/$Repo/releases/latest/download/$Asset"
    }
    return "https://github.com/$Repo/releases/download/$Version/$Asset"
}

function Get-RemoteFile {
    param([string]$Url, [string]$OutFile)
    try {
        Invoke-WebRequest -Uri $Url -OutFile $OutFile -UseBasicParsing
    } catch {
        throw "download failed for $Url : $($_.Exception.Message)"
    }
}

function Install-Asset {
    param(
        [string]$Asset,
        [string]$TargetName,
        [string]$Staging,
        [System.Collections.Generic.Dictionary[string, string]]$Checksums
    )

    Write-Info "downloading $Asset"
    $staged = Join-Path $Staging $Asset
    try {
        Get-RemoteFile -Url (Get-ReleaseUrl $Asset) -OutFile $staged
    } catch {
        Write-Fail "could not download $Asset.`nIf this is the tray app, it may not ship for your platform; see the README.`n`n$($_.Exception.Message)"
    }

    if (-not $NoVerify) {
        # Fail closed: a missing checksum aborts rather than warning, so an
        # attacker who can drop one request cannot silently downgrade this
        # installer to no verification.
        if (-not $Checksums.ContainsKey($Asset)) {
            Write-Fail "no checksum published for $Asset.`nThe release may be incomplete. Re-run with -NoVerify only if you accept the risk."
        }
        $actual = (Get-FileHash $staged -Algorithm SHA256).Hash.ToLower()
        $expected = $Checksums[$Asset]
        if ($actual -ne $expected) {
            Write-Fail "checksum mismatch for $Asset`n  expected $expected`n  actual   $actual`nDo not use this file. Please report it."
        }
    }

    if (-not (Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    }

    $final = Join-Path $InstallDir $TargetName
    # A running ccm-tray holds a lock on its own exe. Say so plainly rather
    # than surfacing a bare access-denied.
    try {
        Move-Item -Path $staged -Destination $final -Force
    } catch {
        Write-Fail "could not write $final. If ccm-tray is running, close it and retry.`n$($_.Exception.Message)"
    }
    Write-Ok "installed $final"
}

function Add-ToUserPath {
    param([string]$Directory)

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (-not $userPath) { $userPath = '' }

    # Compare case-insensitively and ignore a trailing slash, so repeated runs
    # do not append duplicates.
    $normalized = $Directory.TrimEnd('\')
    $already = $false
    foreach ($entry in $userPath.Split(';')) {
        if ($entry -and ($entry.TrimEnd('\') -ieq $normalized)) { $already = $true; break }
    }

    if ($already) {
        return $false
    }

    if ($userPath -eq '') {
        $newPath = $Directory
    } else {
        $newPath = $userPath.TrimEnd(';') + ';' + $Directory
    }
    [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')

    # Make it usable in this session too, so the next lines can run ccm.
    $env:Path = $env:Path + ';' + $Directory
    return $true
}

# ---------------------------------------------------------------------------

# Windows PowerShell 5.1 defaults to TLS 1.0 for outbound HTTPS, which GitHub
# refuses. Without this the download fails with an unhelpful connection error.
try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
} catch {
    # PowerShell 7 manages this itself and the property may be unavailable.
}

$arch = Get-Architecture
Write-Info "Installing ccm (windows/$arch, version $Version)"

$staging = Join-Path ([IO.Path]::GetTempPath()) ("ccm-install-" + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $staging | Out-Null

try {
    # Hashtables are case-insensitive by default, which would let
    # ccm-windows-amd64.exe match a differently-cased entry. Asset names are
    # case-sensitive on GitHub, so use an ordinal-comparing table.
    $checksums = New-Object 'System.Collections.Generic.Dictionary[string,string]' ([StringComparer]::Ordinal)

    if (-not $NoVerify) {
        $sumsFile = Join-Path $staging 'SHA256SUMS'
        try {
            Get-RemoteFile -Url (Get-ReleaseUrl 'SHA256SUMS') -OutFile $sumsFile
        } catch {
            Write-Fail "could not download SHA256SUMS from the $Version release.`nEither that release does not exist, or the network blocked it.`n`nCheck https://github.com/$Repo/releases`nTo install without verification anyway, re-run with -NoVerify (not recommended)."
        }
        foreach ($line in (Get-Content $sumsFile)) {
            # Format: "<hash>  <name>" or "<hash> *<name>" for binary mode.
            if ($line -match '^([0-9a-fA-F]{64})\s+\*?(.+)$') {
                $checksums[$Matches[2].Trim()] = $Matches[1].ToLower()
            }
        }
    }

    Install-Asset -Asset "ccm-windows-$arch.exe" -TargetName 'ccm.exe' -Staging $staging -Checksums $checksums

    if ($Tray) {
        Install-Asset -Asset "ccm-tray-windows-$arch.exe" -TargetName 'ccm-tray.exe' -Staging $staging -Checksums $checksums
    }

    $added = Add-ToUserPath -Directory $InstallDir

    $installedVersion = & (Join-Path $InstallDir 'ccm.exe') --version
    Write-Ok ""
    Write-Ok $installedVersion

    if ($added) {
        Write-Warn ""
        Write-Warn "Added $InstallDir to your user PATH."
        Write-Warn "Open a new terminal for it to take effect in other windows."
    }

    Write-Info ""
    Write-Host "Next steps" -ForegroundColor White
    Write-Info "  ccm init            pin your Claude Code config directory"
    Write-Info "  ccm add work        save the account you are signed into now"
    Write-Info ""
    Write-Info "Capture your current account BEFORE running /logout in Claude Code."
    Write-Info "Logging out destroys the refresh token there would be nothing left to save."
}
catch {
    # Write-Fail already exits with a clean message, so anything reaching here
    # is unexpected. Print it readably rather than dumping a PowerShell stack.
    Write-Host "error: $($_.Exception.Message)" -ForegroundColor Red
    exit 1
}
finally {
    Remove-Item $staging -Recurse -Force -ErrorAction SilentlyContinue
}
