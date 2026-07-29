//go:build linux

package autostart

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func mechanism() string { return "XDG autostart (~/.config/autostart)" }

// autostartDir follows the XDG base directory spec, which every mainstream
// desktop environment reads at login.
func autostartDir() (string, error) {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "autostart"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "autostart"), nil
}

func entryPath(name string) (string, error) {
	dir, err := autostartDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".desktop"), nil
}

func location(name string) string {
	p, err := entryPath(name)
	if err != nil {
		return "~/.config/autostart/" + name + ".desktop"
	}
	return p
}

func enable(e Entry) error {
	path, err := entryPath(e.Name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create autostart directory: %w", err)
	}

	display := e.DisplayName
	if display == "" {
		display = e.Name
	}

	// X-GNOME-Autostart-enabled is honoured by GNOME and ignored elsewhere.
	// Writing it explicitly means a desktop that had previously disabled this
	// entry through its own UI does not silently keep it off after a re-enable.
	body := "[Desktop Entry]\n" +
		"Type=Application\n" +
		"Name=" + escapeValue(display) + "\n" +
		"Exec=" + execLine(e) + "\n" +
		"Terminal=false\n" +
		"X-GNOME-Autostart-enabled=true\n" +
		"Comment=Switch between Claude Code accounts\n"

	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func disable(name string) error {
	path, err := entryPath(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

func isEnabled(name string) (bool, error) {
	path, err := entryPath(name)
	if err != nil {
		return false, err
	}
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}

	// A desktop environment turning the entry off rewrites this key rather than
	// deleting the file, so the file existing is not the same as it being on.
	for _, line := range strings.Split(string(body), "\n") {
		if strings.EqualFold(strings.TrimSpace(line), "X-GNOME-Autostart-enabled=false") {
			return false, nil
		}
		if strings.EqualFold(strings.TrimSpace(line), "Hidden=true") {
			return false, nil
		}
	}
	return true, nil
}

// execLine builds the Exec value. The desktop entry spec reserves several
// characters, and a path under a home directory with a space in it is common
// enough that quoting is not optional.
func execLine(e Entry) string {
	parts := make([]string, 0, len(e.Args)+1)
	parts = append(parts, quoteArg(e.Exec))
	for _, a := range e.Args {
		parts = append(parts, quoteArg(a))
	}
	return strings.Join(parts, " ")
}

func quoteArg(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"'\\><~|&;$*?#()`") {
		return s
	}
	r := strings.NewReplacer(`\`, `\\\\`, `"`, `\"`, "`", "\\`", `$`, `\$`)
	return `"` + r.Replace(s) + `"`
}

// escapeValue escapes the characters the desktop entry spec treats specially
// inside a plain string value.
func escapeValue(s string) string {
	r := strings.NewReplacer("\\", `\\`, "\n", `\n`, "\t", `\t`, "\r", `\r`)
	return r.Replace(s)
}
