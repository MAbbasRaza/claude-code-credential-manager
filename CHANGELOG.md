# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.1] - 2026-07-29

### Fixed

- The desktop app misreported a broken running-session check as a live session. `manager` refuses
  a switch when it cannot determine what is running, and that refusal reads "could not determine
  whether Claude Code is running". The app classified refusals with a substring test for "Claude
  Code is running", which that sentence contains, so a machine where process enumeration fails
  showed a modal reading "0 Claude Code processes are running" beside a "Switch anyway" button.
  A user who correctly believed nothing was running would override a guard that had not fired.
  Refusals are now distinguished by type rather than by message text, and the two states are
  presented differently: one names the process IDs and offers an override, the other says the
  check itself failed, shows the real reason, and does not.
- The app also treated an enumeration failure as "nothing is running" in the header banner and in
  Diagnostics, so a report pasted into a bug thread claimed "No warnings" on exactly the machines
  where the guard was inoperative. Both now say so, matching what the CLI already did.

### Added

- Tests for the desktop app's binding layer and for `internal/proc`, both of which shipped
  untested. Two assert that no token material reaches the page or the diagnostics text, which the
  UI advertises as safe to share. One pins the JSON contract in both directions, since a mismatch
  between a Go tag and the property the page reads renders a blank field with no error anywhere.

## [0.2.0] - 2026-07-28

### Added

- `ccm-gui`, a desktop application. One window for everything: the saved accounts with the active
  one marked, and switch, capture, rename, remove, diagnostics and settings. It is the only
  surface where every operation is available, since a tray menu cannot take text input and the
  VS Code extension only helps inside VS Code.
- The tray gains a **Manage accounts…** entry that opens it, shown only when it is installed.
- `install.ps1 -Gui` and `CCM_GUI=1` for the shell installer.

### Notes

The desktop app renders through the platform's own browser engine: WebView2 on Windows,
WKWebView on macOS, WebKitGTK on Linux. The first implementation used Fyne, which draws through
OpenGL, and it crashed at window creation on the development machine. A ten-line Fyne program
crashed identically, so the fault was the toolkit against that graphics setup rather than this
code, and shipping a GUI that cannot start is worse than shipping none. The replacement also
turned out to be a tenth of the size: 6.6 MB against 42.4 MB.

[0.2.0]: https://github.com/MAbbasRaza/claude-code-credential-manager/releases/tag/v0.2.0

## [0.1.2] - 2026-07-28

### Added

- `ccm rename <old> <new>`, which moves a profile while keeping its stored credentials.
- A management UI in the VS Code extension: **Claude: Manage Accounts** lists every profile with
  inline rename and remove buttons, alongside separate rename and remove commands.

### Fixed

- The error raised when an account is already stored under another name recommended
  `ccm rm <name>` as the way to rename it. That advice was destructive. Re-adding only works for
  the account currently signed into Claude Code, so following it for a parked profile discarded
  the only stored copy of a refresh token that nothing can regenerate without a browser sign-in.
  It now points at `ccm rename`.

[0.1.2]: https://github.com/MAbbasRaza/claude-code-credential-manager/releases/tag/v0.1.2

## [0.1.1] - 2026-07-28

### Added

- Official project icon. The tray app now embeds the real artwork at 16, 32, 48, 64 and 128
  pixels, each downsampled once from the 1254px original rather than scaled at runtime, and
  assembles them into a multi-resolution Windows ICO so the shell picks the right one per display
  scaling. The VS Code extension carries it as its marketplace icon.

[0.1.1]: https://github.com/MAbbasRaza/claude-code-credential-manager/releases/tag/v0.1.1

## [0.1.0] - 2026-07-27

First release. Verified by CI on Windows, macOS and Linux.

### Added

- CLI (`ccm`) with `init`, `list`, `use`, `add`, `rm`, `status`, `config`, `doctor`, an
  interactive picker, and `--json` output for programmatic use.
- System tray app (`ccm-tray`) for one-click switching.
- VS Code extension: status bar account indicator, switch command, capture command, doctor
  command, and a Reload Window prompt after a switch.
- Key-scoped JSON merge that preserves the `mcpOAuth` subtree of MCP server logins and all
  unrelated `.claude.json` state byte-for-byte.
- Capture-on-switch: the outgoing account's live tokens are written back to its profile before the
  incoming account is installed, so background token rotation cannot strand a profile.
- Five-level config directory resolution with a pinned setting, so the CLI, tray and extension
  agree even when they inherit different environments.
- Per-platform vault protection: DPAPI on Windows, AES-256-GCM under a Keychain-held key on macOS,
  mode 0600 on Linux.
- Refusal to switch while Claude Code is running, a single-writer lock across all surfaces,
  timestamped backups of both documents before every write, and atomic writes throughout.

### Fixed

- One account could be stored under two profile names. Because the account-to-profile lookup
  iterated a Go map, whose order is randomized, capture-on-switch wrote refreshed tokens into a
  nondeterministically chosen duplicate and left the other holding a rotated-away refresh token,
  which silently decayed into a forced browser re-authorization. `add` now refuses to duplicate an
  account or to reuse a name held by a different account, lookup iterates sorted names, and
  `doctor` reports pre-existing duplicates.
- The interactive picker treated a UTF-8 BOM on piped stdin as an invalid selection.
- `doctor` reported a config directory conflict whenever `CLAUDE_CONFIG_DIR` was set, because it
  counted the platform default as a competing claim rather than a fallback.

### Known limitations

- A switch takes effect when Claude Code next starts; credentials are read at startup.
- Parked accounts still expire when the underlying login reaches its lifetime cap, which no amount
  of capture-on-switch avoids.
- The macOS Keychain backend is covered by tests that run against a real Keychain on the CI
  runner, but a full switch between two real Claude accounts has only been exercised on Windows.
- The VS Code extension compiles and its `ccm --json` contract is tested, but its UI has not been
  driven in a real editor window.
- The tray app's menu behaviour is covered only by unit tests; nobody has clicked it.
- `ccm-tray` requires cgo and ships only for linux/amd64, darwin/arm64 and windows/amd64.

[0.1.0]: https://github.com/MAbbasRaza/claude-code-credential-manager/releases/tag/v0.1.0
