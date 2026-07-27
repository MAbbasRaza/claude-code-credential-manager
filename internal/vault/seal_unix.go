//go:build !windows && !darwin

package vault

// NewSealer returns the passthrough sealer on Linux and other unix platforms.
//
// Claude Code stores .credentials.json in plaintext with mode 0600 here, so a
// 0600 vault file matches its protection exactly. Encrypting under a key that
// would have to sit beside it in the same user's home directory adds ceremony
// without changing the threat model.
func NewSealer() Sealer { return plainSealer{} }
