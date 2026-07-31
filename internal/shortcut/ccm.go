package shortcut

import (
	"errors"
	"runtime"

	"github.com/MAbbasRaza/claude-code-multi-account-manager/internal/locate"
)

// AppName is the shortcut's filename and what the user sees.
//
// Defined once and imported by every surface that creates or removes one. The
// autostart entry name is instead a literal repeated in three packages with a
// comment asking that they match, which is a standing invitation for them to
// stop matching; there is no reason to repeat that here.
const AppName = "Claude Code Accounts"

// TrayName is the tray app's entry. It belongs only in the application menu:
// two icons for one tool on the desktop is clutter, but a user who declined
// start-at-login still needs some way to launch the tray by hand.
const TrayName = "Claude Code Accounts (tray)"

// ErrNoDesktopApp reports that the desktop app is not installed, so there is
// nothing for a shortcut to open.
var ErrNoDesktopApp = errors.New("the desktop app is not installed, so a shortcut would have nothing to open")

// ForDesktopApp builds the entry for ccm's desktop application.
//
// The desktop app is what a shortcut should open, not the CLI, which needs a
// terminal, and not the tray, which is started at login instead and would put a
// second icon in the notification area every time it was clicked.
func ForDesktopApp() (Entry, error) {
	target := locate.Executable(locate.GUI)
	if target == "" {
		return Entry{}, ErrNoDesktopApp
	}

	// On macOS point at the bundle rather than the binary inside it, so the
	// link opens through LaunchServices and the application gets its icon and
	// its name. locate.AppBundle is empty when ccm was installed as a loose
	// binary rather than from the package, in which case the executable is all
	// there is to point at.
	if runtime.GOOS == "darwin" {
		if bundle := locate.AppBundle(locate.GUI); bundle != "" {
			target = bundle
		}
	}

	return Entry{
		Name:        AppName,
		DisplayName: AppName,
		Target:      target,
		Description: "Switch between Claude Code accounts without signing in again",
		Icon:        "ccm",
	}, nil
}

// ForTrayApp builds the entry for the tray app, or reports that it is not
// installed. Callers are expected to skip it in that case rather than fail: the
// tray is optional, and a menu entry for it is optional in turn.
func ForTrayApp() (Entry, bool) {
	target := locate.Executable(locate.Tray)
	if target == "" {
		return Entry{}, false
	}
	if runtime.GOOS == "darwin" {
		if bundle := locate.AppBundle(locate.Tray); bundle != "" {
			target = bundle
		}
	}
	return Entry{
		Name:        TrayName,
		DisplayName: TrayName,
		Target:      target,
		Description: "Switch accounts from the system tray",
		Icon:        "ccm",
	}, true
}

// EntriesFor lists what a kind should contain.
//
// The application menu carries both programs, which is what a Start Menu group
// is for. The desktop carries only the window: a second desktop icon for a tray
// applet is clutter nobody asked for.
func EntriesFor(k Kind) ([]Entry, error) {
	app, err := ForDesktopApp()
	if err != nil {
		return nil, err
	}
	out := []Entry{app}
	if k == Menu {
		if tray, ok := ForTrayApp(); ok {
			out = append(out, tray)
		}
	}
	return out, nil
}

// NamesFor lists the names a kind may have created, for removal. Unlike
// EntriesFor this never fails, because an uninstall has to clean up shortcuts
// after the programs they point at are already gone.
func NamesFor(k Kind) []string {
	if k == Menu {
		return []string{AppName, TrayName}
	}
	return []string{AppName}
}
