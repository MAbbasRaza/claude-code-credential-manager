package locate

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// rootDir is an absolute path valid on whatever host runs the tests.
//
// Everything below is built from it so the expectations do not encode a
// separator or a drive letter. That matters because these cases assert macOS
// behaviour and have to pass unchanged on the Windows development machine,
// where filepath.Join emits backslashes and filepath.Abs prepends a drive.
var rootDir = func() string {
	r, err := filepath.Abs(string(filepath.Separator) + "ccm-locate-test")
	if err != nil {
		panic(err)
	}
	return r
}()

func at(parts ...string) string {
	return filepath.Join(append([]string{rootDir}, parts...)...)
}

var (
	// The two bundles the macOS package installs.
	guiApp  = at("Applications", "Claude Code Accounts.app", "Contents", "MacOS")
	trayApp = at("Applications", "Claude Code Accounts Menu Bar.app", "Contents", "MacOS")
)

// fakeEnv builds a finder over a synthetic filesystem.
//
// Every environment dependency is injected, which is the point: the macOS
// bundle layout only exists once ccm is installed from the package, so without
// this it could not be exercised from a development machine at all.
func fakeEnv(goos, self string, execs []string, path map[string]string) finder {
	set := make(map[string]bool, len(execs))
	for _, e := range execs {
		set[filepath.Clean(e)] = true
	}
	return finder{
		goos:       goos,
		executable: func() (string, error) { return self, nil },
		evalSyms:   func(s string) (string, error) { return s, nil },
		lookPath: func(name string) (string, error) {
			if p, ok := path[name]; ok {
				return p, nil
			}
			return "", errors.New("not found")
		},
		isExec: func(p string) bool { return set[filepath.Clean(p)] },
	}
}

func TestFind(t *testing.T) {
	flat := at("opt", "ccm")
	bin := at("usr", "local", "bin")

	tests := []struct {
		name   string
		goos   string
		self   string
		execs  []string
		path   map[string]string
		target string
		want   string
	}{
		// The two cases the two-bundle layout introduced. Both fail before the
		// sibling-bundle lookup exists, and both fail silently in production:
		// the tray drops its "Manage accounts" item and `ccm autostart enable`
		// reports the tray as not installed on a correctly installed machine.
		{
			name:   "darwin tray finds the desktop app in the neighbouring bundle",
			goos:   "darwin",
			self:   filepath.Join(trayApp, "ccm-tray"),
			execs:  []string{filepath.Join(trayApp, "ccm-tray"), filepath.Join(guiApp, "ccm-gui"), filepath.Join(guiApp, "ccm")},
			target: GUI,
			want:   filepath.Join(guiApp, "ccm-gui"),
		},
		{
			name:   "darwin CLI finds the tray in the neighbouring bundle",
			goos:   "darwin",
			self:   filepath.Join(guiApp, "ccm"),
			execs:  []string{filepath.Join(guiApp, "ccm"), filepath.Join(guiApp, "ccm-gui"), filepath.Join(trayApp, "ccm-tray")},
			target: Tray,
			want:   filepath.Join(trayApp, "ccm-tray"),
		},

		// Beside-the-binary still wins, and is still tried first.
		{
			name:   "darwin CLI finds the desktop app inside its own bundle",
			goos:   "darwin",
			self:   filepath.Join(guiApp, "ccm"),
			execs:  []string{filepath.Join(guiApp, "ccm"), filepath.Join(guiApp, "ccm-gui")},
			target: GUI,
			want:   filepath.Join(guiApp, "ccm-gui"),
		},
		{
			name:   "a flat directory resolves without any bundle involvement",
			goos:   "darwin",
			self:   filepath.Join(bin, "ccm"),
			execs:  []string{filepath.Join(bin, "ccm"), filepath.Join(bin, "ccm-tray")},
			target: Tray,
			want:   filepath.Join(bin, "ccm-tray"),
		},
		{
			name:   "linux resolves beside the binary, which is what the deb installs",
			goos:   "linux",
			self:   at("usr", "bin", "ccm"),
			execs:  []string{at("usr", "bin", "ccm"), at("usr", "bin", "ccm-tray")},
			target: Tray,
			want:   at("usr", "bin", "ccm-tray"),
		},

		// The bundle probe must not fire outside its layout, or a stray
		// directory named *.app could redirect the lookup.
		{
			name:   "darwin does not probe bundles when not running inside one",
			goos:   "darwin",
			self:   filepath.Join(flat, "ccm-tray"),
			execs:  []string{filepath.Join(flat, "ccm-tray"), filepath.Join(guiApp, "ccm-gui")},
			target: GUI,
			want:   "",
		},
		{
			name:   "a directory that merely ends in .app is not a bundle",
			goos:   "darwin",
			self:   at("weird", "thing.app", "ccm-tray"),
			execs:  []string{at("weird", "thing.app", "ccm-tray"), filepath.Join(guiApp, "ccm-gui")},
			target: GUI,
			want:   "",
		},
		{
			name:   "the bundle probe is darwin only",
			goos:   "linux",
			self:   filepath.Join(trayApp, "ccm-tray"),
			execs:  []string{filepath.Join(trayApp, "ccm-tray"), filepath.Join(guiApp, "ccm-gui")},
			target: GUI,
			want:   "",
		},

		// PATH remains the last resort.
		{
			name:   "falls back to PATH",
			goos:   "linux",
			self:   filepath.Join(flat, "ccm"),
			execs:  []string{filepath.Join(flat, "ccm")},
			path:   map[string]string{"ccm-tray": at("usr", "bin", "ccm-tray")},
			target: Tray,
			want:   at("usr", "bin", "ccm-tray"),
		},
		{
			name:   "PATH is consulted after the bundle probe misses",
			goos:   "darwin",
			self:   filepath.Join(trayApp, "ccm-tray"),
			execs:  []string{filepath.Join(trayApp, "ccm-tray")},
			path:   map[string]string{"ccm-gui": filepath.Join(bin, "ccm-gui")},
			target: GUI,
			want:   filepath.Join(bin, "ccm-gui"),
		},
		{
			name:   "nothing installed is an empty string, not an error",
			goos:   "linux",
			self:   filepath.Join(flat, "ccm"),
			execs:  []string{filepath.Join(flat, "ccm")},
			target: GUI,
			want:   "",
		},

		// Windows keeps its extension handling.
		{
			name:   "windows appends .exe",
			goos:   "windows",
			self:   filepath.Join(flat, "ccm.exe"),
			execs:  []string{filepath.Join(flat, "ccm.exe"), filepath.Join(flat, "ccm-gui.exe")},
			target: GUI,
			want:   filepath.Join(flat, "ccm-gui.exe"),
		},
		{
			name:   "windows does not accept a name lacking the extension",
			goos:   "windows",
			self:   filepath.Join(flat, "ccm.exe"),
			execs:  []string{filepath.Join(flat, "ccm.exe"), filepath.Join(flat, "ccm-gui")},
			target: GUI,
			want:   "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := fakeEnv(tc.goos, tc.self, tc.execs, tc.path)
			if got := f.find(tc.target); got != tc.want {
				t.Errorf("find(%q) = %q, want %q", tc.target, got, tc.want)
			}
		})
	}
}

