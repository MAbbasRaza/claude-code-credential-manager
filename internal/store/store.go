// Package store reads and writes Claude Code's credentials document.
//
// The document is transported differently per platform (a file on Windows and
// Linux, the Keychain on macOS) but its contents are the same JSON either way.
// Keeping the merge logic above this interface means the account-swapping code
// is written once and is platform independent.
package store

import (
	"fmt"

	"github.com/MAbbasRaza/claude-code-multi-account-manager/internal/config"
)

// Store is the whole credentials document, not just the account portion.
// Callers must splice with internal/patch rather than replacing the blob,
// because the document also carries every MCP server login.
type Store interface {
	// LoadBlob returns the raw document. A missing store returns an empty
	// slice and no error, since a machine that has never logged in is a
	// normal state rather than a failure.
	LoadBlob() ([]byte, error)
	// SaveBlob replaces the raw document.
	SaveBlob(b []byte) error
	// Describe is a human-readable location, for doctor output and errors.
	Describe() string
}

// New returns the store matching the resolved paths.
func New(p config.Paths) (Store, error) {
	switch p.Backend {
	case config.BackendFile:
		if p.CredentialsPath == "" {
			return nil, fmt.Errorf("file backend selected but no credentials path resolved")
		}
		return &FileStore{Path: p.CredentialsPath}, nil
	case config.BackendKeychain:
		return newKeychainStore()
	default:
		return nil, fmt.Errorf("unknown credential backend %q", p.Backend)
	}
}
