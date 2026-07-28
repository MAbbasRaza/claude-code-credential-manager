# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
