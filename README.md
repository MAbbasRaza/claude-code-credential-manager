<p align="center">
  <img src="assets/icon.png" alt="ccm" width="128" height="128">
</p>

<h1 align="center">Claude Code Credential Manager (<code>ccm</code>)</h1>

<p align="center">Switch between Claude Code accounts without signing in again.</p>

Claude Code stores exactly one account's authentication state at a time. Signing into a second
account overwrites the first one's refresh token, so returning to an account you used ten
minutes ago requires a full browser authorization round trip. `ccm` parks each account's state
in a vault and swaps it back in on demand, turning an account switch into an offline, sub-second
operation.

```text
$ ccm list
* work       jc@example.com        max
  personal   me@example.com        pro

$ ccm use personal
Captured outgoing account jc@example.com (updated "work").
Switched to "personal" (me@example.com).
Restart Claude Code for the new account to take effect.
```

Ships as a CLI, a system tray app, and a VS Code extension, all over one shared core.

---

## The problem, precisely

Your account identity lives in two files, and **each file is shared with state that has nothing
to do with your account**:

| File | Account keys | Also contains |
|---|---|---|
| `.credentials.json` | `claudeAiOauth` (access token, refresh token, expiry, scopes, plan) | `mcpOAuth`: one OAuth entry per MCP server you have ever linked |
| `.claude.json` | `oauthAccount`, `userID` | Per-project history, MCP configuration, trust decisions, onboarding state. Commonly tens of thousands of lines. |

There is exactly one slot for `claudeAiOauth` and one for `oauthAccount`. `/login` overwrites
both. Claude Code has no `login` or `logout` subcommand, only in-session slash commands and
`claude setup-token`.

### Why the obvious fixes are wrong

This is where most switchers go wrong, and it is worth understanding before trusting any of them
(including this one) with your credentials.

**Swapping `.credentials.json` wholesale destroys your MCP logins.** The file holds one
`claudeAiOauth` object next to a `mcpOAuth` map containing every connector you have authorized.
Replace the file and you re-authorize Gmail, Slack, Vercel, Linear and the rest on every single
switch. `ccm` replaces one key and leaves the `mcpOAuth` subtree byte-identical; there is a test
that asserts exactly that.

**Giving each account its own `CLAUDE_CONFIG_DIR` forks your whole setup.** That directory also
holds `settings.json`, plugins, skills, agents, output styles and project history. Isolating per
account means maintaining N copies of all of it. `ccm` keeps one shared configuration and swaps
only the authentication.

**A one-time snapshot goes stale silently.** Claude Code refreshes your access token in the
background, and OAuth refresh tokens rotate when used. A profile saved once at add time holds a
superseded refresh token within hours, which is why several tools in this space "work once, then
stop". `ccm` re-reads the live credentials and writes them back to the outgoing profile *before*
installing the incoming one. This is the single most important thing it does.

---

## Install

**macOS and Linux**

```bash
curl -fsSL https://raw.githubusercontent.com/MAbbasRaza/claude-code-credential-manager/main/install.sh | sh
```

**Windows**

```powershell
irm https://raw.githubusercontent.com/MAbbasRaza/claude-code-credential-manager/main/install.ps1 | iex
```

Both installers detect your architecture, verify the download against the published
`SHA256SUMS`, install into your home directory, and tell you how to fix your `PATH` if needed.
Neither ever asks for `sudo` or administrator rights. Set `CCM_TRAY=1` (or pass `-Tray` on
Windows) to install the tray app as well.

<details>
<summary>Other ways to install</summary>

**Homebrew** (macOS, Linux)

```bash
brew install MAbbasRaza/tap/ccm
```

**Scoop** (Windows) — installs straight from the manifest, no bucket to add:

```powershell
scoop install https://raw.githubusercontent.com/MAbbasRaza/claude-code-credential-manager/main/packaging/scoop/ccm.json
```

**Go** — if you already have a toolchain:

```bash
go install github.com/MAbbasRaza/claude-code-credential-manager/cmd/ccm@latest
```

