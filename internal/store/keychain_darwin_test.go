//go:build darwin

package store

import (
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

	acct := os.Getenv("USER")
	if acct == "" {
		acct = "ccm-test"
	}
	svc := fmt.Sprintf("ccm-test-%d-%d", os.Getpid(), time.Now().UnixNano())

	if svc == config.KeychainService {
		t.Fatal("refusing to run against the real Claude Code keychain item")
	}

	k := &KeychainStore{Service: svc, Account: acct}
	t.Cleanup(func() {
		_ = exec.Command("security", "delete-generic-password", "-s", svc, "-a", acct).Run()
	})
	return k
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

func TestKeychainStoreDescribe(t *testing.T) {
	k := testKeychainStore(t)
	d := k.Describe()
	if !strings.Contains(d, k.Service) {
		t.Errorf("Describe should name the service, got %q", d)
	}
}
