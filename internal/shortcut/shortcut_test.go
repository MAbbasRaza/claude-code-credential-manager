package shortcut

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// isolate points the platform's shortcut directories at a temporary tree, so
// these tests never write to the developer's real desktop or Start Menu.
//
// Every implementation reads its directory from the environment for exactly
// this reason. Without it the only options would be mocking the filesystem,
// which would stop the tests exercising the real .lnk and .desktop writers, or
// littering the machine running them.
func isolate(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	switch runtime.GOOS {
	case "windows":
		t.Setenv("USERPROFILE", root)
		t.Setenv("APPDATA", filepath.Join(root, "AppData", "Roaming"))
	case "darwin":
		t.Setenv("HOME", root)
	default:
		t.Setenv("HOME", root)
		t.Setenv("XDG_DESKTOP_DIR", filepath.Join(root, "Desktop"))
		t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".local", "share"))
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	}
	return root
}

// target creates something real to point at. A shortcut to a path that does not
// exist is still written on every platform, but pointing at a real file keeps
// the test honest about the normal case.
func target(t *testing.T, root string) string {
	t.Helper()
	if runtime.GOOS == "darwin" {
		// The macOS shortcut is a symlink to a bundle, not to a binary.
		app := filepath.Join(root, "Applications", "Claude Code Accounts.app")
		if err := os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755); err != nil {
			t.Fatal(err)
		}
		return app
	}
	name := "ccm-gui"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	p := filepath.Join(root, "bin", name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func entry(t *testing.T, root string) Entry {
	return Entry{
		Name:        "Claude Code Accounts",
		DisplayName: "Claude Code Accounts",
		Target:      target(t, root),
		Description: "Switch between Claude Code accounts",
		Icon:        "ccm",
	}
}

// The core contract, run against the real platform writer on whichever host
// the suite is on.
func TestAddExistsRemove(t *testing.T) {
	for _, k := range Kinds {
		t.Run(string(k), func(t *testing.T) {
			root := isolate(t)
			if !Supported(k) {
				t.Skipf("%s shortcuts are not supported on %s", k, runtime.GOOS)
			}
			e := entry(t, root)

			on, err := Exists(k, e.Name)
			if err != nil {
				t.Fatal(err)
			}
			if on {
				t.Fatal("a fresh environment should have no shortcut")
			}

			if err := Add(k, e); err != nil {
				t.Fatalf("Add: %v", err)
			}

			on, err = Exists(k, e.Name)
			if err != nil {
				t.Fatal(err)
			}
			if !on {
				t.Errorf("Exists is false after Add; Location says %s", Location(k, e.Name))
			}

			// Whatever Add wrote must be exactly where Location says it is, or
			// a user could not remove it by hand and Remove would miss it.
			if _, err := os.Lstat(Location(k, e.Name)); err != nil {
				t.Errorf("nothing at the reported location: %v", err)
			}

			if err := Remove(k, e.Name); err != nil {
				t.Fatalf("Remove: %v", err)
			}
			on, err = Exists(k, e.Name)
			if err != nil {
				t.Fatal(err)
			}
			if on {
				t.Error("Exists is still true after Remove")
			}
		})
	}
}

// Add is documented idempotent so the installer and an upgrade can both call it
// without checking first.
func TestAddIsIdempotent(t *testing.T) {
	root := isolate(t)
	e := entry(t, root)

	if err := Add(Desktop, e); err != nil {
		t.Fatal(err)
	}
	if err := Add(Desktop, e); err != nil {
		t.Fatalf("second Add should succeed, got %v", err)
	}
	on, err := Exists(Desktop, e.Name)
	if err != nil || !on {
		t.Errorf("shortcut missing after two Adds: %v %v", on, err)
	}
}

// Callers are not expected to check first.
func TestRemoveMissingIsNotAnError(t *testing.T) {
	isolate(t)
	if err := Remove(Desktop, "nothing-here"); err != nil {
		t.Errorf("removing an absent shortcut should be a no-op, got %v", err)
	}
}

// A name becomes a filename, so anything that could escape the directory is
// refused rather than sanitised. Same rule as internal/autostart.
func TestNamesThatCouldEscapeAreRefused(t *testing.T) {
	root := isolate(t)
	tgt := target(t, root)

	for _, bad := range []string{
		"", "   ",
		"../evil", `..\evil`, "a/b", `a\b`,
		"a:b", "a*b", "a?b", `a"b`, "a<b", "a>b", "a|b",
		"a\x00b",
	} {
		err := Add(Desktop, Entry{Name: bad, Target: tgt})
		if err == nil {
			t.Errorf("Add accepted name %q", bad)
			continue
		}
		if !errors.Is(err, ErrInvalidEntry) {
			t.Errorf("Add(%q) = %v, want ErrInvalidEntry", bad, err)
		}
	}
}

func TestEmptyTargetIsRefused(t *testing.T) {
	isolate(t)
	err := Add(Desktop, Entry{Name: "x", Target: "  "})
	if !errors.Is(err, ErrInvalidEntry) {
		t.Errorf("got %v, want ErrInvalidEntry", err)
	}
}

func TestUnknownKindIsRefused(t *testing.T) {
	isolate(t)
	if err := Add(Kind("dock"), Entry{Name: "x", Target: "y"}); !errors.Is(err, ErrInvalidEntry) {
		t.Errorf("got %v, want ErrInvalidEntry", err)
	}
	if _, err := Exists(Kind("dock"), "x"); !errors.Is(err, ErrInvalidEntry) {
		t.Errorf("got %v, want ErrInvalidEntry", err)
	}
}

// An unsupported kind must be inert rather than failing: callers hide the
// option, and a Remove during uninstall must not error on a platform that never
// had one.
func TestUnsupportedKindIsInert(t *testing.T) {
	isolate(t)
	for _, k := range Kinds {
		if Supported(k) {
			continue
		}
		if err := Remove(k, "Claude Code Accounts"); err != nil {
			t.Errorf("Remove(%s) on an unsupporting platform should be a no-op, got %v", k, err)
		}
		on, err := Exists(k, "Claude Code Accounts")
		if err != nil || on {
			t.Errorf("Exists(%s) = %v, %v; want false, nil", k, on, err)
		}
		if got := Location(k, "x"); got != "" {
			t.Errorf("Location(%s) = %q, want empty", k, got)
		}
		if err := Add(k, Entry{Name: "x", Target: "y"}); !errors.Is(err, ErrUnsupported) {
			t.Errorf("Add(%s) = %v, want ErrUnsupported", k, err)
		}
	}
}

// macOS is the platform with no separate application menu, and that has to stay
// deliberate rather than becoming an accident of some later edit.
func TestMenuIsUnsupportedOnlyOnDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		if Supported(Menu) {
			t.Error("macOS has no application menu separate from /Applications")
		}
		if !Supported(Desktop) {
			t.Error("macOS does have a desktop")
		}
		return
	}
	if !Supported(Menu) || !Supported(Desktop) {
		t.Errorf("both kinds should be supported on %s", runtime.GOOS)
	}
}

