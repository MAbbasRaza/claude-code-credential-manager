package vault

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func newProfile(name, uuid string) *Profile {
	return &Profile{
		Name:          name,
		AccountUUID:   uuid,
		EmailAddress:  name + "@example.invalid",
		ClaudeAiOauth: json.RawMessage(`{"accessToken":"A","refreshToken":"R"}`),
		OAuthAccount:  json.RawMessage(`{"accountUuid":"` + uuid + `"}`),
	}
}

func openTemp(t *testing.T) *Vault {
	t.Helper()
	t.Setenv("CCM_HOME", t.TempDir())
	v, err := Open(filepath.Join(t.TempDir(), "vault.json"))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestSaveAndReopenRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CCM_HOME", home)
	path := filepath.Join(home, "vault.json")

	v, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	v.Put(newProfile("work", "acct-a"))
	if err := v.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Reopening exercises the platform sealer end to end: DPAPI on Windows,
	// Keychain-backed AES-GCM on macOS, plain 0600 on Linux.
	v2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	p, err := v2.Get("work")
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if p.AccountUUID != "acct-a" {
		t.Errorf("AccountUUID = %q, want acct-a", p.AccountUUID)
	}
	if string(p.ClaudeAiOauth) == "" {
		t.Error("credentials did not survive the round trip")
	}
}

// Go randomizes map iteration. If a vault ever holds two profiles for one
// account, the lookup must still resolve to the same one every time, otherwise
// capture-on-switch refreshes a different profile on each run and lets the
// other decay into a dead refresh token.
func TestFindByAccountUUIDIsDeterministic(t *testing.T) {
	v := openTemp(t)
	for _, n := range []string{"zeta", "alpha", "middle", "beta"} {
		v.Put(newProfile(n, "same-account"))
	}

	first, ok := v.FindByAccountUUID("same-account")
	if !ok {
		t.Fatal("expected a match")
	}
	for i := 0; i < 50; i++ {
		got, ok := v.FindByAccountUUID("same-account")
		if !ok || got.Name != first.Name {
			t.Fatalf("iteration %d returned %v, want a stable %q", i, got, first.Name)
		}
	}
	if first.Name != "alpha" {
		t.Errorf("expected the lexicographically first name, got %q", first.Name)
	}
}

func TestFindAllAndDuplicateAccounts(t *testing.T) {
	v := openTemp(t)
	v.Put(newProfile("work", "acct-a"))
	v.Put(newProfile("work-copy", "acct-a"))
	v.Put(newProfile("personal", "acct-b"))

	all := v.FindAllByAccountUUID("acct-a")
	if len(all) != 2 {
		t.Fatalf("FindAllByAccountUUID returned %d, want 2", len(all))
	}
	if all[0].Name != "work" || all[1].Name != "work-copy" {
		t.Errorf("results not sorted by name: %q, %q", all[0].Name, all[1].Name)
	}

	dupes := v.DuplicateAccounts()
	if len(dupes) != 1 {
		t.Fatalf("DuplicateAccounts returned %d entries, want 1: %v", len(dupes), dupes)
	}
	if names := dupes["acct-a"]; len(names) != 2 {
		t.Errorf("acct-a duplicates = %v, want two names", names)
	}
	if _, reported := dupes["acct-b"]; reported {
		t.Error("a single-profile account must not be reported as duplicated")
	}
}

func TestEmptyUUIDNeverMatches(t *testing.T) {
	v := openTemp(t)
	// A profile captured before ccm recorded account UUIDs has an empty one.
	// It must not match a live account that also lacks a UUID, since that
	// would park an unrelated account's tokens on top of it.
	v.Put(newProfile("legacy", ""))
	if _, ok := v.FindByAccountUUID(""); ok {
		t.Error("an empty account UUID must not match anything")
	}
	if len(v.DuplicateAccounts()) != 0 {
		t.Error("profiles with no account UUID must not be grouped as duplicates")
	}
}

func TestUniqueName(t *testing.T) {
	v := openTemp(t)
	if got := v.UniqueName("work"); got != "work" {
		t.Errorf("UniqueName on a free name = %q, want work", got)
	}
	v.Put(newProfile("work", "acct-a"))
	if got := v.UniqueName("work"); got != "work-2" {
		t.Errorf("UniqueName on a taken name = %q, want work-2", got)
	}
	v.Put(newProfile("work-2", "acct-b"))
	if got := v.UniqueName("work"); got != "work-3" {
		t.Errorf("UniqueName = %q, want work-3", got)
	}
	if got := v.UniqueName(""); got != "captured" {
		t.Errorf("UniqueName on an empty base = %q, want captured", got)
	}
}

