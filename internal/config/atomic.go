package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteFileAtomic writes to a temporary file in the destination directory,
// fsyncs it, then renames over the target.
//
// The temporary file must share a directory with the target so the rename is
// a same-volume operation. On Windows os.Rename maps to MoveFileEx with
// MOVEFILE_REPLACE_EXISTING, so replacing an existing file works.
//
// This matters more than usual here: a torn write to .claude.json destroys
// tens of thousands of lines of unrelated project state, and a torn write to
// the credentials document can log the user out of every MCP connector.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create dir %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".ccm-*")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	tmpName := tmp.Name()

	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}

	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp: %w", err)
	}
	// Chmod after close; on Windows this is a no-op beyond the read-only bit,
	// where the file inherits the user profile directory's ACLs instead.
	if err := os.Chmod(tmpName, perm); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename %s -> %s: %w", tmpName, path, err)
	}
	return nil
}
