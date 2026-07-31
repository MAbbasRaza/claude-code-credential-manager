// Command ccm-tray puts account switching in the system tray.
//
// It links the same internal packages as the CLI rather than shelling out, so
// there is exactly one implementation of the switch algorithm and one set of
// safety checks.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"fyne.io/systray"

	"github.com/MAbbasRaza/claude-code-multi-account-manager/internal/autostart"
	"github.com/MAbbasRaza/claude-code-multi-account-manager/internal/icon"
	"github.com/MAbbasRaza/claude-code-multi-account-manager/internal/locate"
	"github.com/MAbbasRaza/claude-code-multi-account-manager/internal/manager"
)

var version = "dev"

func main() {
	systray.Run(onReady, func() {})
}

func onReady() {
	systray.SetIcon(icon.Data(runtime.GOOS))
	systray.SetTitle("ccm")
	systray.SetTooltip("Claude Code account switcher")

	// Left click opens the desktop app, right click keeps the menu, which is
	// what a Windows user expects from a tray icon.
	//
	// Only wired when the app is actually installed. SetOnTapped replaces the
	// default left-click behaviour rather than adding to it, and the function
	// that shows the menu is unexported, so setting this without the app
	// present would leave left click doing nothing at all.
	if guiPath() != "" {
		systray.SetOnTapped(openGUI)
		systray.SetTooltip("Claude Code account switcher\nClick to manage accounts")
	}

	rebuild()
}

// rebuildMu serializes menu rebuilds.
//
// rebuild is reached from every profile-click goroutine plus the Refresh and
// Capture handlers, so without this two rebuilds can interleave. systray's
// ResetMenu snapshots the item map and closes each removed item's channel, and
// two overlapping snapshots can both reach the same item, panicking with
// "close of closed channel".
var rebuildMu sync.Mutex

// rebuild tears down and re-creates the menu.
//
// systray has no way to remove items once added, so switching accounts marks
// the whole menu stale and the app rebuilds it by resetting the tray. Keeping
// this in one place avoids a menu that drifts out of sync with the vault.
func rebuild() {
	rebuildMu.Lock()
	defer rebuildMu.Unlock()

	systray.ResetMenu()

	m, err := manager.Open("")
	if err != nil {
		addDisabled(fmt.Sprintf("Error: %v", err))
		addQuit()
		return
	}

	st, err := m.Status()
	if err != nil {
		addDisabled(fmt.Sprintf("Error: %v", err))
		addQuit()
		return
	}

	header := "Not signed in"
	if st.LoggedIn {
		header = "Active: " + orUnknown(st.EmailAddress)
	}
	addDisabled(header)
	addDisabled("Config: " + shorten(st.ConfigDir))
	systray.AddSeparator()

	profiles := m.Vault.List()
	if len(profiles) == 0 {
		addDisabled("No profiles yet")
		addDisabled("Run: ccm add <name>")
	}

	for _, p := range profiles {
		label := p.Name
		if p.EmailAddress != "" {
			label += "  (" + p.EmailAddress + ")"
		}
		item := systray.AddMenuItemCheckbox(label, switchTooltip(p.Name), p.AccountUUID == st.AccountUUID)
		if p.AccountUUID == st.AccountUUID {
			item.Disable()
		}
		go watch(item, p.Name)
	}

	systray.AddSeparator()

	// A tray menu cannot take text input, so renaming and the rest of the
	// management surface live in the desktop app. Offer it only when it is
	// actually installed, rather than showing an entry that fails on click.
	if guiPath() != "" {
		manage := systray.AddMenuItem("Manage accounts…", "Open the desktop app")
		go onClick(manage.ClickedCh, openGUI)
	}

	// A checkbox rather than a plain item, so the current state is visible
	// without opening anything else.
	//
	// The path comes from locate rather than os.Executable so this registers
	// exactly what `ccm autostart enable` would. They differ when the tray was
	// started through a symlink, which is how the macOS package installs it:
	// the raw value would record the link, and the two surfaces would then
	// disagree about what is registered.
	if self := locate.Executable(locate.Tray); self != "" {
		on, _ := autostart.IsEnabled(autostartName)
		boot := systray.AddMenuItemCheckbox("Start at login", "Run the tray app when you log in", on)
		go onClick(boot.ClickedCh, func() { toggleAutostart(self, boot) })
	}

	refresh := systray.AddMenuItem("Refresh", "Re-read profiles and active account")
	go onClick(refresh.ClickedCh, rebuild)

	capture := systray.AddMenuItem("Capture current login…", "Save the signed-in account as a new profile")
	go onClick(capture.ClickedCh, capturePrompt)

	addQuit()
}

func switchTooltip(name string) string {
	return "Switch to " + name + " (restart Claude Code afterwards)"
}

