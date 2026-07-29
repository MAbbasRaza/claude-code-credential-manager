package vault

import (
	"errors"
	"os"
	"strings"

	"github.com/MAbbasRaza/claude-code-multi-account-manager/internal/config"
)

// ErrBadVaultBackend reports an unusable CCM_VAULT_BACKEND value.
//
// An unrecognised value is refused rather than ignored. Silently falling back
// to the default would mean a user who typed "keychan" believed their vault was
// sealed one way while it was sealed another, and the envelope check would then
// surface it much later as an unreadable vault.
var ErrBadVaultBackend = errors.New("unknown vault backend")

// vaultBackendPref reads and normalises CCM_VAULT_BACKEND. Both "" and "auto"
// mean "use the platform default", and are returned as "".
func vaultBackendPref() string {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(config.EnvVaultBackend)))
	if v == "auto" {
		return ""
	}
	return v
}

// Sealer protects the vault at rest.
//
// Implementations are chosen per platform by NewSealer so the vault is never
// less protected than Claude Code's own credential store on that platform.
type Sealer interface {
	// Name is the stable identifier recorded in the vault envelope. Changing
	// it for an existing scheme would make old vaults unreadable.
	Name() string
	// Describe is human-readable, for doctor output.
	Describe() string
	Seal(plain []byte) ([]byte, error)
	Unseal(sealed []byte) ([]byte, error)
}

// plainSealer is a passthrough used where the platform gives no user-scoped
// secret store beyond file permissions. The vault file is written 0600, which
// is exactly what Claude Code does with .credentials.json on the same platform.
type plainSealer struct{}

func (plainSealer) Name() string { return "plain-0600" }
func (plainSealer) Describe() string {
	return "file permissions only (0600), matching Claude Code's own storage on this platform"
}
func (plainSealer) Seal(plain []byte) ([]byte, error)    { return plain, nil }
func (plainSealer) Unseal(sealed []byte) ([]byte, error) { return sealed, nil }
