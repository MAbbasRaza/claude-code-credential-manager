# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Installers for Windows, macOS and Ubuntu.** Each installs the command line tool, the desktop
  app and the tray app together and puts `ccm` on the `PATH`, so the desktop programs arrive as
  real applications instead of loose binaries in a bin directory.
  - **Windows** `ccm-setup-windows.exe`. The NSIS wizard has existed since it was written and was
    never built by anything: there was no `makensis` step in the workflow, and the
    `scripts/build-installer.ps1` its own header referenced had never existed in the tree or in
    git history. That script now exists and is the single place `makensis` runs.
  - **macOS** `ccm-macos.pkg`, an Installer.app wizard that puts two application bundles in
    `/Applications` and the CLI in `/usr/local/bin`. Universal, so one package serves Intel and
    Apple Silicon. Ships `ccm-uninstall`, because a `.pkg` has no uninstaller of its own.
  - **Ubuntu and Debian** `ccm-linux-amd64.deb`, with application menu entries, icons at every
    hicolor size, and a dependency list so apt installs the GTK and WebKit libraries the two cgo
    programs need.
- **Desktop and application menu shortcuts are now selectable, and changeable after install.** The
  Windows components page gained a Shortcuts group with Start Menu and Desktop entries alongside
  the existing start-at-login option, all ticked by default. Options that depend on something else
  grey out when it is deselected, instead of silently creating a shortcut to a program that was
  never installed.
- `ccm shortcut status|add|remove [desktop|menu|all]`, and matching checkboxes in the desktop app's
  Settings. These work on every platform and after any install method, not only through the Windows
  wizard, and they read the current state back from disk rather than remembering it, so a shortcut
  deleted by hand shows as absent.
- `internal/shortcut`, which is why the above is possible at all. The installer could have called
  NSIS `CreateShortcut` in two fewer lines, but then nothing else could have told whether a
  shortcut existed or removed it. This follows the rule start-at-login already set: one
  implementation, and the installer delegates to it.
- Two application bundles on macOS rather than one. macOS derives an application's identity by
  walking up from the executable path, so every binary in one bundle shares its `Info.plist`, and
  a single plist cannot both set `LSUIElement` and not set it. The menu bar app needs it; the
  desktop app must not have it or it loses its Dock icon and Cmd-Tab entry.
- `scripts/relver`, shared by the Windows and macOS packaging. `VIProductVersion` requires exactly
  four numeric parts and `CFBundleShortVersionString` at most three, so a tag like `v0.3.0-rc1`
  would have failed the Windows build outright and produced a bundle Finder shows with no version.
- Packaging smoke tests on every push. The release packaging only runs on a tag, so without them
  the first sign of a broken installer would be a failed release.
- Signing and notarization wired into the macOS release job but inert, gated on secrets that do
  not exist yet, so the pipeline ships unsigned today and notarized the day a Developer ID is
  added.

### Fixed

- `internal/locate` could not find a sibling program across two macOS application bundles, and the
  failure was silent by construction: an empty result means "not installed", so a correctly
  installed Mac would have shown a tray with no "Manage accounts" entry and a `ccm autostart
  enable` insisting the tray was not installed.
- `ccm-tray` kept a private copy of the sibling lookup with two defects the shared implementation
  does not have. It never resolved symlinks, so a tray started through one searched the link's
  directory; the macOS package installs the CLI as a symlink, so this would have bitten
  immediately. And it accepted any directory entry, so a directory named `ccm-gui` reached `exec`
  and failed at launch with a message the user could do nothing about. The tray's start-at-login
  toggle now takes its path from the same place, so it registers exactly what
  `ccm autostart enable` would rather than disagreeing with it through a symlink.
- The release notes told users to download a loose binary and put it on their `PATH`, and listed a
  set of tray platforms that had been missing darwin/amd64 since Intel Mac support was added.