// Renaming has to preserve the credentials. The whole reason this operation
// exists is that the obvious alternative, remove and re-capture, only works for
// the account currently signed into Claude Code and would otherwise destroy a
// refresh token nothing can regenerate offline.
func TestRenamePreservesCredentials(t *testing.T) {
	v := openTemp(t)
	p := newProfile("work", "acct-a")
	p.ClaudeAiOauth = json.RawMessage(`{"accessToken":"KEEP-ME","refreshToken":"KEEP-ME-TOO"}`)
	v.Put(p)

	if err := v.Rename("work", "day-job"); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	if _, err := v.Get("work"); err == nil {
		t.Error("the old name should no longer resolve")
	}
	got, err := v.Get("day-job")
	if err != nil {
		t.Fatalf("Get(day-job): %v", err)
	}
	if got.Name != "day-job" {
		t.Errorf("Name = %q, want day-job", got.Name)
	}
	if got.AccountUUID != "acct-a" {
		t.Errorf("AccountUUID = %q, want acct-a", got.AccountUUID)
	}
	if string(got.ClaudeAiOauth) != `{"accessToken":"KEEP-ME","refreshToken":"KEEP-ME-TOO"}` {
		t.Errorf("credentials were altered: %s", got.ClaudeAiOauth)
	}
	if n := len(v.List()); n != 1 {
		t.Errorf("vault holds %d profiles, want 1", n)
	}
}

func TestRenameRejectsCollisionAndMissing(t *testing.T) {
	v := openTemp(t)
	v.Put(newProfile("work", "acct-a"))
	v.Put(newProfile("personal", "acct-b"))

	if err := v.Rename("work", "personal"); err == nil {
		t.Error("renaming onto an existing name must fail rather than overwrite it")
	}
	// The target must be untouched by the failed attempt.
	if p, err := v.Get("personal"); err != nil || p.AccountUUID != "acct-b" {
		t.Error("the existing profile was disturbed by a failed rename")
	}
	if err := v.Rename("nope", "whatever"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestRenameToSameNameIsANoOp(t *testing.T) {
	v := openTemp(t)
	v.Put(newProfile("work", "acct-a"))
	if err := v.Rename("work", "work"); err != nil {
		t.Fatalf("renaming to the same name should be harmless: %v", err)
	}
	if n := len(v.List()); n != 1 {
		t.Errorf("vault holds %d profiles, want 1", n)
	}
}

func TestValidateName(t *testing.T) {
	valid := []string{"work", "day-job", "personal_2", "a", "Work.Account"}
	for _, n := range valid {
		if err := ValidateName(n); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", n, err)
		}
	}

	invalid := map[string]string{
		"empty":         "",
		"only spaces":   "   ",
		"leading dash":  "-force",
		"inner space":   "my profile",
		"trailing gap":  "work ",
		"leading gap":   " work",
		"embedded tab":  "wo\trk",
		"embedded line": "wo\nrk",
	}
	for label, n := range invalid {
		if err := ValidateName(n); err == nil {
			t.Errorf("ValidateName(%s: %q) should have failed", label, n)
		}
	}
}

func TestRenameRejectsInvalidTargetName(t *testing.T) {
	v := openTemp(t)
	v.Put(newProfile("work", "acct-a"))
	// A dash-prefixed name would be parsed as a flag by the CLI.
	if err := v.Rename("work", "-f"); !errors.Is(err, ErrBadName) {
		t.Errorf("expected ErrBadName, got %v", err)
	}
	if _, err := v.Get("work"); err != nil {
		t.Error("the original profile must survive a rejected rename")
	}
}

func TestDeleteAndListOrdering(t *testing.T) {
	v := openTemp(t)
	v.Put(newProfile("zeta", "acct-z"))
	v.Put(newProfile("alpha", "acct-a"))

	list := v.List()
	if len(list) != 2 || list[0].Name != "alpha" {
		t.Errorf("List is not sorted by name: %v", list)
	}

	if err := v.Delete("alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Get("alpha"); err == nil {
		t.Error("Get should fail after Delete")
	}
	if err := v.Delete("alpha"); err == nil {
		t.Error("deleting a missing profile should report ErrNotFound")
	}
}
