//go:build darwin

package autostart

import (
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// labelPrefix namespaces the LaunchAgent so it cannot collide with another
// program's job. launchd requires labels to be unique per user session.
const labelPrefix = "com.mabbasraza.ccm."

func mechanism() string { return "launchd LaunchAgent (~/Library/LaunchAgents)" }

func agentPath(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", labelPrefix+name+".plist"), nil
}

func location(name string) string {
	p, err := agentPath(name)
	if err != nil {
		return "~/Library/LaunchAgents/" + labelPrefix + name + ".plist"
	}
	return p
}

func enable(e Entry) error {
	path, err := agentPath(e.Name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create LaunchAgents directory: %w", err)
	}

	label := labelPrefix + e.Name
	args := append([]string{e.Exec}, e.Args...)

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">` + "\n<dict>\n")
	b.WriteString("  <key>Label</key>\n  <string>" + escapeXML(label) + "</string>\n")
	b.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	for _, a := range args {
		b.WriteString("    <string>" + escapeXML(a) + "</string>\n")
	}
	b.WriteString("  </array>\n")
	b.WriteString("  <key>RunAtLoad</key>\n  <true/>\n")
	// Deliberately not KeepAlive. The tray is meant to be quittable from its
	// own menu; relaunching it against the user's wishes would be hostile.
	b.WriteString("  <key>KeepAlive</key>\n  <false/>\n")
	b.WriteString("</dict>\n</plist>\n")

	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	// Best effort: the plist alone takes effect at next login, but loading it
	// now means the user sees the result immediately. A failure here is not
	// worth failing the whole operation over.
	_ = exec.Command("launchctl", "load", "-w", path).Run()
	return nil
}

func disable(name string) error {
	path, err := agentPath(name)
	if err != nil {
		return err
	}
	// Unload before removing, otherwise launchd keeps the job registered for
	// the rest of the session and the change appears not to have worked.
	_ = exec.Command("launchctl", "unload", "-w", path).Run()

	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

func isEnabled(name string) (bool, error) {
	path, err := agentPath(name)
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
	// A plist with RunAtLoad false is registered but will not start, which is
	// not what the user asked for when they enabled it.
	return strings.Contains(string(body), "<key>RunAtLoad</key>") &&
		!strings.Contains(collapse(string(body)), "<key>RunAtLoad</key><false/>"), nil
}

func collapse(s string) string {
	return strings.Join(strings.Fields(s), "")
}

func escapeXML(s string) string {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		return s
	}
	return b.String()
}
