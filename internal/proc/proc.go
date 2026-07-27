// Package proc detects running Claude Code processes.
//
// This is a safety check, not a nicety. Claude Code reads credentials at
// startup and rewrites .claude.json when it exits, so a switch performed
// underneath a live session is both invisible to that session and liable to be
// overwritten by it moments later. Refusing to switch while Claude Code is
// running is the difference between a tool that works and one that
// intermittently loses an account.
package proc

import "strings"

// Process is a running Claude Code process.
type Process struct {
	PID  int
	Name string
}

// names that indicate a running Claude Code CLI or its native build.
var claudeNames = []string{"claude", "claude.exe"}

func isClaude(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, c := range claudeNames {
		if n == c {
			return true
		}
	}
	return false
}

// FindClaude returns every running Claude Code process.
//
// Detection is best effort: a platform where enumeration fails returns the
// error so callers can tell "none running" apart from "could not tell", and
// refuse accordingly rather than assuming it is safe to proceed.
func FindClaude() ([]Process, error) {
	return findClaude()
}
