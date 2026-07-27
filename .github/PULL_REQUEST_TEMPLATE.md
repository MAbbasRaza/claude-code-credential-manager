## What this changes

<!-- One or two sentences. -->

## Why

<!--
For a bug fix, state the concrete failure scenario: what input or state produces the wrong outcome
today, and what the user sees as a result.
-->

## Invariants

This project has three invariants that silently corrupt state when broken. Confirm the ones your
change touches, or mark not applicable.

- [ ] **Only `claudeAiOauth`, `oauthAccount` and `userID` are written.** Everything else in both
      documents survives byte-identically, especially the `mcpOAuth` subtree of MCP server logins.
- [ ] **Capture-on-switch still happens before install.** The outgoing account's live tokens are
      written back to its profile first, so a rotated refresh token is never lost.
- [ ] **One account still maps to exactly one profile.**
- [ ] Not applicable to this change.

## Testing

- [ ] `go test ./... -count=1` passes
- [ ] `gofmt -l .` prints nothing and `go vet ./...` is clean
- [ ] Added or updated tests for the behaviour change
- [ ] No real credentials were used; fixtures are synthetic

Platforms actually exercised:

- [ ] Windows
- [ ] macOS
- [ ] Linux

<!--
Say plainly which platforms you could not test. Unverified is fine and useful to know; a claim
that turns out to be untested is not.
-->
