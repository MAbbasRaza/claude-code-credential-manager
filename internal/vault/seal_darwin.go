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
func NewSealer() Sealer { return keychainSealer{} }

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
		return nil, fmt.Errorf("read vault key from keychain: %w: %s", err, strings.TrimSpace(errb.String()))
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
		return nil, fmt.Errorf("store vault key in keychain: %w: %s", err, strings.TrimSpace(errb.String()))
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
