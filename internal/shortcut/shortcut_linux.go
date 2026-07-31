//go:build linux

package shortcut

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func supported(Kind) bool { return true }

func describe(k Kind) string {
	if k == Desktop {
		return "a .desktop file on your desktop"
	}
	return "a .desktop file in ~/.local/share/applications"
}

// desktopDir finds the user's desktop directory.
//
// XDG_DESKTOP_DIR is set from ~/.config/user-dirs.dirs by most desktop
// environments and is the only reliable source on a localised system, where
// the directory is named "Escritorio" or "Bureau" rather than "Desktop".
// Falling straight back to ~/Desktop would create a second, English-named
// folder the user never sees.
func desktopDir() (string, error) {
	if d := os.Getenv("XDG_DESKTOP_DIR"); d != "" {
		return expandHome(d)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	// user-dirs.dirs is shell syntax, but the one line needed from it is
	// simple enough to read directly rather than running a shell.
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		cfg = filepath.Join(home, ".config")
	}
	if b, err := os.ReadFile(filepath.Join(cfg, "user-dirs.dirs")); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "XDG_DESKTOP_DIR=") {
				continue
			}
			v := strings.Trim(strings.TrimPrefix(line, "XDG_DESKTOP_DIR="), `"`)
			if v != "" {
				return expandHome(v)
			}
		}
	}
	return filepath.Join(home, "Desktop"), nil
}

func expandHome(p string) (string, error) {
	if !strings.HasPrefix(p, "$HOME") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, strings.TrimPrefix(p, "$HOME")), nil
}

func menuDir() (string, error) {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "applications"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "applications"), nil
}

func kindDir(k Kind) (string, error) {
	if k == Desktop {
		return desktopDir()
	}
	return menuDir()
}

func location(k Kind, name string) string {
	dir, err := kindDir(k)
	if err != nil {
		return ""
	}
	return filepath.Join(dir, name+".desktop")
}

func exists(k Kind, name string) (bool, error) {
	p := location(k, name)
	if p == "" {
		return false, nil
	}
	info, err := os.Stat(p)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !info.IsDir(), nil
}

func add(k Kind, e Entry) error {
	dir, err := kindDir(k)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	path := location(k, e.Name)

	icon := e.Icon
	if icon == "" {
		icon = "ccm"
	}

	cmdline := quoteExec(e.Target)
	for _, a := range e.Args {
		cmdline += " " + quoteExec(a)
	}

	var b strings.Builder
	b.WriteString("[Desktop Entry]\n")
	b.WriteString("Type=Application\n")
	b.WriteString("Name=" + escapeValue(e.displayName()) + "\n")
	if e.Description != "" {
		b.WriteString("Comment=" + escapeValue(e.Description) + "\n")
	}
	b.WriteString("Exec=" + cmdline + "\n")
	b.WriteString("Icon=" + escapeValue(icon) + "\n")
	b.WriteString("Terminal=false\n")
	b.WriteString("Categories=Development;\n")
	b.WriteString("StartupNotify=true\n")

	// 0755, not 0644. GNOME and KDE both refuse to launch a desktop-directory
	// entry that is not executable, showing it as an untrusted text file
	// instead. The applications directory does not care, but there is no
	// reason for the two to differ.
	if err := os.WriteFile(path, []byte(b.String()), 0o755); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	if k == Desktop {
		// GNOME additionally requires the entry be marked trusted before it
		// will run it. Best effort: gio is absent on plenty of systems, and on
		// KDE and XFCE the executable bit alone is enough.
		markTrusted(path)
	}
	if k == Menu {
		refreshMenu(dir)
	}
	return nil
}

func markTrusted(path string) {
	if _, err := exec.LookPath("gio"); err != nil {
		return
	}
	_ = exec.Command("gio", "set", path, "metadata::trusted", "true").Run()
}

func refreshMenu(dir string) {
	if _, err := exec.LookPath("update-desktop-database"); err != nil {
		return
	}
	_ = exec.Command("update-desktop-database", "-q", dir).Run()
}

// escapeValue escapes a plain string value: Name, Comment, Icon.
//
// Deliberately does not quote. The Desktop Entry specification only gives
// quoting a meaning inside Exec, where the value is parsed into arguments;
// everywhere else a quote is just a character, so wrapping a name that happens
// to contain a space puts visible quotation marks in the user's application
// menu. desktop-file-validate accepts that happily, because quotes are legal
// there, which is why it has to be got right rather than merely checked.
func escapeValue(s string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\t", `\t`, "\r", `\r`).Replace(s)
}

// quoteExec escapes and, where necessary, quotes one word of an Exec line.
// Without the quoting a target path containing a space would be read as a
// command plus an argument.
func quoteExec(s string) string {
	s = escapeValue(s)
	// The specification reserves these inside Exec.
	if strings.ContainsAny(s, " \t\n\"'\\><~|&;$*?#()`") {
		s = `"` + strings.NewReplacer(`"`, `\"`, "`", "\\`", `$`, `\$`).Replace(s) + `"`
	}
	return s
}

func remove(k Kind, name string) error {
	p := location(k, name)
	if p == "" {
		return nil
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", p, err)
	}
	if k == Menu {
		if dir, err := kindDir(k); err == nil {
			refreshMenu(dir)
		}
	}
	return nil
}