**Manually** — download from
[Releases](https://github.com/MAbbasRaza/claude-code-credential-manager/releases), check it
against `SHA256SUMS`, rename it to `ccm`, and put it on your `PATH`.

**From source** — requires Go 1.25 or later:

```bash
git clone https://github.com/MAbbasRaza/claude-code-credential-manager.git
cd claude-code-credential-manager
go build -o ccm ./cmd/ccm
go test ./... -count=1

go build -o ccm-tray ./cmd/ccm-tray   # optional; needs cgo, and
                                      # libayatana-appindicator3-dev on Linux
```

A `Makefile` wraps the common targets (`make build`, `make test`, `make check`). See
[CONTRIBUTING.md](CONTRIBUTING.md) for the architecture map and the three invariants any change
must preserve.

</details>

**VS Code extension** — download `ccm-extension.vsix` from the release, then:

```bash
code --install-extension ccm-extension.vsix
```

### Uninstall

Delete the binary, then remove `~/.config/ccm` and `~/.local/share/ccm` on Linux,
`~/Library/Application Support/ccm` on macOS, or `%APPDATA%\ccm` and `%LOCALAPPDATA%\ccm` on
Windows. Nothing is written outside those locations and your own Claude Code config directory.

## Getting started

```bash
ccm init                # detect and pin your Claude Code config directory
ccm add work            # capture the account you are signed into now
                        # then /login as your other account
ccm add personal        # capture that one too
ccm use work            # switch back, no browser
```

**Capture before you sign out.** `/logout` destroys the refresh token, and once it is gone there
is nothing left for `ccm` to save. Run `ccm add` first, every time.

Full walkthrough, including the tray app, the VS Code extension and troubleshooting:
[docs/SETUP.md](docs/SETUP.md).

Adding an account requires one ordinary sign-in per account; there is no supported way to mint a
session offline. After that, switching never touches the network.

You can also skip `ccm add` entirely: when `ccm use` sees a signed-in account it does not
recognize, it parks it as a new profile rather than discarding it. Profiles accumulate simply by
signing in normally.

## Commands

```text
ccm                            interactive account picker
ccm init                       detect and pin the Claude Code config directory
ccm list                       list profiles, marking the active one
ccm use <profile>              switch
ccm add [name]                 capture the current login
ccm rm <profile>               remove a profile
ccm status                     show the active account
ccm config get|set|path        read or change ccm's own settings
ccm doctor                     diagnose path resolution, permissions and vault health

--config-dir <path>            override the config directory for one run
--json                         machine-readable output
--force, -f                    proceed even if Claude Code is running
```

## Configuration

`ccm` resolves which Claude Code installation to manage through five levels, highest first:

1. `--config-dir <path>`
2. `CCM_CLAUDE_CONFIG_DIR`
3. `claudeConfigDir` in ccm's settings file, written by `ccm init`
4. `CLAUDE_CONFIG_DIR`, when inherited
5. Platform default (`~/.claude`)

Level 3 exists for a specific reason. If you export `CLAUDE_CONFIG_DIR` from `.bashrc`, `.zshrc`
or a PowerShell profile, a shell-launched CLI inherits it but a tray app or VS Code window
launched from your desktop does not. Those two would then manage different directories, and the
GUI one would quietly operate on a stale installation. `ccm init` pins the answer so every
surface agrees. `ccm doctor` re-resolves all five levels and warns when they disagree.

Settings file location (override with `CCM_HOME`):

| Platform | Path |
|---|---|
| Windows | `%APPDATA%\ccm\config.json` |
| macOS | `~/Library/Application Support/ccm/config.json` |
| Linux | `$XDG_CONFIG_HOME/ccm/config.json` |

```json
{
  "claudeConfigDir": "/path/to/claude/config",
  "vaultPath": "",
  "requireClosedSessions": true,
  "credentialsBackend": "auto"
}
```

### macOS over SSH or in tmux

On macOS Claude Code keeps credentials in the Keychain, which is unavailable in exactly the
situations where switching accounts is most useful: an SSH session, a tmux pane detached from the
login session, or CI. Claude Code falls back to reading `~/.claude/.credentials.json` when that
file exists, so you can point `ccm` at the file instead:

```bash
ccm config set credentialsBackend file
# or, for one command only:
CCM_CREDENTIALS_BACKEND=file ccm use work
```

Valid values are `auto` (default, the platform's normal store), `file`, and `keychain`.

## Where your tokens are stored

**The vault is never less protected than Claude Code's own credential store on the same
platform.** That rule decides the implementation:

| Platform | Claude Code stores credentials as | `ccm` vault |
|---|---|---|
| Windows | plaintext file | DPAPI, bound to your user account (stronger) |
| macOS | Keychain | AES-256-GCM under a key held in the Keychain (equivalent) |
| Linux | plaintext file, mode 0600 | file, mode 0600 (equivalent) |

Vaults are not portable between machines or users; on Windows and macOS the protection is bound
to the local account. Every write to your real Claude Code files is preceded by a timestamped
backup of both documents.

See [SECURITY.md](SECURITY.md) for the threat model.

## VS Code extension

In `extension/`. Adds a status bar item showing the active account, a **Claude: Switch Account**
command, capture and doctor commands, and a Reload Window prompt after a switch. It shells out to
`ccm --json` and contains no switching logic of its own, so there is one implementation to audit.

```bash
cd extension && npm install && npm run compile
```

## Limitations

Stated plainly, because each one will otherwise look like a bug.

1. **A switch takes effect when Claude Code next starts.** Credentials are read at startup, so a
   running session keeps using the previous account. `ccm` refuses to switch while Claude Code is
   running; pass `--force` if you know what you are doing.

2. **Parked accounts still expire.** A `/login` session has a finite lifetime independent of
   access token refresh. When it lapses, Claude Code reports `Login expired · Please run /login`
   and no amount of capture-on-switch avoids signing in again. `ccm doctor` shows expiry per
   profile.

3. **The macOS Keychain backend is not covered by the automated end-to-end tests.** It is
   implemented and it compiles and unit-tests on macOS in CI, but the full switch cycle has been
   exercised on Windows only. Treat the first macOS switch as something to verify, and note that
   `ccm` backs up both documents before writing.

4. **These file formats are internal to Claude Code and unversioned.** They have already changed
   within the 2.1.x series. `ccm` validates the shape it finds and fails loudly rather than
   writing something Claude Code cannot read, but a future release could still break it.

5. **`CLAUDE_CODE_OAUTH_TOKEN` is deliberately not used.** It sits above subscription credentials
   in Claude Code's authentication precedence, so it would mask whichever account you switched to,
   and it has been reported to delete macOS Keychain credentials on exit
   ([anthropics/claude-code#37512](https://github.com/anthropics/claude-code/issues/37512)).

## Prior art

[ccswitch](https://github.com/vyshnavsdeepak/ccswitch) ·
[claude-swap](https://github.com/realiti4/claude-swap) ·
[cc-account-switcher](https://github.com/KagasiraBunJee/cc-account-switcher) ·
[claude-code-profiles](https://github.com/pegasusheavy/claude-code-profiles) ·
[claude-profile-switch](https://github.com/guibes/claude-profile-switch) ·
[CCSwitcher](https://github.com/XueshiQiao/CCSwitcher) ·
[caam](https://github.com/Dicklesworthstone/coding_agent_account_manager)

`ccm` differs in preserving `mcpOAuth` and unrelated `.claude.json` state through a key-scoped
merge, capturing tokens on the way out so profiles do not rot, resolving the config directory
explicitly rather than trusting an inherited environment variable, and refusing to switch under a
running Claude Code process.

## Contributing

Contributions are welcome. Start with [CONTRIBUTING.md](CONTRIBUTING.md), which covers the
development setup, the architecture, and the three invariants that silently corrupt state when
broken.

Two things worth knowing up front:

- **Never test against your real account.** Every test here uses synthetic tokens in a temporary
  directory. The contributing guide explains how to build a scratch installation.
- **macOS verification is especially valuable.** The Keychain backend is implemented and unit
  tested, but the full switch cycle has only ever been exercised on Windows.

Security issues go through a private
[security advisory](https://github.com/MAbbasRaza/claude-code-credential-manager/security/advisories/new),
never a public issue. See [SECURITY.md](SECURITY.md).

## License

MIT. See [LICENSE](LICENSE).

Not affiliated with or endorsed by Anthropic. `ccm` reads and writes Claude Code's local
credential files, which are an internal, unversioned format that can change without notice.
