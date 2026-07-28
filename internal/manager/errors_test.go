package manager

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/MAbbasRaza/claude-code-credential-manager/internal/proc"
)

// This is the trap that made typed errors necessary, pinned so nobody
// reintroduces a substring test.
//
// The detection-failure message contains the words "Claude Code is running".
// A caller classifying refusals with strings.Contains therefore reports a
// broken guard as a live session. The desktop app did exactly that: it showed
// "0 Claude Code processes are running" beside a "Switch anyway" button, on
// precisely the machines where it could not tell what was running.
func TestDetectionFailureMessageContainsTheRunningPhrase(t *testing.T) {
	detect := &ErrDetectionFailed{Err: errors.New("ps: not found")}

	if !strings.Contains(detect.Error(), "Claude Code is running") {
		t.Fatal("the premise of this test no longer holds; if the wording changed, " +
			"re-check that no caller has gone back to matching on message text")
	}

	// The point: despite the shared wording, the types are unambiguous.
	var running *ErrClaudeRunning
	if errors.As(error(detect), &running) {
		t.Error("a detection failure must not classify as ErrClaudeRunning")
	}
	var failed *ErrDetectionFailed
	if !errors.As(error(detect), &failed) {
		t.Error("a detection failure must classify as ErrDetectionFailed")
	}
}

func TestClaudeRunningClassifies(t *testing.T) {
	err := &ErrClaudeRunning{Procs: []proc.Process{
		{PID: 111, Name: "claude.exe"},
		{PID: 222, Name: "claude.exe"},
	}}

	var running *ErrClaudeRunning
	if !errors.As(error(err), &running) {
		t.Fatal("should classify as ErrClaudeRunning")
	}
	var failed *ErrDetectionFailed
	if errors.As(error(err), &failed) {
		t.Error("must not also classify as ErrDetectionFailed")
	}

	if got := running.PIDs(); len(got) != 2 || got[0] != 111 || got[1] != 222 {
		t.Errorf("PIDs() = %v, want [111 222]", got)
	}

	// The message is what the CLI prints, so it must still name the processes
	// and stay actionable.
	msg := err.Error()
	for _, want := range []string{"2 processes", "pid 111", "pid 222", "--force"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message is missing %q:\n%s", want, msg)
		}
	}
}

func TestClaudeRunningSingularWording(t *testing.T) {
	err := &ErrClaudeRunning{Procs: []proc.Process{{PID: 7, Name: "claude"}}}
	if !strings.Contains(err.Error(), "1 process)") {
		t.Errorf("a single process should not be pluralised:\n%s", err.Error())
	}
}

// Callers wrap these before returning, so classification has to survive it.
func TestClassificationSurvivesWrapping(t *testing.T) {
	base := &ErrClaudeRunning{Procs: []proc.Process{{PID: 9}}}
	wrapped := fmt.Errorf("switching to %q: %w", "work", base)

	var running *ErrClaudeRunning
	if !errors.As(wrapped, &running) {
		t.Fatal("wrapping should not hide the type")
	}
	if len(running.Procs) != 1 || running.Procs[0].PID != 9 {
		t.Error("the process list should survive wrapping")
	}
}

func TestDetectionFailedUnwrapsTheCause(t *testing.T) {
	cause := errors.New("permission denied")
	err := &ErrDetectionFailed{Err: cause}
	if !errors.Is(error(err), cause) {
		t.Error("the underlying enumeration error should be reachable with errors.Is")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Error("the cause should appear in the message so the user can act on it")
	}
}

// EnsureClosed is the only producer of these errors; this checks it returns a
// type rather than a bare fmt.Errorf, which is what regressed before.
func TestEnsureClosedReturnsTypedRefusal(t *testing.T) {
	dir := newEnv(t)
	writeLive(t, dir, credsDoc("A", "R"), configDoc("acct-a", "a@example.invalid"))

	ccmHome := homeDirFor(t)
	writeSettings(t, ccmHome, dir, true)

	m, err := Open("")
	if err != nil {
		t.Fatal(err)
	}

	err = m.EnsureClosed(false)
	if err == nil {
		t.Skip("no Claude Code processes running, so there is no refusal to inspect")
	}

	var running *ErrClaudeRunning
	var failed *ErrDetectionFailed
	if !errors.As(err, &running) && !errors.As(err, &failed) {
		t.Fatalf("EnsureClosed returned an untyped error, so callers are back to "+
			"matching on text: %T %v", err, err)
	}

	// force always wins.
	if err := m.EnsureClosed(true); err != nil {
		t.Errorf("force should bypass the guard, got %v", err)
	}
}
