package store

import (
	"errors"
	"fmt"
	"os"

	"github.com/MAbbasRaza/claude-code-credential-manager/internal/config"
)

// FileStore is the Windows and Linux backend.
//
// Claude Code documents mode 0600 on Linux and relies on the user profile
// directory's ACLs on Windows. ccm writes 0600 on both; on Windows the mode is
// largely advisory but costs nothing.
type FileStore struct {
	Path string
}

func (f *FileStore) LoadBlob() ([]byte, error) {
	b, err := os.ReadFile(f.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read credentials %s: %w", f.Path, err)
	}
	return b, nil
}

func (f *FileStore) SaveBlob(b []byte) error {
	return config.WriteFileAtomic(f.Path, b, 0o600)
}

func (f *FileStore) Describe() string { return f.Path }
