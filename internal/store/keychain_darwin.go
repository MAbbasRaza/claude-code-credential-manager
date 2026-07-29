//go:build darwin

package store

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/MAbbasRaza/claude-code-multi-account-manager/internal/config"
)

// KeychainStore is the macOS backend.
//
// Claude Code stores the credentials document as a generic password item named
// "Claude Code-credentials". Shelling out to /usr/bin/security avoids cgo,
// which keeps cross-compilation and CI simple for a tool this small.
type KeychainStore struct {
	Service string
	Account string
}

func newKeychainStore() (Store, error) {
	acct := os.Getenv("USER")
	if acct == "" {
		u, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("cannot determine keychain account: %w", err)
		}
		acct = strings.TrimPrefix(u, "/Users/")
	}
	return &KeychainStore{Service: config.KeychainService, Account: acct}, nil
}

func (k *KeychainStore) LoadBlob() ([]byte, error) {
	cmd := exec.Command("security", "find-generic-password",
		"-s", k.Service, "-a", k.Account, "-w")
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		// security exits 44 when the item does not exist, which is the
		// never-logged-in case rather than a failure.
		if strings.Contains(errb.String(), "could not be found") {
			return nil, nil
		}
		return nil, fmt.Errorf("keychain read (%s/%s): %w: %s",
			k.Service, k.Account, err, strings.TrimSpace(errb.String()))
	}
	return bytes.TrimRight(out.Bytes(), "\n"), nil
}

func (k *KeychainStore) SaveBlob(b []byte) error {
	// -U updates an existing item in place. Without it, security fails with
	// -25299 "item already exists"; Claude Code itself has shipped that bug
	// (anthropics/claude-code#48162), so it is worth being explicit.
	cmd := exec.Command("security", "add-generic-password",
		"-U", "-s", k.Service, "-a", k.Account, "-w", string(b))
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("keychain write (%s/%s): %w: %s",
			k.Service, k.Account, err, strings.TrimSpace(errb.String()))
	}
	return nil
}

func (k *KeychainStore) Describe() string {
	return fmt.Sprintf("macOS Keychain item %q (account %s)", k.Service, k.Account)
}
