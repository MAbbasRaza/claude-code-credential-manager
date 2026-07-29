package manager

import (
	"fmt"
	"strings"

	"github.com/MAbbasRaza/claude-code-multi-account-manager/internal/proc"
)

// The two reasons a switch can be refused are different in kind, and callers
// have to tell them apart. One means a live session will undo the switch; the
// other means ccm has no idea what is running and is declining rather than
// guessing.
//
// They are separate types because the alternative, matching on message text,
// is actively dangerous here: the detection-failure sentence contains the
// substring "Claude Code is running", so a caller testing for that phrase
// classifies a detection failure as a live session. The desktop app did
// exactly that, showed "0 Claude Code processes are running" and offered a
// one-click override, which is how this was found.

// ErrClaudeRunning reports a refusal because Claude Code is running. It
// carries the processes so a caller offering an override can name them
// instead of polling again, which would fail for a second time on the very
// machines where this matters.
type ErrClaudeRunning struct {
	Procs []proc.Process
}

func (e *ErrClaudeRunning) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Claude Code is running (%d process", len(e.Procs))
	if len(e.Procs) != 1 {
		b.WriteString("es")
	}
	b.WriteString("): ")
	for i, p := range e.Procs {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "pid %d", p.PID)
	}
	b.WriteString(".\nA running session keeps using the old account and rewrites .claude.json when it exits, " +
		"which would undo the switch. Close Claude Code and retry, or pass --force to override.")
	return b.String()
}

// PIDs is a convenience for callers rendering the refusal.
func (e *ErrClaudeRunning) PIDs() []int {
	out := make([]int, 0, len(e.Procs))
	for _, p := range e.Procs {
		out = append(out, p.PID)
	}
	return out
}

// ErrDetectionFailed reports that ccm could not determine whether Claude Code
// is running. It is deliberately not the same as ErrClaudeRunning: the guard
// is inoperative rather than triggered, which is worth telling the user
// plainly instead of inventing a process count.
type ErrDetectionFailed struct {
	Err error
}

func (e *ErrDetectionFailed) Error() string {
	return fmt.Sprintf("could not determine whether Claude Code is running (%v); "+
		"close Claude Code and retry, or pass --force", e.Err)
}

func (e *ErrDetectionFailed) Unwrap() error { return e.Err }
