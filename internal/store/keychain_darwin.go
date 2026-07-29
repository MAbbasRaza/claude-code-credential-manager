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

// keychainErr explains a locked login keychain instead of surfacing "exit
// status 36".
//
// The same condition that blocks the vault key blocks the credentials item, so
// the two report it the same way, differing only in which override resolves it.
// Confirmed on a 2018 Intel Mac over SSH, where /usr/bin/security refuses with
// errSecInteractionNotAllowed rather than prompting.
func keychainErr(op string, err error, stderr string) error {
	msg := strings.TrimSpace(stderr)
	if strings.Contains(msg, "User interaction is not allowed") {
		return fmt.Errorf("%s: the macOS login keychain is locked for this session, "+
			"which is normal over SSH or in a LaunchAgent that starts before login. "+
			"Run ccm from a graphical login session, or unlock it first with "+
			"`security unlock-keychain`, or set %s=file if Claude Code on this machine "+
			"keeps its credentials in ~/.claude/.credentials.json: %w: %s",
			op, config.EnvCredentialsBackend, err, msg)
	}
	return fmt.Errorf("%s: %w: %s", op, err, msg)
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
		return nil, keychainErr(fmt.Sprintf("keychain read (%s/%s)", k.Service, k.Account),
			err, errb.String())
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
		return keychainErr(fmt.Sprintf("keychain write (%s/%s)", k.Service, k.Account),
			err, errb.String())
	}
	return nil
}

func (k *KeychainStore) Describe() string {
	return fmt.Sprintf("macOS Keychain item %q (account %s)", k.Service, k.Account)
}