// Installed from the macOS package the CLI is a symlink in /usr/local/bin, so a
// program started through one has to resolve before looking at its neighbours
// or it searches the symlink's directory. The tray's old private lookup did not
// resolve, which is one of the two reasons it was folded into this package.
func TestSymlinkedEntryPointResolvesIntoTheBundle(t *testing.T) {
	link := at("usr", "local", "bin", "ccm")
	real := filepath.Join(guiApp, "ccm")

	f := fakeEnv("darwin", link,
		[]string{real, filepath.Join(guiApp, "ccm-gui"), filepath.Join(trayApp, "ccm-tray")}, nil)
	f.evalSyms = func(s string) (string, error) {
		if s == link {
			return real, nil
		}
		return s, nil
	}

	if got, want := f.find(GUI), filepath.Join(guiApp, "ccm-gui"); got != want {
		t.Errorf("desktop app = %q, want %q", got, want)
	}
	if got, want := f.find(Tray), filepath.Join(trayApp, "ccm-tray"); got != want {
		t.Errorf("tray = %q, want %q", got, want)
	}
}

// A directory named like the executable must not be returned. The tray's old
// private lookup used a bare os.Stat and would have handed one to exec, which
// fails at launch with a message the user cannot act on.
func TestDirectoryNamedLikeTheBinaryIsRejected(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, exeName("ccm-gui")), 0o755); err != nil {
		t.Fatal(err)
	}

	f := realFinder()
	f.executable = func() (string, error) { return filepath.Join(dir, exeName("ccm")), nil }
	f.lookPath = func(string) (string, error) { return "", errors.New("not found") }

	if got := f.find(GUI); got != "" {
		t.Errorf("find(GUI) = %q, want empty; a directory is not an executable", got)
	}
}

func exeName(n string) string {
	if runtime.GOOS == "windows" {
		return n + ".exe"
	}
	return n
}

// Guards the map against a rename that would leave the Go side and
// packaging/macos/build-pkg.sh disagreeing. The release workflow proves they
// really match by installing the package; this only catches a missing key.
func TestEveryProgramHasABundle(t *testing.T) {
	for _, name := range []string{CLI, Tray, GUI} {
		if darwinBundles[name] == "" {
			t.Errorf("no bundle recorded for %q", name)
		}
	}
	if darwinBundles[Tray] == darwinBundles[GUI] {
		t.Error("the tray must live in its own bundle so it can set LSUIElement " +
			"without also hiding the desktop app from the Dock")
	}
}

func TestBundleRoot(t *testing.T) {
	tests := []struct {
		dir  string
		want string
		ok   bool
	}{
		{guiApp, at("Applications", "Claude Code Accounts.app"), true},
		{at("Applications", "Foo.app", "Contents", "Resources"), "", false},
		{at("Applications", "Foo", "Contents", "MacOS"), "", false},
		{at("usr", "local", "bin"), "", false},
		{"", "", false},
	}
	for _, tc := range tests {
		got, ok := bundleRoot(tc.dir)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("bundleRoot(%q) = %q,%v want %q,%v", tc.dir, got, ok, tc.want, tc.ok)
		}
	}
}

// Sanity check that the real finder is wired to the real environment, since
// every other test replaces all of it.
func TestRealFinderIsFullyWired(t *testing.T) {
	f := realFinder()
	if f.goos == "" || f.executable == nil || f.evalSyms == nil || f.lookPath == nil || f.isExec == nil {
		t.Fatal("realFinder left a dependency unset")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not on PATH")
	}
	if got := f.find("go"); got == "" {
		t.Error("expected the PATH fallback to find the go toolchain")
	}
}
