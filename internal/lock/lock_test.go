package lock

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireAndRelease(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ccm.lock")

	l, err := Acquire(p, "switch")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("lock file should exist: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("lock file should be gone after Release")
	}
}

func TestSecondAcquireIsRefused(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ccm.lock")

	l, err := Acquire(p, "switch to work")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()

	_, err = Acquire(p, "switch to personal")
	if !errors.Is(err, ErrHeld) {
		t.Fatalf("expected ErrHeld, got %v", err)
	}
	// The message must name the holder so a stuck lock is diagnosable.
	if err != nil && !contains(err.Error(), "switch to work") {
		t.Errorf("error should describe the holding operation, got %q", err)
	}
}

func TestStaleLockIsBroken(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ccm.lock")

	stale, _ := json.Marshal(holder{
		PID:       999999,
		Host:      "gone",
		Acquired:  time.Now().Add(-2 * StaleAfter),
		Operation: "abandoned",
	})
	if err := os.WriteFile(p, stale, 0o600); err != nil {
		t.Fatal(err)
	}

	l, err := Acquire(p, "switch")
	if err != nil {
		t.Fatalf("a lock older than StaleAfter should be broken, got %v", err)
	}
	defer l.Release()
}

// An unreadable lock file has unknown age. Treating it as free would license a
// concurrent write, so it must be treated as held.
func TestCorruptLockIsTreatedAsHeld(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ccm.lock")
	if err := os.WriteFile(p, []byte("not json at all"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Acquire(p, "switch"); !errors.Is(err, ErrHeld) {
		t.Fatalf("expected ErrHeld for a corrupt lock, got %v", err)
	}
}

func TestReleaseOnNilAndMissingIsSafe(t *testing.T) {
	var l *Lock
	if err := l.Release(); err != nil {
		t.Errorf("releasing a nil lock should be a no-op, got %v", err)
	}

	p := filepath.Join(t.TempDir(), "ccm.lock")
	l2, err := Acquire(p, "switch")
	if err != nil {
		t.Fatal(err)
	}
	if err := l2.Release(); err != nil {
		t.Fatal(err)
	}
	if err := l2.Release(); err != nil {
		t.Errorf("double release should be a no-op, got %v", err)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
