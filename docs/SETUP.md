# Setup guide

From nothing to switching accounts offline. Roughly five minutes, most of it spent signing into
your second account once.

---

## 1. Install

Download the installer for your platform from
[Releases](https://github.com/MAbbasRaza/claude-code-multi-account-manager/releases/latest) and
run it. Each installs the `ccm` command, the desktop app and the tray app together.

| Platform | File | What it does |
|---|---|---|
| Windows | `ccm-setup-windows.exe` | Installs to `%LOCALAPPDATA%\Programs\ccm`, adds Start Menu shortcuts, and offers to start the tray app at login. Per-user, so no UAC prompt. |
| macOS | `ccm-macos.pkg` | Installs two apps into `/Applications` and `ccm` into `/usr/local/bin`. Asks for your password. |
| Ubuntu, Debian | `ccm-linux-amd64.deb` | `sudo apt install ./ccm-linux-amd64.deb`. Adds both apps to your application menu. |

The Windows installer's components page lets you choose what you get. Everything is ticked by
default:

| Option | What it does |
|---|---|
| Command line tool | Always installed, and added to your `PATH` |
| Desktop app | The window for managing accounts |
| System tray app | The tray icon for switching without opening anything |
| Shortcuts > Start Menu | Entries for the desktop app and the tray app |
| Shortcuts > Desktop shortcut | An icon on your desktop for the desktop app |
| Start the tray app when I log in | Registers the tray to run at login |

Options that depend on something else are unticked and greyed out when that thing is deselected,
rather than silently creating a shortcut to a program that was never installed. Deselect the tray
app and start-at-login goes with it; deselect the desktop app and the desktop shortcut goes too.

**You can change any of these later**, from the desktop app's Settings or from the command line,
and on every platform rather than only through the Windows installer:

```bash
ccm shortcut status              # what exists now, and where
ccm shortcut add                 # desktop and application menu
ccm shortcut add desktop         # just one of them
ccm shortcut remove menu
ccm autostart enable             # or: disable, status
```

They are read back from disk rather than remembered, so a shortcut you delete yourself shows as
absent the next time you look.

### First launch on an unsigned build

These builds are **not signed** by Apple or by a Windows code-signing certificate yet, and both
operating systems will warn you. The warning is expected, not a sign that something is wrong.
Verify the file against `SHA256SUMS` from the same release instead of relying on a publisher
name.

**macOS** refuses to open an unsigned `.pkg` from Finder at all: you get *"Apple could not
verify this app is free of malware"* with no way past it. Install from Terminal instead:

```bash
sudo installer -pkg ~/Downloads/ccm-macos.pkg -target /
```

Or strip the quarantine flag the browser applied, then double-click as normal:

```bash
xattr -d com.apple.quarantine ~/Downloads/ccm-macos.pkg
```

**Windows** shows *"Windows protected your PC"* with the Run button hidden. Click **More info**,
then **Run anyway**. The publisher will read "Unknown publisher".

### Command line only

If you want just the `ccm` command and no desktop apps:

**macOS and Linux**

```bash
curl -fsSL https://raw.githubusercontent.com/MAbbasRaza/claude-code-multi-account-manager/main/install.sh | sh
```

**Windows**

```powershell
irm https://raw.githubusercontent.com/MAbbasRaza/claude-code-multi-account-manager/main/install.ps1 | iex
```

Add `CCM_TRAY=1` (or `-Tray` on Windows) for the tray app as well, though these install loose
binaries rather than real applications: on macOS that means no Dock icon and no application
identity, which is exactly what `ccm-macos.pkg` exists to provide.

These two verify the download against the published `SHA256SUMS`, install into your home
directory, and add it to your `PATH`. Unlike the macOS and Debian packages above, neither ever
asks for `sudo` or administrator rights. If it tells you to open a new terminal, do that before
continuing.

Check it worked:

```bash
ccm --version
```

Other install methods (Homebrew, Scoop, `go install`, from source) are in the
[README](../README.md#install).

---

## 2. Pin your Claude Code directory

```bash
ccm init
```

This detects where Claude Code keeps its configuration and records it, so the CLI, the tray app
and the VS Code extension all agree.

That last part is the reason this step exists. If you export `CLAUDE_CONFIG_DIR` from `.bashrc`,
`.zshrc` or a PowerShell profile, a terminal inherits it but an app launched from your desktop
does not, and the two would quietly manage different installations. Pinning removes the guesswork.

---

## 3. Capture the account you are signed into right now

```bash
ccm add work
```

Use any name you like. `ccm` reads the account currently signed into Claude Code and parks a copy.

> **Do this before you sign out of anything.**
>
> `/logout` destroys the refresh token. Once it is gone there is nothing left to save, and that
> account needs a full browser sign-in again. Capture first, always.

You can run this while Claude Code is open. Only the actual switch requires closing it.

---

## 4. Add your second account

Inside Claude Code:

```
/login
```

Sign in as the other account. If Claude Code does not offer a choice, run `/logout` first — this
is safe now, because the account you were using is already in the vault.

Back in your terminal:

```bash
ccm add personal
```

Repeat for as many accounts as you like. Check the result:

```bash
ccm list
```

```text
* personal   me@example.com        max
  work       jc@example.com        max
```

The `*` marks the account currently active in Claude Code.

---

## 5. Switch

```bash
ccm use work
```

or run `ccm` on its own for an interactive picker.

**Close Claude Code and reopen it for the switch to take effect.** Credentials are read at
startup, so a running session keeps using the previous account. `ccm` refuses to switch while
Claude Code is running and lists the process IDs; that refusal is protecting you, because a live
session rewrites its config when it exits and would undo the switch.

From here on, switching never touches the network and never opens a browser.

---

## Renaming and removing profiles

```bash
ccm rename work day-job     # keeps the stored credentials
ccm rm old-account
```

Use `ccm rename` rather than removing and re-adding. Re-adding only works for the account
currently signed into Claude Code, so for any other profile that sequence would discard the only
stored copy of its refresh token and leave a browser sign-in as the only way back.

---

## Optional: the desktop app

If you would rather not use the terminal at all, install the desktop app:

```powershell
# Windows, alongside the CLI
.\install.ps1 -Gui
```

```bash
# macOS, Linux
CCM_GUI=1 curl -fsSL https://raw.githubusercontent.com/MAbbasRaza/claude-code-multi-account-manager/main/install.sh | sh
```

Run `ccm-gui`. It gives you one window with everything: the list of saved accounts with the
active one marked, and buttons to switch, capture the current login, rename, remove, view
diagnostics, and change settings.

It is the only surface where every operation is available. A tray menu cannot take text input, so
renaming is impossible there, and the VS Code extension only helps if you work in VS Code.

It draws through the browser engine your system already has: WebView2 on Windows, WKWebView on
macOS, WebKitGTK on Linux. Nothing to install, and no OpenGL, which matters on machines where a
GPU-drawn toolkit fails to start.

Like the tray, it needs cgo, so prebuilt binaries ship for linux/amd64, darwin/amd64,
darwin/arm64 and windows/amd64. The platform installers include it already; on macOS it is the
`Claude Code Accounts` app in `/Applications`, and on Debian it appears in your application menu.
Building from source on Linux additionally needs `libgtk-3-dev` and `libwebkit2gtk-4.1-dev`.

---

## Optional: the tray app

Run `ccm-tray`. It puts an icon in your system tray with one entry per account; click one to
switch. Failures, including the running-Claude-Code refusal, appear as desktop notifications.

To start it automatically:

```bash
ccm autostart enable      # or: disable, status
```

It registers with whichever mechanism your system uses, all per-user and none needing
administrator rights:

| Platform | Mechanism | Set up by the installer? |
|---|---|---|
| Windows | `HKCU\…\CurrentVersion\Run` | Yes, ticked by default; untick it during setup |
| macOS | LaunchAgent in `~/Library/LaunchAgents` | No, run the command above |
| Linux | XDG entry in `~/.config/autostart` | No, run the command above |

**Only the Windows installer can do this for you, and the asymmetry is deliberate.** That
installer runs as you. The macOS `.pkg` and the Debian package both run as root, while the entry
they would need to write is per-user, so there is no correct user for them to register. Rather
than guess, they leave it to you. You can also change it any time from the tray menu or the
desktop app's Settings.

`ccm autostart status` prints which mechanism is in use and where the entry lives, so you can
remove it by hand if you prefer.

The tray app needs cgo, so prebuilt binaries ship only for linux/amd64, darwin/amd64,
darwin/arm64 and windows/amd64. On other platforms, build it from source.

---

## Optional: the VS Code extension

Download `ccm-extension.vsix` from the
[latest release](https://github.com/MAbbasRaza/claude-code-multi-account-manager/releases/latest),
then:

```bash
code --install-extension ccm-extension.vsix
```

It adds your active account to the status bar, a **Claude: Switch Account** command, and a
**Reload Window** prompt after switching, which is exactly the restart the switch needs.

It requires `ccm` on your `PATH`, or set `ccm.binaryPath` in settings. Run `ccm init` before using
it: a VS Code window launched from your desktop does not inherit `CLAUDE_CONFIG_DIR` from your
shell profile, so without pinning it can resolve a different directory than your terminal does.

---

## macOS over SSH or in tmux

Claude Code keeps credentials in the Keychain on macOS, which is unavailable in a detached tmux
pane or an SSH session. Claude Code falls back to `~/.claude/.credentials.json` when that file
exists, so point `ccm` at the file:

```bash
ccm config set credentialsBackend file
```

Valid values are `auto` (default), `file` and `keychain`.

---

## Troubleshooting

Start with `ccm doctor`. It reports which directory it resolved and why, file permissions, vault
health, expiry per profile, and any running Claude Code processes. Its output is safe to paste
into an issue: it never contains token material.

**"Claude Code is running (N processes)"**
Working as intended. Close Claude Code and retry. `--force` exists but a running session will
usually undo your switch when it exits.

**"no active login found; run /login in Claude Code first"**
`ccm` found no signed-in account to capture. Sign in first, then `ccm add`.

**"account X is already saved as profile Y"**
That account is already in the vault. One account maps to exactly one profile, because duplicates
would leave only one copy receiving refreshed tokens while the other silently went stale. Run
`ccm add Y` to refresh the existing profile.

**"token expired" next to a profile in `ccm list`**
The stored access token has lapsed. `ccm` can still install the profile and Claude Code will
usually refresh it. If the underlying login has also expired, Claude Code asks for `/login`, and
no switcher can avoid that.

**"Login expired · Please run /login" after switching**
A `/login` session has a finite lifetime independent of token refresh. Sign in again for that
account and re-run `ccm add <name>` to refresh the profile.

**`ccm doctor` warns that precedence levels disagree**
Two sources name different directories. Run `ccm init` to pin one for every surface.

**The command is not found after installing**
Open a new terminal. On Windows, the installer updates your user `PATH`, which existing terminals
do not pick up.

---

## Where things are stored

| | Windows | macOS | Linux |
|---|---|---|---|
| Settings | `%APPDATA%\ccm\` | `~/Library/Application Support/ccm/` | `$XDG_CONFIG_HOME/ccm/` |
| Vault | `%LOCALAPPDATA%\ccm\` | `~/Library/Application Support/ccm/` | `$XDG_DATA_HOME/ccm/` |
| Protection | DPAPI, bound to your user | AES-256-GCM, key in Keychain | file mode 0600 |

Both locations can be overridden with `CCM_HOME`. Every switch writes a timestamped backup of
Claude Code's two files into the vault's `backups/` directory first, and the path is printed in
the output.

## Uninstall

| Installed with | Remove with |
|---|---|
| `ccm-setup-windows.exe` | Settings > Apps > Claude Code Multi-Account Manager |
| `ccm-macos.pkg` | `sudo ccm-uninstall` |
| `ccm-linux-amd64.deb` | `sudo apt remove ccm` |
| `install.sh`, `install.ps1`, Homebrew, Scoop, manual | Delete the binaries, or use the package manager you installed with |

All of them also undo start-at-login and, on Windows, the `PATH` entry.

**None of them delete your saved accounts.** The vault holds the credentials for every account
you added, and an uninstall that destroyed it would force a browser sign-in for each one to get
back. Remove the two directories listed above yourself if you really want them gone. Nothing is
written anywhere else except your own Claude Code configuration directory.
