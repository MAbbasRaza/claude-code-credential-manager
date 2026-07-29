package vault

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A typo must not be silently ignored. If it were, a user who set "keychan"
// would believe the vault was sealed one way while it was sealed another, and
// would only discover it later as an unreadable vault.
func TestNewSealerRejectsUnknownBackend(t *testing.T) {
	t.Setenv("CCM_VAULT_BACKEND", "keychan")

	s, err := NewSealer()
	if err == nil {
		t.Fatalf("expected an error, got sealer %q", s.Name())
	}
	if !errors.Is(err, ErrBadVaultBackend) {
		t.Errorf("error should classify as ErrBadVaultBackend, got %T %v", err, err)
	}
}

// "auto" and "" are the same instruction and must both give the platform
// default, whatever that is here.
func TestAutoAndEmptyAgree(t *testing.T) {
	t.Setenv("CCM_VAULT_BACKEND", "")
	empty, err := NewSealer()
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("CCM_VAULT_BACKEND", "auto")
	auto, err := NewSealer()
	if err != nil {
		t.Fatal(err)
	}

	if empty.Name() != auto.Name() {
		t.Errorf("empty gave %q but auto gave %q", empty.Name(), auto.Name())
	}
}

func TestBackendPrefIsCaseAndSpaceInsensitive(t *testing.T) {
	t.Setenv("CCM_VAULT_BACKEND", "  AUTO  ")
	if got := vaultBackendPref(); got != "" {
		t.Errorf("normalised pref = %q, want \"\"", got)
	}
}

// Windows is the one platform with nothing to escape from, so a downgrade
// request is refused rather than honoured.
func TestWindowsRefusesAWeakerVault(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows only")
	}
	t.Setenv("CCM_VAULT_BACKEND", "file")
	if _, err := NewSealer(); !errors.Is(err, ErrBadVaultBackend) {
		t.Errorf("Windows should refuse a file vault, got %v", err)
	}
}

// The escape hatch that makes ccm usable on a Mac over SSH. This is the case
// that failed on a real 2018 Intel Mac before the backend was selectable: the
// login keychain is locked in a non-GUI session, so every seal and unseal
// failed with errSecInteractionNotAllowed and the vault was unreachable.
func TestDarwinFileBackendWorksWithoutAKeychain(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only")
	}
	t.Setenv("CCM_VAULT_BACKEND", "file")

	s, err := NewSealer()
	if err != nil {
		t.Fatal(err)
	}
	if s.Name() != "plain-0600" {
		t.Fatalf("sealer = %q, want plain-0600", s.Name())
	}

	home := t.TempDir()
	t.Setenv("CCM_HOME", home)
	path := filepath.Join(home, "vault.json")

	v, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	v.Put(newProfile("work", "acct-a"))
	if err := v.Save(); err != nil {
		t.Fatalf("Save must not need the keychain under the file backend: %v", err)
	}

	v2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	p, err := v2.Get("work")
	if err != nil {
		t.Fatal(err)
	}
	if p.AccountUUID != "acct-a" {
		t.Errorf("accountUuid = %q, want acct-a", p.AccountUUID)
	}
}

// Switching backend must fail loudly, never decode a vault under the wrong
// scheme or overwrite it. The envelope records which sealer wrote the file, and
// Open compares before touching the payload.
func TestSwitchingBackendIsRefusedNotMisread(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS is the only platform with two vault backends")
	}

	home := t.TempDir()
	t.Setenv("CCM_HOME", home)
	path := filepath.Join(home, "vault.json")

	t.Setenv("CCM_VAULT_BACKEND", "file")
	v, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	v.Put(newProfile("work", "acct-a"))
	if err := v.Save(); err != nil {
		t.Fatal(err)
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("CCM_VAULT_BACKEND", "keychain")
	if _, err := Open(path); err == nil {
		t.Fatal("opening a file-sealed vault as keychain should fail, not succeed")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("a refused open must leave the vault byte-identical")
	}
}

// The envelope has to name the sealer for that check to be possible at all.
func TestEnvelopeRecordsTheSealer(t *testing.T) {
	skipIfSealerUnavailable(t)

	home := t.TempDir()
	t.Setenv("CCM_HOME", home)
	path := filepath.Join(home, "vault.json")

	v, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	v.Put(newProfile("work", "acct-a"))
	if err := v.Save(); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Sealer string `json:"sealer"`
		Data   string `json:"data"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		t.Fatal(err)
	}
	if env.Sealer != v.sealer.Name() {
		t.Errorf("envelope sealer = %q, want %q", env.Sealer, v.sealer.Name())
	}
	if env.Data == "" {
		t.Error("envelope carries no payload")
	}
}
