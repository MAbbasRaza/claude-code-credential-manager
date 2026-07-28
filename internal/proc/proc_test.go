package proc

import (
	"os"
	"testing"
)

// This package is what stops a switch happening under a live Claude Code
// session. A false negative here is expensive: the switch appears to succeed,
// then the running session rewrites its configuration on exit and silently
// undoes it.

func TestIsClaudeMatchesRealProcessNames(t *testing.T) {
	match := []string{
		"claude",
		"claude.exe",
		"CLAUDE.EXE", // Windows process names are not case-sensitive
		"Claude",
		"  claude  ", // ps output can carry padding
	}
	for _, n := range match {
		if !isClaude(n) {
			t.Errorf("isClaude(%q) = false, want true", n)
		}
	}
}

// Over-matching is its own failure: refusing to switch because an unrelated
// process happens to contain "claude" would block the user with no way to tell
// why, and the message names PIDs that look wrong.
func TestIsClaudeRejectsUnrelatedNames(t *testing.T) {
	reject := []string{
		"",
		"   ",
		"claudia",
		"claude-code-helper",
		"myclaude",
		"claude.exe.bak",
		"ccm",
		"ccm-tray.exe",
		"ccm-gui.exe",
		"code.exe",
		"node",
	}
	for _, n := range reject {
		if isClaude(n) {
			t.Errorf("isClaude(%q) = true, want false", n)
		}
	}
}

// ccm's own binaries must never be mistaken for Claude Code. If ccm-gui counted
// itself, the desktop app could never switch anything.
func TestCcmBinariesAreNotClaude(t *testing.T) {
	for _, n := range []string{"ccm", "ccm.exe", "ccm-tray", "ccm-tray.exe", "ccm-gui", "ccm-gui.exe"} {
		if isClaude(n) {
			t.Errorf("%q was treated as Claude Code; ccm would block itself", n)
		}
	}
}

// FindClaude must return a usable answer on whatever platform the suite runs
// on. Callers treat an error as "cannot tell" and refuse to switch, so an
// enumeration that always failed would make the tool unusable.
func TestFindClaudeEnumeratesWithoutError(t *testing.T) {
	procs, err := FindClaude()
	if err != nil {
		t.Fatalf("process enumeration failed on this platform: %v", err)
	}
	for _, p := range procs {
		if p.PID <= 0 {
			t.Errorf("process reported with a non-positive PID: %+v", p)
		}
		if !isClaude(p.Name) {
			t.Errorf("FindClaude returned %q, which isClaude rejects", p.Name)
		}
	}
	t.Logf("found %d Claude Code process(es) on this machine", len(procs))
}

// Enumeration must not report the test binary itself, which proves the filter
// is actually applied rather than everything being returned.
func TestFindClaudeExcludesThisProcess(t *testing.T) {
	procs, err := FindClaude()
	if err != nil {
		t.Fatal(err)
	}
	self := os.Getpid()
	for _, p := range procs {
		if p.PID == self {
			t.Fatalf("FindClaude returned the test process itself (pid %d)", self)
		}
	}
}
