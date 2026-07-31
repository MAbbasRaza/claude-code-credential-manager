package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/MAbbasRaza/claude-code-multi-account-manager/internal/shortcut"
)

// parseKinds turns the optional positional argument into the kinds to act on.
//
// No argument means every kind this platform has, which is what makes
// `ccm shortcut add` do the obvious thing rather than needing the user to know
// that macOS has no application menu.
func parseKinds(args []string) ([]shortcut.Kind, error) {
	if len(args) == 0 || args[0] == "all" {
		var out []shortcut.Kind
		for _, k := range shortcut.Kinds {
			if shortcut.Supported(k) {
				out = append(out, k)
			}
		}
		return out, nil
	}

	switch strings.ToLower(args[0]) {
	case "desktop":
		return []shortcut.Kind{shortcut.Desktop}, nil
	case "menu", "start-menu", "startmenu", "applications":
		return []shortcut.Kind{shortcut.Menu}, nil
	default:
		return nil, fmt.Errorf("unknown shortcut kind %q (valid: desktop, menu, all)", args[0])
	}
}

func cmdShortcut(g globalOpts, args []string) error {
	action := "status"
	if len(args) > 0 {
		action = args[0]
		args = args[1:]
	}

	switch action {
	case "status":
		return shortcutStatus(g)
	case "add", "create", "enable":
		return shortcutSet(g, args, true)
	case "remove", "rm", "delete", "disable":
		return shortcutSet(g, args, false)
	default:
		return fmt.Errorf("unknown shortcut action %q (valid: status, add, remove)", action)
	}
}

func shortcutSet(g globalOpts, args []string, want bool) error {
	kinds, err := parseKinds(args)
	if err != nil {
		return err
	}
	if len(kinds) == 0 {
		return errors.New("this platform has no shortcut locations")
	}

	results := map[string]any{}
	for _, k := range kinds {
		if want {
			// Resolved per kind: the application menu carries the tray app as
			// well, the desktop does not.
			entries, err := shortcut.EntriesFor(k)
			if err != nil {
				return err
			}
			for _, e := range entries {
				if err := shortcut.Add(k, e); err != nil {
					return fmt.Errorf("%s shortcut: %w", k, err)
				}
				if !g.jsonOut {
					fmt.Printf("Added %s\n", shortcut.Location(k, e.Name))
				}
			}
		} else {
			// Never resolves the programs: an uninstall has to clean up
			// shortcuts after the executables they point at are already gone.
			for _, name := range shortcut.NamesFor(k) {
				if err := shortcut.Remove(k, name); err != nil {
					return fmt.Errorf("%s shortcut: %w", k, err)
				}
			}
			if !g.jsonOut {
				fmt.Printf("Removed the %s shortcut\n", k)
			}
		}
		results[string(k)] = shortcut.Location(k, shortcut.AppName)
	}

	if g.jsonOut {
		return emitJSON(map[string]any{"added": want, "locations": results})
	}
	return nil
}

func shortcutStatus(g globalOpts) error {
	type kindStatus struct {
		Kind      string `json:"kind"`
		Supported bool   `json:"supported"`
		Present   bool   `json:"present"`
		Location  string `json:"location,omitempty"`
		Mechanism string `json:"mechanism,omitempty"`
	}

	_, appErr := shortcut.ForDesktopApp()
	var out []kindStatus
	for _, k := range shortcut.Kinds {
		s := kindStatus{Kind: string(k), Supported: shortcut.Supported(k)}
		if s.Supported {
			on, err := shortcut.Exists(k, shortcut.AppName)
			if err != nil {
				return err
			}
			s.Present = on
			s.Location = shortcut.Location(k, shortcut.AppName)
			s.Mechanism = shortcut.Describe(k)
		}
		out = append(out, s)
	}

	if g.jsonOut {
		return emitJSON(map[string]any{
			"shortcuts":     out,
			"desktopAppOK":  appErr == nil,
			"desktopAppErr": errString(appErr),
		})
	}

	for _, s := range out {
		if !s.Supported {
			fmt.Printf("%-8s not applicable on this platform\n", s.Kind+":")
			continue
		}
		state := "not created"
		if s.Present {
			state = "created"
		}
		fmt.Printf("%-8s %s\n", s.Kind+":", state)
		fmt.Printf("  where   %s\n", s.Location)
		fmt.Printf("  as      %s\n", s.Mechanism)
	}
	if appErr != nil {
		fmt.Println()
		fmt.Println(appErr)
	}
	return nil
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