// The Linux entry has to be launchable, which needs more than the right
// contents: GNOME and KDE both refuse a desktop-directory entry that is not
// executable, showing it as a text file instead.
func TestLinuxDesktopEntryIsExecutableAndWellFormed(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux only")
	}
	root := isolate(t)
	e := entry(t, root)
	if err := Add(Desktop, e); err != nil {
		t.Fatal(err)
	}

	p := Location(Desktop, e.Name)
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("mode is %v; a desktop entry that is not executable will not launch", info.Mode().Perm())
	}

	body, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{
		"[Desktop Entry]", "Type=Application",
		"Name=Claude Code Accounts", "Icon=ccm", "Terminal=false",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("entry is missing %q:\n%s", want, text)
		}
	}
	if !strings.Contains(text, "Exec="+e.Target) {
		t.Errorf("Exec does not name the target:\n%s", text)
	}
}

// Quoting is only meaningful inside Exec. A quoted Name puts visible quotation
// marks in the application menu, and desktop-file-validate accepts it without
// complaint because quotes are legal characters there. Caught exactly that way:
// the first Ubuntu run produced Name="Claude Code Accounts" and passed
// validation.
func TestLinuxDoesNotQuotePlainValues(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux only")
	}
	root := isolate(t)
	e := entry(t, root)
	if err := Add(Desktop, e); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(Location(Desktop, e.Name))
	if err != nil {
		t.Fatal(err)
	}

	for _, line := range strings.Split(string(body), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok || key == "Exec" {
			continue
		}
		if strings.HasPrefix(value, `"`) {
			t.Errorf("%s is quoted, which shows the quotes to the user: %s", key, line)
		}
	}

	// The display name has a space in it, so this is not a vacuous check.
	if !strings.Contains(string(body), "Name="+e.displayName()+"\n") {
		t.Errorf("Name should be the bare display name:\n%s", body)
	}
}

// A path with a space is the normal case on Windows and common on macOS, and
// an unquoted one would be read as a command plus an argument.
func TestLinuxQuotesATargetContainingSpaces(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux only")
	}
	root := isolate(t)
	spaced := filepath.Join(root, "some dir", "ccm-gui")
	if err := os.MkdirAll(filepath.Dir(spaced), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spaced, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := Add(Desktop, Entry{Name: "Spaced", Target: spaced}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(Location(Desktop, "Spaced"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `Exec="`+spaced+`"`) {
		t.Errorf("a target with a space must be quoted:\n%s", body)
	}
}

// The .lnk has to be a real shell link, not an empty or malformed file that
// Explorer shows as broken. The header is 76 bytes beginning with a length
// field of 0x4C and the shell link class id.
func TestWindowsWritesARealShellLink(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows only")
	}
	root := isolate(t)
	e := entry(t, root)
	if err := Add(Desktop, e); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(Location(Desktop, e.Name))
	if err != nil {
		t.Fatal(err)
	}
	if len(body) < 76 {
		t.Fatalf("a shell link header is 76 bytes; got %d", len(body))
	}
	if body[0] != 0x4C || body[1] != 0 || body[2] != 0 || body[3] != 0 {
		t.Errorf("HeaderSize is not 0x0000004C: % x", body[:4])
	}
	// CLSID_ShellLink, {00021401-0000-0000-C000-000000000046}, little endian.
	want := []byte{0x01, 0x14, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00,
		0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}
	for i, b := range want {
		if body[4+i] != b {
			t.Errorf("link class id mismatch at byte %d: % x", i, body[4:20])
			break
		}
	}
}

func TestLocationIsUnderTheIsolatedRoot(t *testing.T) {
	root := isolate(t)
	for _, k := range Kinds {
		if !Supported(k) {
			continue
		}
		p := Location(k, "Claude Code Accounts")
		if p == "" {
			t.Errorf("Location(%s) is empty", k)
			continue
		}
		if !strings.HasPrefix(p, root) {
			t.Errorf("Location(%s) = %q, which is outside the test root %q; "+
				"this test would be writing to the real desktop", k, p, root)
		}
	}
}
