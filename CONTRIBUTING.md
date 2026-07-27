# Contributing

Thanks for considering a contribution. This tool handles OAuth credentials, so a few of the rules
below are stricter than a typical Go project. Please read the safety section before running
anything.

## Safety first: never test against your real account

`ccm` reads and writes the files Claude Code uses to authenticate you. A careless test can log you
out of every MCP connector, or destroy a refresh token and force a browser re-authorization.

**Every test in this repository uses synthetic tokens in a temporary directory.** Keep it that way:

- Tests set `CLAUDE_CONFIG_DIR` and `CCM_HOME` to `t.TempDir()`. Never point a test at a real
  installation.
- Fixture tokens are obvious fakes such as `FAKE-A-REFRESH`, and fixture emails use the reserved
  `.invalid` TLD. Do not paste anything real.
- To exercise the CLI by hand, build a scratch installation rather than using your own:

  ```bash
  export CCM_HOME=/tmp/ccm-scratch/home
  export CLAUDE_CONFIG_DIR=/tmp/ccm-scratch/claude
  mkdir -p "$CCM_HOME" "$CLAUDE_CONFIG_DIR"
  # write synthetic .credentials.json and .claude.json, then run ./ccm against it
  ```

- **Never commit a vault, a `.credentials.json`, or a `.claude.json`.** `.gitignore` covers the
  obvious names, but check `git diff --staged` before pushing.
- Report vulnerabilities privately through the Security tab, not a public issue. See
  [SECURITY.md](SECURITY.md).

## Prerequisites

| Component | Needs |
|---|---|
| CLI (`cmd/ccm`) | Go 1.25+. Pure Go, cross-compiles anywhere. |
| Tray (`cmd/ccm-tray`) | Go 1.25+, plus cgo on macOS and Linux. On Linux: `libayatana-appindicator3-dev`. |
| Extension (`extension/`) | Node 22+. |

## Build and test

```bash
go build ./cmd/ccm                 # CLI
go build ./cmd/ccm-tray            # tray (needs cgo off Windows)
go test ./... -count=1             # full suite
go test ./internal/patch/ -v       # the correctness core, run this first
gofmt -l .                         # must print nothing
go vet ./...
```

Extension:

```bash
cd extension
npm ci
npm run compile      # or: npm run watch
```

To try the extension for real, open `extension/` in VS Code and press F5 to launch an Extension
Development Host. It needs `ccm` on `PATH`, or `ccm.binaryPath` pointed at your build.

A `Makefile` wraps the common targets if you prefer (`make test`, `make build`, `make check`). It
is optional; the raw commands above are the source of truth.

## Architecture

Read these in order. The first three are where the difficulty lives.

| Package | Responsibility |
|---|---|
| `internal/patch` | Key-scoped JSON splicing. **The correctness core.** |
| `internal/manager` | The switch algorithm, including capture-on-switch. |
| `internal/config` | Five-level config directory resolution, settings, atomic writes. |
| `internal/vault` | Profile storage and per-platform sealing. |
| `internal/store` | Reads/writes Claude Code's credentials document (file or macOS Keychain). |
| `internal/lock`, `internal/proc` | Single-writer guard, running-Claude-Code detection. |
| `cmd/ccm`, `cmd/ccm-tray` | CLI and tray. Thin; no credential logic. |
| `extension/` | VS Code extension. Shells out to `ccm --json`; no credential logic. |

### Three invariants any change must preserve

Break one of these and the tool silently corrupts state or loses an account. Each has tests; if
you change behaviour here, extend them.

1. **Only three keys are ever written**: `claudeAiOauth`, `oauthAccount`, `userID`. Everything else
   in both documents must survive byte-identically, above all the `mcpOAuth` subtree that holds
   every MCP server login. This is why edits splice raw bytes with `sjson` instead of decoding and
   re-encoding. See `TestSetCredentialsPreservesMCPOAuthByteForByte`.

2. **Capture-on-switch**. Claude Code refreshes tokens in the background and refresh tokens
   *rotate*. The outgoing account's live tokens must be written back to its profile *before* the
   incoming account is installed, or that profile decays into a dead token. See
   `TestCaptureOnSwitchPicksUpBackgroundRefresh`.

3. **One account maps to exactly one profile**. Duplicates make the account-to-profile lookup
   ambiguous, so only one copy receives refreshed tokens. See
   `TestCaptureRefusesDuplicateAccountUnderAnotherName`.

## Pull requests

- One logical change per PR. A bug fix and a refactor in the same diff is two PRs.
- **Tests are expected for behaviour changes**, especially anything touching `internal/patch` or
  `internal/manager`. A test that would have caught the bug is the best possible justification.
- Match the surrounding style. Comments explain *why*, particularly constraints the code cannot
  show, such as a platform quirk or a Claude Code behaviour. Do not narrate what the code does.
- `gofmt`, `go vet` and the full test suite must pass. CI enforces all three on Linux, macOS and
  Windows.
- Explain the failure scenario in the PR description: what input or state produces the wrong
  outcome today.

Commit messages: a short imperative subject, then a body explaining why. If you fixed a bug, say
what breaks without the fix.

## Platform notes for reviewers

Claude Code stores credentials differently per platform, and the differences are load-bearing:

- **macOS** uses the Keychain item `Claude Code-credentials`, *ignoring* `CLAUDE_CONFIG_DIR` for
  credentials, though `.claude.json` is still an ordinary file. Writes must pass `-U` to
  `security add-generic-password` or they fail with `-25299`.
- **Windows and Linux** use `.credentials.json` in the config directory.
- In the **default** layout the two files are not siblings: credentials live in `~/.claude/` while
  `.claude.json` sits at `~/.claude.json`. Setting `CLAUDE_CONFIG_DIR` moves both inside it.

**Tests that touch the credentials store must force the file backend.** Set
`CCM_CREDENTIALS_BACKEND=file` (as `newEnv` in `internal/manager` does). Without it a test that
writes a synthetic `.credentials.json` passes on Windows and Linux but fails on macOS, where the
manager reads the Keychain and never sees the file. The first CI run on macOS failed for exactly
this reason.

Contributions that can only be tested on one platform are welcome, but say so in the PR. macOS
verification is especially valuable; the original author could only test on Windows.

## Reporting bugs

Open an issue using the bug template. `ccm doctor` output is designed to be safe to paste: it
reports paths, sizes, expiry timestamps and account emails, and never token material. **Do not
paste `.credentials.json` or your vault.**

## Code of conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md).
