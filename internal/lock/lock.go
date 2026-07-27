// Package lock provides a single-writer guard across ccm processes.
//
// The CLI, the tray app and the VS Code extension can all trigger a switch.
// Two concurrent switches would interleave reads and writes of the same two
// documents and could park an account under the wrong name, so every mutating
// path takes this lock first.
package lock

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// StaleAfter is how long a lock file may persist before it is treated as
// abandoned. A switch is a sub-second operation; anything older than this is a
// process that died holding the lock.
const StaleAfter = 2 * time.Minute

type holder struct {
	PID       int       `json:"pid"`
	Host      string    `json:"host"`
	Acquired  time.Time `json:"acquired"`
	Operation string    `json:"operation"`
}

// Lock is an acquired advisory lock.
type Lock struct {
	path string
}

// ErrHeld reports that another ccm process holds the lock.
var ErrHeld = errors.New("another ccm process is holding the lock")

// Acquire takes the lock at path, creating parent directories.
func Acquire(path, operation string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			host, _ := os.Hostname()
			b, _ := json.Marshal(holder{
				PID: os.Getpid(), Host: host,
				Acquired: time.Now().UTC(), Operation: operation,
			})
			_, _ = f.Write(b)
			f.Close()
			return &Lock{path: path}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("acquire lock %s: %w", path, err)
		}

		// Held. Break it only when clearly stale, and say so rather than
		// silently stealing it.
		h, age, readErr := inspect(path)
		if readErr == nil && age > StaleAfter {
			if rmErr := os.Remove(path); rmErr != nil {
				return nil, fmt.Errorf("remove stale lock %s: %w", path, rmErr)
			}
			continue
		}
		if readErr != nil {
			// Unreadable lock file of unknown age: treat as held rather than
			// guessing, so a corrupt file cannot license a concurrent write.
			return nil, fmt.Errorf("%w (lock file %s is unreadable: %v)", ErrHeld, path, readErr)
		}
		return nil, fmt.Errorf("%w: pid %d on %s since %s (%s)",
			ErrHeld, h.PID, h.Host, h.Acquired.Format(time.RFC3339), h.Operation)
	}
	return nil, fmt.Errorf("%w: %s", ErrHeld, path)
}

func inspect(path string) (holder, time.Duration, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return holder{}, 0, err
	}
	var h holder
	if err := json.Unmarshal(b, &h); err != nil {
		return holder{}, 0, err
	}
	if h.Acquired.IsZero() {
		return h, 0, fmt.Errorf("lock file has no acquisition time")
	}
	return h, time.Since(h.Acquired), nil
}

// Release removes the lock file.
func (l *Lock) Release() error {
	if l == nil || l.path == "" {
		return nil
	}
	err := os.Remove(l.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
