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
	"strings"
)

// Names of the executables, without any platform suffix.
const (
	CLI  = "ccm"
	Tray = "ccm-tray"
	GUI  = "ccm-gui"
)

// darwinBundles maps a program to the application bundle that carries it.
//
// macOS derives an application's identity by walking up from the executable
// path, so every binary inside one bundle shares that bundle's Info.plist. The
// menu bar app needs LSUIElement and the desktop app must not have it, and a
// single plist cannot say both. They therefore ship as two bundles, and the CLI
// rides along in the desktop app's because it is the one a user opens.
//
// The names here must match the directories packaging/macos/build-pkg.sh
// creates. Nothing at compile time checks that, so the release workflow asserts
// it by installing the package and reading back `ccm autostart status`.
var darwinBundles = map[string]string{
	CLI:  "Claude Code Accounts",
	GUI:  "Claude Code Accounts",
	Tray: "Claude Code Accounts Menu Bar",
}

// bundleSuffix is the path from a bundle root to the directory holding its
// executables.
var bundleSuffix = filepath.Join("Contents", "MacOS")

// Executable returns the absolute path to a sibling program, or an empty
// string when it is not installed.
//
// An empty result is a normal condition rather than an error: the tray and the
// desktop app are optional, and callers are expected to hide the features that
// depend on them instead of offering something that fails on use.
func Executable(name string) string {
	return realFinder().find(name)
}

// finder holds everything Executable touches in the environment.
//
// It exists so the macOS bundle logic can be tested from any platform. That
// path is the one most likely to break and the one least likely to be exercised
// during development, since it only appears once the program is installed from
// the package rather than run from a build directory.
type finder struct {
	goos       string
	executable func() (string, error)
	evalSyms   func(string) (string, error)
	lookPath   func(string) (string, error)
	isExec     func(string) bool
}

func realFinder() finder {
	return finder{
		goos:       runtime.GOOS,
		executable: os.Executable,
		evalSyms:   filepath.EvalSymlinks,
		lookPath:   exec.LookPath,
		isExec:     isExecutableFile,
	}
}

func (f finder) find(name string) string {
	if f.goos == "windows" {
		name += ".exe"
	}

	if self, err := f.executable(); err == nil {
		// Resolve first: on macOS a program started through a symlink in
		// /usr/local/bin reports the symlink, and its siblings live beside the
		// real file inside the bundle, not beside the link.
		if resolved, err := f.evalSyms(self); err == nil {
			self = resolved
		}
		dir := filepath.Dir(self)

		// Beside the running binary. An installation directory is the
		// strongest signal of which copy belongs together.
		if candidate := filepath.Join(dir, name); f.isExec(candidate) {
			return candidate
		}

		// A neighbouring application bundle. Only reachable on macOS, and only
		// when the running binary is itself inside one, so an ordinary
		// directory of binaries still resolves above and never gets here.
		if candidate := f.siblingBundle(dir, name); candidate != "" {
			return candidate
		}
	}

	if p, err := f.lookPath(name); err == nil {
		if abs, err := filepath.Abs(p); err == nil {
			return abs
		}
		return p
	}
	return ""
}

// AppBundle returns the macOS application bundle carrying a program, or an
// empty string when there is none: on other platforms, or when the program is
// installed as a loose binary rather than from the package.
//
// A desktop shortcut has to point at the bundle rather than the executable
// inside it. Opening the bundle goes through LaunchServices, which gives the
// process its icon, its name in the menu bar and its Info.plist; opening the
// inner binary directly gets none of that.
func AppBundle(name string) string { return realFinder().appBundle(name) }

func (f finder) appBundle(name string) string {
	if f.goos != "darwin" {
		return ""
	}
	exe := f.find(name)
	if exe == "" {
		return ""
	}
	root, ok := bundleRoot(filepath.Dir(exe))
	if !ok {
		return ""
	}
	return root
}

// siblingBundle looks for name inside another .app sitting alongside the bundle
// that dir belongs to.
//
// dir is the directory holding the running executable. When it looks like
// <anything>.app/Contents/MacOS, the directory three levels up is where the
// installer put the bundles, so the companion is at
// <that>/<bundle for name>.app/Contents/MacOS/<name>.
func (f finder) siblingBundle(dir, name string) string {
	if f.goos != "darwin" {
		return ""
	}
	bundle, ok := darwinBundles[name]
	if !ok {
		return ""
	}
	root, ok := bundleRoot(dir)
	if !ok {
		return ""
	}
	candidate := filepath.Join(filepath.Dir(root), bundle+".app", bundleSuffix, name)
	if f.isExec(candidate) {
		return candidate
	}
	return ""
}

// bundleRoot reports the .app directory containing dir, when dir is a bundle's
// Contents/MacOS.
func bundleRoot(dir string) (string, bool) {
	macos := filepath.Clean(dir)
	if filepath.Base(macos) != "MacOS" {
		return "", false
	}
	contents := filepath.Dir(macos)
	if filepath.Base(contents) != "Contents" {
		return "", false
	}
	root := filepath.Dir(contents)
	if !strings.HasSuffix(root, ".app") {
		return "", false
	}
	return root, true
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
