// Package autostart registers a program to run when the user logs in.
//
// Each platform has one conventional, per-user mechanism, and this uses that
// rather than anything requiring elevation:
//
//   - Windows: a value under HKCU\...\CurrentVersion\Run
//   - macOS:   a LaunchAgent plist in ~/Library/LaunchAgents
//   - Linux:   a .desktop entry in ~/.config/autostart
//
// All three are per-user and writable without administrator rights, which
// matters because ccm installs into the user's home directory and never asks
// for elevation. A machine-wide autostart would also be wrong on its own terms:
// the vault it opens is bound to one user account.
package autostart

import (
	"errors"
	"fmt"
	"strings"
)

// Entry describes a program to launch at login.
type Entry struct {
	// Name is the stable identifier used as the registry value, plist label
	// suffix or .desktop filename. Changing it orphans an existing
	// registration, so it is not derived from anything cosmetic.
	Name string

	// DisplayName is shown to the user by the desktop environment.
	DisplayName string

	// Exec is the absolute path to the executable.
	Exec string

	// Args are passed on launch.
	Args []string
}

// ErrInvalidEntry reports an entry that cannot be registered.
var ErrInvalidEntry = errors.New("invalid autostart entry")

func (e Entry) validate() error {
	if strings.TrimSpace(e.Name) == "" {
		return fmt.Errorf("%w: name is empty", ErrInvalidEntry)
	}
	// The name becomes a filename on two platforms, so anything that could
	// escape the intended directory is refused rather than sanitised.
	if strings.ContainsAny(e.Name, `/\:*?"<>|`+"\x00") {
		return fmt.Errorf("%w: name %q contains a path or wildcard character", ErrInvalidEntry, e.Name)
	}
	if strings.TrimSpace(e.Exec) == "" {
		return fmt.Errorf("%w: exec path is empty", ErrInvalidEntry)
	}
	return nil
}

// Enable registers the entry to run at login. It is idempotent: enabling an
// already-registered entry rewrites it, which is what makes it safe to call
// after an upgrade that moved the executable.
func Enable(e Entry) error {
	if err := e.validate(); err != nil {
		return err
	}
	return enable(e)
}

// Disable removes the registration. Removing one that does not exist is not an
// error, so callers do not have to check first.
func Disable(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: name is empty", ErrInvalidEntry)
	}
	return disable(name)
}

// IsEnabled reports whether the entry is currently registered.
func IsEnabled(name string) (bool, error) {
	if strings.TrimSpace(name) == "" {
		return false, fmt.Errorf("%w: name is empty", ErrInvalidEntry)
	}
	return isEnabled(name)
}

// Location describes where the registration lives, for diagnostics and so a
// user can remove it by hand if they prefer.
func Location(name string) string { return location(name) }

// Mechanism names the platform facility in use, for diagnostics.
func Mechanism() string { return mechanism() }
