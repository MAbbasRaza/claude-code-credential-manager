//go:build windows

package autostart

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// runKey is the per-user autostart key. HKCU rather than HKLM: no elevation,
// and the vault is bound to this user anyway.
const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func mechanism() string { return `HKCU\` + runKey }

func location(name string) string { return `HKCU\` + runKey + ` -> ` + name }

func enable(e Entry) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open %s: %w", mechanism(), err)
	}
	defer k.Close()

	if err := k.SetStringValue(e.Name, commandLine(e)); err != nil {
		return fmt.Errorf("write autostart value %q: %w", e.Name, err)
	}
	return nil
}

func disable(name string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return nil
		}
		return fmt.Errorf("open %s: %w", mechanism(), err)
	}
	defer k.Close()

	// Deleting an absent value is not a failure; the caller wanted it gone.
	if err := k.DeleteValue(name); err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("remove autostart value %q: %w", name, err)
	}
	return nil
}

func isEnabled(name string) (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return false, nil
		}
		return false, fmt.Errorf("open %s: %w", mechanism(), err)
	}
	defer k.Close()

	v, _, err := k.GetStringValue(name)
	if err != nil {
		if err == registry.ErrNotExist {
			return false, nil
		}
		return false, fmt.Errorf("read autostart value %q: %w", name, err)
	}
	return strings.TrimSpace(v) != "", nil
}

// commandLine quotes the executable so a path containing spaces, which
// %LOCALAPPDATA%\Programs\... often does under a user profile with a space in
// it, is not split into separate arguments by the shell that runs it.
func commandLine(e Entry) string {
	var b strings.Builder
	b.WriteString(quote(e.Exec))
	for _, a := range e.Args {
		b.WriteByte(' ')
		b.WriteString(quote(a))
	}
	return b.String()
}

func quote(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, ` "`) {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