// watch handles clicks on one profile entry.
func watch(item *systray.MenuItem, name string) {
	onClick(item.ClickedCh, func() {
		mgr, err := manager.Open("")
		if err != nil {
			notify("ccm", "Error: "+err.Error())
			return
		}
		res, err := mgr.Switch(name, false)
		if err != nil {
			// The running-Claude-Code guard lands here, and its message is the
			// actionable part, so it is surfaced verbatim.
			notify("ccm could not switch", firstLine(err.Error()))
			return
		}
		msg := "Switched to " + res.To
		if res.ToEmail != "" {
			msg += " (" + res.ToEmail + ")"
		}
		notify(msg, "Restart Claude Code for it to take effect.")
		rebuild()
	})
}

// capturePrompt saves the live login under an auto-derived name. The tray has
// no text input, so naming is left to the CLI.
func capturePrompt() {
	mgr, err := manager.Open("")
	if err != nil {
		notify("ccm", "Error: "+err.Error())
		return
	}
	p, err := mgr.Capture("")
	if err != nil {
		notify("ccm could not capture", firstLine(err.Error()))
		return
	}
	notify("Captured "+p.Name, p.EmailAddress)
	rebuild()
}

// guiPath locates the desktop app, returning empty when it is not installed.
//
// Delegates to internal/locate rather than looking itself. The copy that used
// to live here had two defects the shared implementation does not: it never
// resolved symlinks, so a tray started through a symlinked entry point searched
// the link's directory instead of the real one, and it accepted any directory
// entry, so a directory named ccm-gui was handed to exec and failed at launch
// with a message the user could do nothing about. It also could not see across
// the two application bundles the macOS package installs.
func guiPath() string {
	return locate.Executable(locate.GUI)
}

func openGUI() {
	path := guiPath()
	if path == "" {
		notify("ccm", "The desktop app is not installed alongside ccm-tray.")
		return
	}
	if err := exec.Command(path).Start(); err != nil {
		notify("ccm could not open the manager", firstLine(err.Error()))
	}
}

// autostartName must match what the CLI and the desktop app use, or each would
// believe the others had not set it.
const autostartName = "ccm-tray"

// toggleAutostart flips start-at-login and reports the outcome.
//
// The checkbox is set from the mechanism's own state afterwards rather than
// assumed, so a write that failed leaves the tick where it really is instead of
// where the click implied.
func toggleAutostart(self string, item *systray.MenuItem) {
	on, err := autostart.IsEnabled(autostartName)
	if err != nil {
		notify("ccm", "Could not read the login setting: "+firstLine(err.Error()))
		return
	}

	if on {
		err = autostart.Disable(autostartName)
	} else {
		err = autostart.Enable(autostart.Entry{
			Name:        autostartName,
			DisplayName: "Claude Code Accounts",
			Exec:        self,
		})
	}
	if err != nil {
		notify("ccm could not change the login setting", firstLine(err.Error()))
	}

	if nowOn, e := autostart.IsEnabled(autostartName); e == nil {
		if nowOn {
			item.Check()
			notify("Start at login enabled", "The tray app will start when you log in.")
		} else {
			item.Uncheck()
			notify("Start at login disabled", "The tray app will no longer start automatically.")
		}
	}
}

func addDisabled(label string) {
	systray.AddMenuItem(label, "").Disable()
}

func addQuit() {
	q := systray.AddMenuItem("Quit", "Exit the tray app")
	// Must go through onClick. A bare receive here treated ResetMenu's channel
	// close as a click and quit the app on the first rebuild after a switch.
	go onClick(q.ClickedCh, systray.Quit)
}

func orUnknown(s string) string {
	if s == "" {
		return "(unknown)"
	}
	return s
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func shorten(p string) string {
	const max = 44
	if len(p) <= max {
		return p
	}
	return "…" + p[len(p)-max+1:]
}

// notify shows a desktop notification, falling back to stderr where no
// notifier is available. Tray menus give no room for multi-line errors, and
// silently swallowing a failed switch is the worst outcome here.
func notify(title, body string) {
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf("display notification %q with title %q", body, title)
		if err := exec.Command("osascript", "-e", script).Run(); err == nil {
			return
		}
	case "linux":
		if err := exec.Command("notify-send", title, body).Run(); err == nil {
			return
		}
	case "windows":
		ps := fmt.Sprintf(
			`[reflection.assembly]::LoadWithPartialName('System.Windows.Forms')>$null;`+
				`$n=New-Object System.Windows.Forms.NotifyIcon;`+
				`$n.Icon=[System.Drawing.SystemIcons]::Information;$n.Visible=$true;`+
				`$n.ShowBalloonTip(5000,%q,%q,'Info');Start-Sleep -Seconds 5;$n.Dispose()`,
			title, body)
		if err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps).Start(); err == nil {
			return
		}
	}
	fmt.Fprintf(os.Stderr, "%s: %s\n", title, body)
}
