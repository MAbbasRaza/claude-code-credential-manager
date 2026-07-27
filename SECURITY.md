# Security

## What this tool handles

`ccm` reads, stores and writes Claude Code OAuth credentials: an access token, a refresh token,
and the account metadata that identifies which Claude account a session belongs to. These are
bearer credentials. Anyone who can read them can use your Claude subscription until they expire
or are revoked.

## Threat model

**In scope.** Protecting parked credentials at rest from other users on the same machine, and
protecting your live Claude Code state from corruption by this tool.

**Out of scope.** An attacker who already has code execution as your user account. On Windows and
Linux, Claude Code itself stores the live credentials in a plaintext file that such an attacker
can simply read. No vault design changes that, and claiming otherwise would be dishonest.

## Protection at rest

The governing rule is that **the vault is never less protected than Claude Code's own credential
store on the same platform**:

| Platform | Claude Code | `ccm` vault |
|---|---|---|
| Windows | plaintext file under the user profile | DPAPI (`CryptProtectData`, user scope) |
| macOS | Keychain item `Claude Code-credentials` | AES-256-GCM; key in Keychain item `ccm-vault-key` |
| Linux | plaintext file, mode 0600 | file, mode 0600 |

On Windows and macOS the vault is bound to the local user account and cannot be read by another
user or moved to another machine. On Linux the protection is file permissions, matching what
Claude Code does with the same secrets. Encrypting under a key stored beside the vault in the same
home directory would add ceremony without changing who can read it.

`ccm` never transmits credentials anywhere. It makes no network requests at all.

## Integrity of your live state

`.claude.json` routinely holds tens of thousands of lines of project history, trust decisions and
MCP configuration. A careless write destroys it. Safeguards:

- **Key-scoped edits.** Only `claudeAiOauth`, `oauthAccount` and `userID` are ever written. Edits
  splice raw bytes rather than decoding and re-encoding, so untouched keys, including the entire
  `mcpOAuth` subtree of MCP server logins, are preserved exactly. Tests assert byte-identity.
- **Atomic writes.** Every write goes to a temporary file in the destination directory, is
  fsynced, then renamed over the target. A crash mid-write cannot truncate either document.
- **Timestamped backups.** Both documents are copied to the vault's `backups/` directory before
  any switch, and the backup path is reported in the output.
- **Single writer.** A lock file serializes the CLI, tray and extension so two switches cannot
  interleave.
- **Fail loudly.** Documents that do not match the expected shape are rejected rather than
  overwritten, and a profile missing a refresh token is refused rather than installed.

## Reporting a vulnerability

Open a private security advisory through GitHub's **Security** tab on this repository. Please do
not open a public issue for anything that would expose credentials.

Include what you did, what happened, and the `ccm --version` output. **Never paste the contents of
`.credentials.json`, your vault file, or any token into an issue or advisory.** `ccm doctor`
output is designed to be safe to share: it reports paths, file sizes, expiry timestamps and
account emails, and never token material.
