// Package shortcut creates and removes desktop and application-menu entries.
//
// Each platform has one conventional, per-user location for each kind, and this
// uses those rather than anything needing elevation:
//
//	          Desktop                     Menu
//	Windows   %USERPROFILE%\Desktop       Start Menu\Programs
//	macOS     ~/Desktop                   not applicable
//	Linux     ~/Desktop                   ~/.local/share/applications
//
// macOS has no separate application menu: /Applications is the menu, and an app
// installed there already appears in Launchpad and Spotlight. Menu is reported
// unsupported there rather than faked, so callers hide the option instead of
// offering one that does nothing.
//
// This exists for the same reason internal/autostart does. The Windows
// installer could create shortcuts itself, but then the app's own settings
// could not read back or undo what the installer wrote, and the two would drift.
// One implementation, three surfaces: the installer, the CLI and the desktop
// app all go through here.
package shortcut

import (
	"errors"
	"fmt"
	"strings"
)

// Kind is where a shortcut lives.
type Kind string

const (
	// Desktop is the user's desktop.
	Desktop Kind = "desktop"
	// Menu is the application menu: the Start Menu on Windows, the XDG
	// applications directory on Linux.
	Menu Kind = "menu"
)

// Kinds is every kind, in the order a user interface should present them.
var Kinds = []Kind{Desktop, Menu}

var (
	// ErrUnsupported reports a kind this platform has no equivalent for.
	ErrUnsupported = errors.New("not supported on this platform")
	// ErrInvalidEntry reports an entry that cannot be created.
	ErrInvalidEntry = errors.New("invalid shortcut")
)

// Entry describes a shortcut to create.
type Entry struct {
	// Name is the stable identifier: the filename stem. Changing it orphans an
	// existing shortcut rather than updating it.
	Name string

	// DisplayName is what the user sees. Empty falls back to Name.
	DisplayName string

	// Target is the absolute path to launch. On macOS this should be the .app
	// bundle rather than the executable inside it, so the shortcut opens the
	// application rather than running a bare binary with no identity.
	Target string

	// Args are passed on launch. Ignored on macOS, where the shortcut is a
	// symlink to a bundle and carries no arguments.
	Args []string

	// Description is a tooltip or comment.
	Description string

	// Icon names an icon. On Linux this is a theme name such as "ccm"; on
	// Windows it is a path, and empty means the target supplies its own.
	Icon string
}

func (e Entry) displayName() string {
	if strings.TrimSpace(e.DisplayName) != "" {
		return e.DisplayName
	}
	return e.Name
}

func (e Entry) validate() error {
	if strings.TrimSpace(e.Name) == "" {
		return fmt.Errorf("%w: name is empty", ErrInvalidEntry)
	}
	// The name becomes a filename, so anything that could escape the intended
	// directory is refused rather than sanitised. Same rule as autostart.
	if strings.ContainsAny(e.Name, `/\:*?"<>|`+"\x00") {
		return fmt.Errorf("%w: name %q contains a path or wildcard character", ErrInvalidEntry, e.Name)
	}
	if strings.ContainsAny(e.displayName(), `/\:*?"<>|`+"\x00") {
		return fmt.Errorf("%w: display name %q contains a path or wildcard character",
			ErrInvalidEntry, e.displayName())
	}
	if strings.TrimSpace(e.Target) == "" {
		return fmt.Errorf("%w: target is empty", ErrInvalidEntry)
	}
	return nil
}

func validKind(k Kind) error {
	switch k {
	case Desktop, Menu:
		return nil
	default:
		return fmt.Errorf("%w: unknown shortcut kind %q", ErrInvalidEntry, k)
	}
}

// Add creates the shortcut, replacing any existing one with the same name.
//
// Idempotent, like autostart.Enable, so it is safe to call after an upgrade
// that moved the target.
func Add(k Kind, e Entry) error {
	if err := validKind(k); err != nil {
		return err
	}
	if err := e.validate(); err != nil {
		return err
	}
	if !Supported(k) {
		return fmt.Errorf("%s shortcuts are %w", k, ErrUnsupported)
	}
	return add(k, e)
}

// Remove deletes the shortcut. Removing one that is not there is not an error,
// so callers do not have to check first.
func Remove(k Kind, name string) error {
	if err := validKind(k); err != nil {
		return err
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: name is empty", ErrInvalidEntry)
	}
	if !Supported(k) {
		return nil
	}
	return remove(k, name)
}

// Exists reports whether the shortcut is currently present.
func Exists(k Kind, name string) (bool, error) {
	if err := validKind(k); err != nil {
		return false, err
	}
	if strings.TrimSpace(name) == "" {
		return false, fmt.Errorf("%w: name is empty", ErrInvalidEntry)
	}
	if !Supported(k) {
		return false, nil
	}
	return exists(k, name)
}

// Location is where the shortcut lives, for diagnostics and so a user can
// remove it by hand. Empty when the kind is unsupported.
func Location(k Kind, name string) string {
	if validKind(k) != nil || !Supported(k) {
		return ""
	}
	return location(k, name)
}

// Supported reports whether this platform has an equivalent of the kind.
func Supported(k Kind) bool {
	if validKind(k) != nil {
		return false
	}
	return supported(k)
}

// Describe names the facility in use, for diagnostics.
func Describe(k Kind) string {
	if validKind(k) != nil {
		return ""
	}
	return describe(k)
}
