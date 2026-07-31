//go:build darwin

package shortcut

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// macOS has no application menu separate from /Applications. An app installed
// there is already in Launchpad and Spotlight, so a Menu shortcut would have
// nothing to create. Reported unsupported rather than silently succeeding, so
// the CLI and the desktop app hide the option instead of offering one that does
// nothing.
func supported(k Kind) bool { return k == Desktop }

func describe(k Kind) string {
	if k == Desktop {
		return "a symlink on your desktop"
	}
	return ""
}

func desktopDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Desktop"), nil
}

// The name carries no extension. Target is the .app bundle, so Finder shows the
// link with the application's own icon and opens it through LaunchServices.
func location(k Kind, name string) string {
	if k != Desktop {
		return ""
	}
	dir, err := desktopDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, name)
}

func exists(k Kind, name string) (bool, error) {
	p := location(k, name)
	if p == "" {
		return false, nil
	}
	// Lstat, not Stat: a symlink pointing at a bundle that has since been
	// removed still exists as a shortcut and still needs to be reported and
	// cleaned up.
	if _, err := os.Lstat(p); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func add(k Kind, e Entry) error {
	dir, err := desktopDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	path := location(k, e.Name)

	// Replaced rather than updated in place; os.Symlink refuses an existing
	// name, and Add is documented idempotent.
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	if err := os.Symlink(e.Target, path); err != nil {
		return fmt.Errorf("link %s: %w", path, err)
	}
	return nil
}

func remove(k Kind, name string) error {
	p := location(k, name)
	if p == "" {
		return nil
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", p, err)
	}
	return nil
}
