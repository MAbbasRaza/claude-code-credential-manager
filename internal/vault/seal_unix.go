//go:build !windows && !darwin

package vault

import "fmt"

// NewSealer returns the passthrough sealer on Linux and other unix platforms.
//
// Claude Code stores .credentials.json in plaintext with mode 0600 here, so a
// 0600 vault file matches its protection exactly. Encrypting under a key that
// would have to sit beside it in the same user's home directory adds ceremony
// without changing the threat model.
//
// "file" is accepted because it names what this platform already does, so a
// CCM_VAULT_BACKEND exported in a shared dotfile does not fail here. "keychain"
// is refused rather than quietly ignored: there is no keychain to use, and
// accepting it would imply protection that is not there.
func NewSealer() (Sealer, error) {
	switch pref := vaultBackendPref(); pref {
	case "", "file", "plain":
		return plainSealer{}, nil
	default:
		return nil, fmt.Errorf("%w %q (valid on this platform: auto, file)", ErrBadVaultBackend, pref)
	}
}
