package autostart

import (
	"path/filepath"
	"strings"
	"testing"
)

// testEntry uses a name unique to the test run so a failure cannot leave a
// registration behind that looks like the real one.
func testEntry(t *testing.T) Entry {
	t.Helper()
	return Entry{
		Name:        "ccm-autostart-test",
		DisplayName: "ccm test entry",
		Exec:        filepath.Join(t.TempDir(), "ccm-tray-fake"),
	}
}

// The full lifecycle against the real platform mechanism: the registry on
// Windows, an XDG desktop entry on Linux, a LaunchAgent on macOS. Nothing here
// is mocked, because the value of this package is entirely in whether the
// operating system agrees with it.
func TestEnableDisableRoundTrip(t *testing.T) {
	e := testEntry(t)
	t.Cleanup(func() { _ = Disable(e.Name) })

	if on, err := IsEnabled(e.Name); err != nil {
		t.Fatalf("IsEnabled before enabling: %v", err)
	} else if on {
		t.Fatal("a fresh entry should not already be enabled")
	}

	if err := Enable(e); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if on, err := IsEnabled(e.Name); err != nil {
		t.Fatalf("IsEnabled after enabling: %v", err)
	} else if !on {
		t.Error("IsEnabled should report true after Enable")
	}

	if err := Disable(e.Name); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if on, err := IsEnabled(e.Name); err != nil {
		t.Fatalf("IsEnabled after disabling: %v", err)
	} else if on {
		t.Error("IsEnabled should report false after Disable")
	}
}

// Enable has to be safe to call when already enabled, because that is what an
// upgrade does when the executable path changes.
func TestEnableIsIdempotentAndUpdatesPath(t *testing.T) {
	e := testEntry(t)
	t.Cleanup(func() { _ = Disable(e.Name) })

	if err := Enable(e); err != nil {
		t.Fatal(err)
	}
	moved := e
	moved.Exec = filepath.Join(t.TempDir(), "moved", "ccm-tray-fake")
	if err := Enable(moved); err != nil {
		t.Fatalf("re-enabling with a new path: %v", err)
	}

	on, err := IsEnabled(e.Name)
	if err != nil {
		t.Fatal(err)
	}
	if !on {
		t.Error("still expected to be enabled after being rewritten")
	}
}

// Disabling something that was never enabled is what a fresh uninstall does,
// so it must not report failure.
func TestDisableUnregisteredIsNotAnError(t *testing.T) {
	if err := Disable("ccm-autostart-never-registered"); err != nil {
		t.Errorf("Disable on an unregistered entry should be a no-op, got %v", err)
	}
}

func TestIsEnabledForUnknownEntry(t *testing.T) {
	on, err := IsEnabled("ccm-autostart-never-registered")
	if err != nil {
		t.Fatalf("IsEnabled for an unknown entry should not error: %v", err)
	}
	if on {
		t.Error("an unregistered entry must not report as enabled")
	}
}

// The name becomes a filename on two of the three platforms, so a traversal
// sequence has to be refused rather than written somewhere unexpected.
func TestEnableRejectsDangerousNames(t *testing.T) {
	bad := map[string]string{
		"empty":          "",
		"whitespace":     "   ",
		"path separator": "../evil",
		"backslash":      `..\evil`,
		"colon":          "a:b",
		"wildcard":       "a*b",
		"null byte":      "a\x00b",
	}
	for label, name := range bad {
		t.Run(label, func(t *testing.T) {
			e := testEntry(t)
			e.Name = name
			if err := Enable(e); err == nil {
				t.Errorf("Enable should refuse the %s name %q", label, name)
				_ = Disable(name)
			}
		})
	}
}

func TestEnableRejectsEmptyExec(t *testing.T) {
	e := testEntry(t)
	e.Exec = ""
	if err := Enable(e); err == nil {
		t.Error("Enable should refuse an entry with no executable path")
	}
}

// A path containing a space is the common case on Windows, where the install
// directory sits under a user profile that often has one. If it is not quoted,
// the shell splits it and the program never starts.
func TestPathWithSpacesSurvivesRegistration(t *testing.T) {
	e := testEntry(t)
	e.Exec = filepath.Join(t.TempDir(), "Program Folder", "ccm tray.exe")
	t.Cleanup(func() { _ = Disable(e.Name) })

	if err := Enable(e); err != nil {
		t.Fatalf("Enable with a spaced path: %v", err)
	}
	on, err := IsEnabled(e.Name)
	if err != nil {
		t.Fatal(err)
	}
	if !on {
		t.Error("an entry with a spaced path should still register")
	}
}

func TestMechanismAndLocationAreDescriptive(t *testing.T) {
	if strings.TrimSpace(Mechanism()) == "" {
		t.Error("Mechanism should name the platform facility for diagnostics")
	}
	loc := Location("ccm-tray")
	if strings.TrimSpace(loc) == "" {
		t.Error("Location should say where the registration lives")
	}
	if !strings.Contains(loc, "ccm-tray") {
		t.Errorf("Location should mention the entry name, got %q", loc)
	}
}
