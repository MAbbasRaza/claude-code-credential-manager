# Claude Code Multi-Account Manager for VS Code

Switch between Claude Code accounts from inside VS Code, without signing in again.

Claude Code keeps one account's authentication state at a time, so signing into a second account
destroys the first one's refresh token and returning to it means a full browser authorization.
This extension drives [`ccm`](https://github.com/MAbbasRaza/claude-code-multi-account-manager), which
parks each account and swaps it back in offline.

## Requirements

The `ccm` executable must be installed and on your `PATH`, or its location set in
`ccm.binaryPath`. The extension contains no credential logic of its own; everything goes through
`ccm --json` so there is a single implementation to audit.

Run `ccm init` once before using the extension. It pins which Claude Code installation to manage,
which matters here specifically: a VS Code window launched from your desktop does not inherit
`CLAUDE_CONFIG_DIR` from your shell profile, so without pinning it could resolve a different
directory than your terminal does.

## Features

- Status bar item showing the active Claude Code account, click to switch.
- **Claude: Switch Account** picks a profile, then offers to reload the window.
- **Claude: Capture Current Login as Profile** saves the account you are signed into.
- **Claude: Diagnose Credential Manager** runs `ccm doctor` into an output channel.

## Settings

| Setting | Default | Description |
|---|---|---|
| `ccm.binaryPath` | `ccm` | Path to the executable. |
| `ccm.claudeConfigDir` | `""` | Override the config directory for this window. Leave empty to use the pinned one. |
| `ccm.showStatusBar` | `true` | Show the active account in the status bar. |
| `ccm.refreshIntervalSeconds` | `60` | Status bar refresh cadence. `0` disables polling. |

## Notes

A switch takes effect when Claude Code next starts, because credentials are read at startup. The
extension offers **Reload Window** after a successful switch for this reason.

If Claude Code is running, `ccm` refuses the switch and the extension surfaces a confirmation
dialog offering to proceed anyway. Overriding is usually a bad idea: a running session rewrites
`.claude.json` when it exits and would undo the switch.

## License

MIT.
