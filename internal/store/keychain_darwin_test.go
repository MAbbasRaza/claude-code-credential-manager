//go:build darwin

package store

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/MAbbasRaza/claude-code-multi-account-manager/internal/config"
)

// testKeychainStore builds a store bound to a throwaway Keychain service.
//
// It must never be config.KeychainService. That item is the user's live Claude
// Code login, and a test that wrote to it would log the developer out of Claude
// Code and destroy a refresh token that only a browser sign-in can replace.
func testKeychainStore(t *testing.T) *KeychainStore {
	t.Helper()

	if _, err := exec.LookPath("security"); err != nil {
		t.Skip("the security command is unavailable")
	}
	skipIfKeychainLocked(t)

	svc := fmt.Sprintf("ccm-test-%d-%d", os.Getpid(), time.Now().UnixNano())
	if svc == config.KeychainService {
		t.Fatal("refusing to run against the real Claude Code keychain item")
	}

	acct := keychainTestAccount()
	k := &KeychainStore{Service: svc, Account: acct}
	t.Cleanup(func() {
		_ = exec.Command("security", "delete-generic-password", "-s", svc, "-a", acct).Run()
	})
	return k
}

func keychainTestAccount() string {
	if acct := os.Getenv("USER"); acct != "" {
		return acct
	}
	return "ccm-test"
}

// skipIfKeychainLocked skips when this session cannot write to the login
// keychain at all.
//
// macOS keeps it locked for any session that did not log in through the GUI, so
// these tests cannot run over SSH, in a LaunchAgent that starts before login,
// or on a headless runner. Failing there would misreport a working backend as
// broken: confirmed on a 2018 Intel Mac, where /usr/bin/security exits 36 with
// errSecInteractionNotAllowed for every write, while the identical flags
// succeed against an unlocked keychain.
//
// The probe uses its own throwaway service name and removes it, so it cannot
// disturb the never-signed-in assertion the round-trip test makes first.
func skipIfKeychainLocked(t *testing.T) {
	t.Helper()

	acct := keychainTestAccount()
	svc := fmt.Sprintf("ccm-probe-%d-%d", os.Getpid(), time.Now().UnixNano())

	cmd := exec.Command("security", "add-generic-password",
		"-U", "-s", svc, "-a", acct, "-w", "probe")
	var errb bytes.Buffer
	cmd.Stderr = &errb
	err := cmd.Run()
	_ = exec.Command("security", "delete-generic-password", "-s", svc, "-a", acct).Run()

	if err == nil {
		return
	}
	msg := strings.TrimSpace(errb.String())
	if strings.Contains(msg, "User interaction is not allowed") {
		t.Skipf("the macOS login keychain is locked for this session, which is normal "+
			"over SSH or on a headless runner: %s", msg)
	}
	t.Fatalf("keychain probe failed for an unexpected reason: %v: %s", err, msg)
}

// The macOS backend was written without a Mac to test on. This is the only
// thing that exercises it for real: everything else in the suite forces the
// file backend so it can run on all three platforms.
func TestKeychainStoreRoundTrip(t *testing.T) {
	k := testKeychainStore(t)

	// A store with no item yet is the never-signed-in case, which must read as
	// empty rather than as an error.
	blob, err := k.LoadBlob()
	if err != nil {
		t.Fatalf("LoadBlob on a missing item should not error: %v", err)
	}
	if len(blob) != 0 {
		t.Fatalf("expected an empty blob for a missing item, got %d bytes", len(blob))
	}

	doc := `{"claudeAiOauth":{"accessToken":"A","refreshToken":"R"},"mcpOAuth":{"gmail":{"accessToken":"G"}}}`
	if err := k.SaveBlob([]byte(doc)); err != nil {
		t.Fatalf("SaveBlob: %v", err)
	}

	got, err := k.LoadBlob()
	if err != nil {
		t.Fatalf("LoadBlob after save: %v", err)
	}
	if string(got) != doc {
		t.Errorf("round trip mismatch:\n got  %s\n want %s", got, doc)
	}
}

// Regression guard for the -U flag. security add-generic-password fails with
// -25299 "item already exists" when updating without it, a bug Claude Code
// itself has shipped (anthropics/claude-code#48162). A switch overwrites this
// item on every use, so a missing -U would break the second switch, not the
// first.
func TestKeychainStoreOverwritesExistingItem(t *testing.T) {
	k := testKeychainStore(t)

	if err := k.SaveBlob([]byte(`{"claudeAiOauth":{"accessToken":"FIRST"}}`)); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if err := k.SaveBlob([]byte(`{"claudeAiOauth":{"accessToken":"SECOND"}}`)); err != nil {
		t.Fatalf("second save must update in place, not fail: %v", err)
	}

	got, err := k.LoadBlob()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "SECOND") {
		t.Errorf("expected the updated value, got %s", got)
	}
	if strings.Contains(string(got), "FIRST") {
		t.Errorf("stale value survived the update: %s", got)
	}
}

// Documents that a plausible payload survives the shell round trip. The
// credentials document is JSON with quotes, braces and slashes, and it is
// passed to /usr/bin/security as an argument.
func TestKeychainStoreHandlesJSONPunctuation(t *testing.T) {
	k := testKeychainStore(t)

	doc := `{"a":"v/w+x=","b":["c","d"],"e":{"f":"g h"},"i":"quote\"inside"}`
	if err := k.SaveBlob([]byte(doc)); err != nil {
		t.Fatalf("SaveBlob: %v", err)
	}
	got, err := k.LoadBlob()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != doc {
		t.Errorf("punctuation was mangled:\n got  %s\n want %s", got, doc)
	}
}

// Built directly rather than through testKeychainStore so it still runs in a
// session with no usable keychain; Describe only formats fields.
func TestKeychainStoreDescribe(t *testing.T) {
	k := &KeychainStore{Service: "ccm-test-describe", Account: "someone"}
	d := k.Describe()
	if !strings.Contains(d, k.Service) {
		t.Errorf("Describe should name the service, got %q", d)
	}
}
