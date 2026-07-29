package main

import (
	"errors"
	"fmt"

	"github.com/MAbbasRaza/claude-code-multi-account-manager/internal/autostart"
	"github.com/MAbbasRaza/claude-code-multi-account-manager/internal/locate"
)

// trayEntry describes what gets registered to run at login.
//
// The tray is the right thing to start, not the CLI or the desktop app: it is
// the only one designed to sit resident, and it is what makes switching
// available without opening anything first.
func trayEntry() (autostart.Entry, error) {
	exe := locate.Executable(locate.Tray)
	if exe == "" {
		return autostart.Entry{}, errors.New(
			"the tray app is not installed, so there is nothing to start at login.\n" +
				"Install it alongside the CLI:\n" +
				"  Windows       install.ps1 -Tray\n" +
				"  macOS, Linux  CCM_TRAY=1 ... install.sh")
	}
	return autostart.Entry{
		Name:        "ccm-tray",
		DisplayName: "Claude Code Accounts",
		Exec:        exe,
	}, nil
}

func cmdAutostart(g globalOpts, args []string) error {
	action := "status"
	if len(args) > 0 {
		action = args[0]
	}

	switch action {
	case "status":
		return autostartStatus(g)

	case "enable", "on":
		e, err := trayEntry()
		if err != nil {
			return err
		}
		if err := autostart.Enable(e); err != nil {
			return err
		}
		if g.jsonOut {
			return emitJSON(map[string]any{"enabled": true, "exec": e.Exec, "location": autostart.Location(e.Name)})
		}
		fmt.Printf("Enabled. %s will start when you log in.\n", e.Exec)
		fmt.Printf("Registered at %s\n", autostart.Location(e.Name))
		return nil

	case "disable", "off":
		if err := autostart.Disable("ccm-tray"); err != nil {
			return err
		}
		if g.jsonOut {
			return emitJSON(map[string]any{"enabled": false})
		}
		fmt.Println("Disabled. The tray app will no longer start at login.")
		return nil

	default:
		return fmt.Errorf("unknown autostart action %q (valid: status, enable, disable)", action)
	}
}

func autostartStatus(g globalOpts) error {
	on, err := autostart.IsEnabled("ccm-tray")
	if err != nil {
		return err
	}
	exe := locate.Executable(locate.Tray)

	if g.jsonOut {
		return emitJSON(map[string]any{
			"enabled":   on,
			"mechanism": autostart.Mechanism(),
			"location":  autostart.Location("ccm-tray"),
			"trayFound": exe != "",
			"exec":      exe,
		})
	}

	if on {
		fmt.Println("Start at login: enabled")
	} else {
		fmt.Println("Start at login: disabled")
	}
	fmt.Printf("  mechanism : %s\n", autostart.Mechanism())
	fmt.Printf("  entry     : %s\n", autostart.Location("ccm-tray"))
	if exe == "" {
		fmt.Println("  tray app  : not installed, so enabling would have nothing to run")
	} else {
		fmt.Printf("  tray app  : %s\n", exe)
	}
	return nil
}
