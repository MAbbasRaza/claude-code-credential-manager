<#
.SYNOPSIS
    Installer for ccm, the Claude Code credential manager.

.DESCRIPTION
    irm https://raw.githubusercontent.com/MAbbasRaza/claude-code-credential-manager/main/install.ps1 | iex

    Installs into %LOCALAPPDATA%\Programs\ccm and adds it to your user PATH.
    Never requires administrator rights: everything lives under your profile and
    only the user PATH is modified, never the machine PATH.

    Structural note: this script is designed to be run through `iex`, which
    executes in the CALLER'S scope. That has two consequences the code works
    around deliberately:

      * `exit` would terminate the user's whole PowerShell session, discarding
        the error message it had just printed. Failures therefore throw, are
        caught at the boundary, and only convert to a real `exit` when the
        script is being run as a file.
      * Assigning $ErrorActionPreference or $ProgressPreference at top level
        would permanently change the user's session. They are saved and
        restored instead.

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

$Repo = 'MAbbasRaza/claude-code-credential-manager'

function Write-Info { param([string]$Message) Write-Host $Message }
function Write-Ok   { param([string]$Message) Write-Host $Message -ForegroundColor Green }
function Write-Warn { param([string]$Message) Write-Host $Message -ForegroundColor Yellow }

# Throws rather than exits. See the structural note above.
function Write-Fail { param([string]$Message) throw $Message }

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

function Test-VersionTag {
    param([string]$Tag)
    # The tag is interpolated into a download URL. Reject anything that could
    # steer that URL somewhere else, such as a value containing a slash or a
    # traversal sequence.
    if ($Tag -eq 'latest') { return }
    if ($Tag -notmatch '^v?[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.\-]+)?$') {
        Write-Fail "invalid version '$Tag'. Expected a tag such as v1.2.3, or 'latest'."
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
    Invoke-WebRequest -Uri $Url -OutFile $OutFile -UseBasicParsing
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

    # Written through the registry rather than
    # [Environment]::SetEnvironmentVariable, which always writes REG_SZ. The
    # user PATH is REG_EXPAND_SZ on a default Windows install, and downgrading
    # it stops the system expanding any %VAR% entries it contains, silently
    # breaking unrelated tools on the user's PATH. Reading with
    # DoNotExpandEnvironmentNames keeps those entries symbolic.
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
    if (-not $key) {
        Write-Fail "could not open HKCU\Environment to update PATH"
    }

    try {
        $raw = $key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
        if ($null -eq $raw) { $raw = '' }

        $kind = [Microsoft.Win32.RegistryValueKind]::ExpandString
        try {
            $existingKind = $key.GetValueKind('Path')
            if ($existingKind -eq [Microsoft.Win32.RegistryValueKind]::String -or
                $existingKind -eq [Microsoft.Win32.RegistryValueKind]::ExpandString) {
                $kind = $existingKind
            }
        } catch {
            # No existing Path value; ExpandString is the sensible default.
        }

        # Compare case-insensitively and ignore a trailing slash, so repeated
        # runs do not append duplicates.
        $normalized = $Directory.TrimEnd('\')
        foreach ($entry in $raw.Split(';')) {
            if ($entry -and ($entry.TrimEnd('\') -ieq $normalized)) {
                return $false
            }
        }

        if ($raw -eq '') { $newPath = $Directory }
        else { $newPath = $raw.TrimEnd(';') + ';' + $Directory }

        $key.SetValue('Path', $newPath, $kind)
    } finally {
        $key.Close()
    }

    # Usable in this session too, so the lines below can run ccm.
    $env:Path = $env:Path + ';' + $Directory

    # Tell Explorer the environment changed, so newly launched terminals see it
    # without a sign-out. Best effort; failure here is cosmetic.
    try {
        if (-not ('CcmNative' -as [type])) {
            Add-Type -Namespace '' -Name 'CcmNative' -MemberDefinition @'
[DllImport("user32.dll", SetLastError = true, CharSet = CharSet.Auto)]
public static extern IntPtr SendMessageTimeout(IntPtr hWnd, uint Msg, UIntPtr wParam,
    string lParam, uint fuFlags, uint uTimeout, out UIntPtr lpdwResult);
'@ -ErrorAction Stop
        }
        $HWND_BROADCAST = [IntPtr]0xffff
        $WM_SETTINGCHANGE = 0x1A
        $result = [UIntPtr]::Zero
        [void][CcmNative]::SendMessageTimeout($HWND_BROADCAST, $WM_SETTINGCHANGE,
            [UIntPtr]::Zero, 'Environment', 2, 5000, [ref]$result)
    } catch {
        # Non-fatal: a new sign-in picks the change up regardless.
    }

    return $true
}

function Invoke-CcmInstall {
    Test-VersionTag -Tag $Version
    $arch = Get-Architecture
    Write-Info "Installing ccm (windows/$arch, version $Version)"

    $staging = Join-Path ([IO.Path]::GetTempPath()) ("ccm-install-" + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Force -Path $staging | Out-Null

    try {
        # Hashtables compare keys case-insensitively, which would let an asset
        # match a differently-cased entry. GitHub asset names are
        # case-sensitive, so use an ordinal-comparing dictionary.
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
    finally {
        Remove-Item $staging -Recurse -Force -ErrorAction SilentlyContinue
    }
}

# --- boundary ---------------------------------------------------------------
# Preferences are saved and restored because `iex` runs this in the caller's
# scope, where leaving them changed would alter how the user's own shell
# behaves for the rest of its life.

$ccmPrevErrorAction = $ErrorActionPreference
$ccmPrevProgress = $ProgressPreference
$ccmPrevTls = $null
try { $ccmPrevTls = [Net.ServicePointManager]::SecurityProtocol } catch { }

$ErrorActionPreference = 'Stop'
# Invoke-WebRequest is dramatically slower with the progress bar on 5.1.
$ProgressPreference = 'SilentlyContinue'

# Windows PowerShell 5.1 defaults to TLS 1.0 for outbound HTTPS, which GitHub
# refuses. Without this the download fails with an unhelpful connection error.
try {
    [Net.ServicePointManager]::SecurityProtocol =
        [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
} catch {
    # PowerShell 7 manages this itself and the property may be unavailable.
}

$ccmFailed = $false
try {
    Invoke-CcmInstall
} catch {
    Write-Host "error: $($_.Exception.Message)" -ForegroundColor Red
    $ccmFailed = $true
} finally {
    $ErrorActionPreference = $ccmPrevErrorAction
    $ProgressPreference = $ccmPrevProgress
    if ($null -ne $ccmPrevTls) {
        try { [Net.ServicePointManager]::SecurityProtocol = $ccmPrevTls } catch { }
    }
}

if ($ccmFailed) {
    # Only a real file invocation may exit; under `irm | iex` that would close
    # the user's session and throw away the message printed above.
    if ($MyInvocation.MyCommand.Path) { exit 1 }
    Write-Host "Installation failed." -ForegroundColor Red
}