- Linux desktop entries quoted `Name` and `Comment`. Quoting only means anything inside `Exec`,
  where the value is parsed into arguments; elsewhere a quote is a literal character, so the
  application menu would have shown `"Claude Code Accounts"` with the quotation marks visible.
  `desktop-file-validate` accepts it without complaint, because quotes are legal there, so the
  first Ubuntu run passed while producing exactly that.

### Notes

- **The macOS and Debian packages deliberately do not switch on start-at-login; the Windows
  installer does.** That installer runs as you, while the other two run as root and the entry they
  would write is per-user, so there is no correct user to register for. Rather than guess, they
  leave it to `ccm autostart enable` or the desktop app's Settings.
- **These builds are not signed yet.** macOS will not open the package from Finder at all, so
  docs/SETUP.md documents the Terminal install, and Windows shows a SmartScreen warning. Verify
  downloads against `SHA256SUMS` rather than a publisher name.

- **`ccm` could not run at all on macOS from a session without an unlocked login keychain.** The
  vault's data key lives in the Keychain, and `loadKey` is called by both seal and unseal, so
  every operation failed with `exit status 36` and nothing more. That covers every SSH session,
  every LaunchAgent started before login, and any tmux pane whose keychain has since relocked.
  Found by running the suite on a 2018 Intel Mac, where 21 tests failed this way; GitHub's macOS
  runner has an unlocked keychain and had never shown it. The credentials store already had an
  escape hatch for this exact situation, but the vault had none.
- Both Keychain paths now explain a locked keychain and name the ways out, instead of surfacing
  a bare exit code.
- `internal/manager` tests discarded the error from `Open` in twenty places, so any failure became
  a nil-pointer dereference inside whichever method ran next. That panic aborts the whole package,
  hiding every other test's result behind a stack trace that does not mention the cause.

### Added

- `CCM_VAULT_BACKEND`, the sealing counterpart to `CCM_CREDENTIALS_BACKEND`. On macOS, `file`
  keeps the vault as a 0600 file rather than AES-256-GCM under a Keychain key. It is opt-in, never
  an automatic fallback: falling back on its own would quietly downgrade a file full of refresh
  tokens. Windows refuses it, because DPAPI works in every session and the downgrade would buy
  nothing. Changing backends cannot corrupt a vault; the envelope already records which scheme
  wrote it and `ccm` refuses a mismatch rather than guessing.
- Keychain-dependent tests now skip with the reason when the keychain is unreachable, rather than
  failing and misreporting a working backend as broken.
- CI cross-compiles both cgo programs for Intel Macs on every push and asserts with `file(1)` that
  the output really is `x86_64`, since an ignored `-arch` flag would produce arm64 binaries under
  an amd64 name and still exit 0.

### Verified

- Full suite, including the race detector, on a 2018 Intel Mac (macOS 15.7.7, `go1.26.5
  darwin/amd64`): green.
- A real two-account switch on that machine against synthetic fixtures: the `mcpOAuth` subtree came
  back byte-identical, both case-differing project keys survived, all other `.claude.json` state
  was unchanged, and capture-on-switch parked the outgoing account's rotated refresh token.
- LaunchAgent autostart on that machine: the plist passes `plutil -lint`, points at a real
  executable, and `disable` removes it completely.

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

[0.2.0]: https://github.com/MAbbasRaza/claude-code-multi-account-manager/releases/tag/v0.2.0

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

[0.1.2]: https://github.com/MAbbasRaza/claude-code-multi-account-manager/releases/tag/v0.1.2

## [0.1.1] - 2026-07-28

### Added

- Official project icon. The tray app now embeds the real artwork at 16, 32, 48, 64 and 128
  pixels, each downsampled once from the 1254px original rather than scaled at runtime, and
  assembles them into a multi-resolution Windows ICO so the shell picks the right one per display
  scaling. The VS Code extension carries it as its marketplace icon.

[0.1.1]: https://github.com/MAbbasRaza/claude-code-multi-account-manager/releases/tag/v0.1.1

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

[0.1.0]: https://github.com/MAbbasRaza/claude-code-multi-account-manager/releases/tag/v0.1.0
