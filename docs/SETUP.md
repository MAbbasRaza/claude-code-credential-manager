# Setup guide

From nothing to switching accounts offline. Roughly five minutes, most of it spent signing into
your second account once.

---

## 1. Install

**macOS and Linux**

```bash
curl -fsSL https://raw.githubusercontent.com/MAbbasRaza/claude-code-credential-manager/main/install.sh | sh
```

**Windows**

```powershell
irm https://raw.githubusercontent.com/MAbbasRaza/claude-code-credential-manager/main/install.ps1 | iex
```

Add `CCM_TRAY=1` (or `-Tray` on Windows) if you want the system tray app too.

The installer verifies the download against the published `SHA256SUMS`, installs into your home
directory, and adds it to your `PATH`. It never asks for `sudo` or administrator rights. If it
tells you to open a new terminal, do that before continuing.

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

## Optional: the tray app

Run `ccm-tray`. It puts an icon in your system tray with one entry per account; click one to
switch. Failures, including the running-Claude-Code refusal, appear as desktop notifications.

To start it automatically:

- **Windows** — press `Win+R`, enter `shell:startup`, and put a shortcut to `ccm-tray.exe` there.
- **macOS** — System Settings → General → Login Items → add `ccm-tray`.
- **Linux** — add a `.desktop` entry in `~/.config/autostart/`.

The tray app needs cgo, so prebuilt binaries ship only for linux/amd64, darwin/arm64 and
windows/amd64. On other platforms, build it from source.

---

## Optional: the VS Code extension

Download `ccm-extension.vsix` from the
[latest release](https://github.com/MAbbasRaza/claude-code-credential-manager/releases/latest),
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

Delete the binary, then remove the two directories above. Nothing is written anywhere else except
your own Claude Code configuration directory.
