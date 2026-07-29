// Package locate finds ccm's sibling executables.
//
// The three programs ship together and need to find each other: the CLI
// registers the tray for autostart, the tray launches the desktop app. Looking
// beside the running executable first means a copy installed somewhere other
// than PATH still works, and that a user with two installations does not get a
// cross-wired pair.
package locate

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Names of the executables, without any platform suffix.
const (
	CLI  = "ccm"
	Tray = "ccm-tray"
	GUI  = "ccm-gui"
)

// Executable returns the absolute path to a sibling program, or an empty
// string when it is not installed.
//
// An empty result is a normal condition rather than an error: the tray and the
// desktop app are optional, and callers are expected to hide the features that
// depend on them instead of offering something that fails on use.
func Executable(name string) string {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	// Beside the running binary first. An installation directory is the
	// strongest signal of which copy belongs together.
	if self, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(self); err == nil {
			self = resolved
		}
		candidate := filepath.Join(filepath.Dir(self), name)
		if isExecutableFile(candidate) {
			return candidate
		}
	}

	if p, err := exec.LookPath(name); err == nil {
		if abs, err := filepath.Abs(p); err == nil {
			return abs
		}
		return p
	}
	return ""
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	// Permission bits are not consulted on Windows, where the extension
	// determines executability and Stat reports 0666 regardless.
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return false
	}
	return true
}
