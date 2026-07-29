//go:build darwin

package vault

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/MAbbasRaza/claude-code-multi-account-manager/internal/config"
)

// keychainKeyService is the Keychain item holding the vault's data key.
// It is deliberately distinct from Claude Code's own item so ccm can never
// overwrite the live credentials by mistake.
const keychainKeyService = "ccm-vault-key"

// NewSealer returns the macOS sealer.
//
// Claude Code keeps its credentials in the Keychain here, so writing a
// plaintext vault would be a downgrade. Instead the vault file is encrypted
// with AES-256-GCM under a random data key that itself lives in the Keychain.
//
// CCM_VAULT_BACKEND=file opts out of that, down to the same 0600 file ccm uses
// on Linux. It is the only way to run ccm on macOS from a session with no
// unlocked login keychain, and it is opt-in precisely because it is weaker.
func NewSealer() (Sealer, error) {
	switch pref := vaultBackendPref(); pref {
	case "", "keychain":
		return keychainSealer{}, nil
	case "file", "plain":
		return plainSealer{}, nil
	default:
		return nil, fmt.Errorf("%w %q (valid on macOS: auto, keychain, file)", ErrBadVaultBackend, pref)
	}
}

type keychainSealer struct{}

func (keychainSealer) Name() string { return "darwin-keychain-aesgcm" }
func (keychainSealer) Describe() string {
	return "AES-256-GCM under a data key stored in the macOS Keychain"
}

func keychainAccount() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return "ccm"
}

// keychainErr turns the one macOS failure users actually hit into something
// actionable.
//
// /usr/bin/security exits 36 with "User interaction is not allowed" when the
// login keychain is locked for the calling session. That is not an edge case:
// it is every SSH session, every LaunchAgent that runs before login, and any
// tmux pane whose keychain has since relocked. Verified on a 2018 Intel Mac,
// where the whole suite failed this way while GitHub's macOS runner passed,
// because the runner has an unlocked keychain and a developer over SSH does
// not. Unwrapped, the user sees only "exit status 36".
func keychainErr(op string, err error, stderr string) error {
	msg := strings.TrimSpace(stderr)
	if strings.Contains(msg, "User interaction is not allowed") {
		return fmt.Errorf("%s: the macOS login keychain is locked for this session, "+
			"which is normal over SSH or in a LaunchAgent that starts before login. "+
			"Run ccm from a graphical login session, or unlock it first with "+
			"`security unlock-keychain`, or set %s=file to keep the vault as a 0600 "+
			"file instead (weaker than the Keychain, but the same protection Claude "+
			"Code itself uses on Linux): %w: %s",
			op, config.EnvVaultBackend, err, msg)
	}
	return fmt.Errorf("%s: %w: %s", op, err, msg)
}

// loadKey fetches the data key, creating one on first use.
func loadKey() ([]byte, error) {
	acct := keychainAccount()
	cmd := exec.Command("security", "find-generic-password",
		"-s", keychainKeyService, "-a", acct, "-w")
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		if strings.Contains(errb.String(), "could not be found") {
			return createKey(acct)
		}
		return nil, keychainErr("read vault key from keychain", err, errb.String())
	}
	raw := strings.TrimSpace(out.String())
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("vault key in keychain is malformed: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("vault key in keychain is %d bytes, want 32", len(key))
	}
	return key, nil
}

func createKey(acct string) ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	enc := base64.StdEncoding.EncodeToString(key)
	cmd := exec.Command("security", "add-generic-password",
		"-U", "-s", keychainKeyService, "-a", acct, "-w", enc)
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, keychainErr("store vault key in keychain", err, errb.String())
	}
	return key, nil
}

func (keychainSealer) Seal(plain []byte) ([]byte, error) {
	key, err := loadKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

func (keychainSealer) Unseal(sealed []byte) ([]byte, error) {
	key, err := loadKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, fmt.Errorf("vault payload is truncated")
	}
	nonce, ct := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, nil)
}
