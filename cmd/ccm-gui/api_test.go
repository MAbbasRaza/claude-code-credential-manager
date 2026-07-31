package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/MAbbasRaza/claude-code-multi-account-manager/internal/shortcut"
)

// The GUI is the least-covered surface in the project and it handles OAuth
// tokens, so these tests exercise the binding layer directly. They use the same
// synthetic-fixture discipline as internal/manager: fake tokens, .invalid
// emails, and a temporary directory. Nothing here touches a real account.

const fakeCreds = `{
  "claudeAiOauth": {
    "accessToken": "FAKE-ACCESS-TOKEN-DO-NOT-USE",
    "refreshToken": "FAKE-REFRESH-TOKEN-DO-NOT-USE",
    "expiresAt": 1750000000000,
    "scopes": ["user:inference"],
    "subscriptionType": "max"
  },
  "mcpOAuth": {"gmail": {"accessToken": "FAKE-MCP-TOKEN"}}
}`

func fakeConfig(uuid, email, org string) string {
	return `{
  "numStartups": 3,
  "userID": "user-` + uuid + `",
  "oauthAccount": {
    "accountUuid": "` + uuid + `",
    "emailAddress": "` + email + `",
    "organizationUuid": "org-` + uuid + `",
    "organizationName": "` + org + `"
  }
}`
}

// newEnv points the whole program at temporary directories.
func newEnv(t *testing.T) string {
	t.Helper()
	claudeDir := t.TempDir()
	ccmHome := t.TempDir()

	t.Setenv("CLAUDE_CONFIG_DIR", claudeDir)
	t.Setenv("CCM_CLAUDE_CONFIG_DIR", "")
	t.Setenv("CCM_HOME", ccmHome)
	// Forced so the suite behaves identically on macOS, where the default
	// backend is the Keychain and the fixture file would be ignored.
	t.Setenv("CCM_CREDENTIALS_BACKEND", "file")
	// Same reason for the vault, and macOS only: its key lives in the login
	// keychain, which a non-GUI session cannot unlock. Windows and Linux seal
	// fine headless, so they keep exercising their real scheme.
	if runtime.GOOS == "darwin" {
		t.Setenv("CCM_VAULT_BACKEND", "file")
	}

	settings := `{"claudeConfigDir":` + quoteJSON(claudeDir) + `,"requireClosedSessions":false}`
	if err := os.WriteFile(filepath.Join(ccmHome, "config.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	return claudeDir
}

func quoteJSON(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}

func signIn(t *testing.T, dir, uuid, email, org string) {
	t.Helper()
	write(t, filepath.Join(dir, ".credentials.json"), fakeCreds)
	write(t, filepath.Join(dir, ".claude.json"), fakeConfig(uuid, email, org))
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestListWithNoLogin(t *testing.T) {
	newEnv(t)
	a := &api{}

	got, err := a.List()
	if err != nil {
		t.Fatalf("List with no config should not error: %v", err)
	}
	if got.LoggedIn {
		t.Error("LoggedIn should be false")
	}
	// The page iterates this directly, so a nil slice would marshal to null
	// and break rendering rather than showing the empty state.
	if got.Profiles == nil {
		t.Error("Profiles must be an empty slice, not nil")
	}
	if len(got.Profiles) != 0 {
		t.Errorf("expected no profiles, got %d", len(got.Profiles))
	}
}

func TestListReportsActiveAccount(t *testing.T) {
	dir := newEnv(t)
	signIn(t, dir, "acct-a", "a@example.invalid", "Org A")

	a := &api{}
	if _, err := a.Capture("work"); err != nil {
		t.Fatalf("Capture: %v", err)
	}

	got, err := a.List()
	if err != nil {
		t.Fatal(err)
	}
	if !got.LoggedIn {
		t.Fatal("expected LoggedIn")
	}
	if got.Email != "a@example.invalid" {
		t.Errorf("Email = %q", got.Email)
	}
	if got.Organization != "Org A" {
		t.Errorf("Organization = %q", got.Organization)
	}
	if got.Plan != "max" {
		t.Errorf("Plan = %q, want max", got.Plan)
	}
	if got.ActiveName != "work" {
		t.Errorf("ActiveName = %q, want work", got.ActiveName)
	}
	if len(got.Profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(got.Profiles))
	}
	if !got.Profiles[0].Active {
		t.Error("the only profile matches the live account and should be marked active")
	}
	if got.Profiles[0].Expiry == "" {
		t.Error("expected a formatted expiry")
	}
}

func TestSwitchRoundTrip(t *testing.T) {
	dir := newEnv(t)
	signIn(t, dir, "acct-a", "a@example.invalid", "Org A")
	a := &api{}
	if _, err := a.Capture("work"); err != nil {
		t.Fatal(err)
	}

	signIn(t, dir, "acct-b", "b@example.invalid", "Org B")
	if _, err := a.Capture("personal"); err != nil {
		t.Fatal(err)
	}

	res, err := a.Switch("work", false)
	if err != nil {
		t.Fatalf("Switch: %v", err)
	}
	if !res.Switched {
		t.Fatal("expected Switched")
	}
	if res.Blocked {
		t.Error("should not be Blocked when the guard is disabled")
	}
	if res.To != "work" {
		t.Errorf("To = %q", res.To)
	}
	// The outgoing account must have been captured, which is the property the
	// whole tool exists to provide.
	if res.CapturedAs != "personal" {
		t.Errorf("CapturedAs = %q, want personal", res.CapturedAs)
	}

	after, _ := a.List()
	if after.ActiveName != "work" {
		t.Errorf("after switching, ActiveName = %q", after.ActiveName)
	}
}

// The page distinguishes a refusal from a failure: a refusal is offered back
// to the user with an override. If this ever surfaced as an error instead, the
// user would see a scary toast and no way forward.
func TestSwitchGuardIsReturnedAsDataNotError(t *testing.T) {
	dir := newEnv(t)
	ccmHome := os.Getenv("CCM_HOME")
	write(t, filepath.Join(ccmHome, "config.json"),
		`{"claudeConfigDir":`+quoteJSON(dir)+`,"requireClosedSessions":true}`)

	signIn(t, dir, "acct-a", "a@example.invalid", "Org A")
	a := &api{}
	if _, err := a.Capture("work"); err != nil {
		t.Fatal(err)
	}
	signIn(t, dir, "acct-b", "b@example.invalid", "Org B")
	if _, err := a.Capture("personal"); err != nil {
		t.Fatal(err)
	}

	res, err := a.Switch("work", false)
	if err != nil {
		// Only meaningful when Claude Code is actually running, which is true
		// on a developer machine but not necessarily in CI.
		t.Fatalf("a refusal must not surface as an error: %v", err)
	}

	if res.Blocked && res.Undetermined {
		t.Error("Blocked and Undetermined are different states and must be mutually exclusive")
	}
	if res.Blocked {
		if res.BlockedCount < 1 {
			t.Error("a Blocked result must report a real process count; reporting zero " +
				"invites the user to override a guard that did fire")
		}
		if len(res.BlockedPIDs) != res.BlockedCount {
			t.Errorf("BlockedPIDs has %d entries but BlockedCount is %d",
				len(res.BlockedPIDs), res.BlockedCount)
		}
		if res.Message == "" {
			t.Error("a Blocked result should carry the explanation")
		}
		if res.Switched {
			t.Error("Blocked and Switched must not both be set")
		}
	}
	if res.Undetermined {
		if res.BlockedCount != 0 {
			t.Error("an undetermined result has no process count to report")
		}
		if res.Message == "" {
			t.Error("an undetermined result must carry the real reason; discarding it " +
				"leaves the user with no way to find out why switching is refused")
		}
	}
}

func TestRenameAndRemove(t *testing.T) {
	dir := newEnv(t)
	signIn(t, dir, "acct-a", "a@example.invalid", "Org A")
	a := &api{}
	if _, err := a.Capture("work"); err != nil {
		t.Fatal(err)
	}

	if err := a.Rename("work", "day-job"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	got, _ := a.List()
	if len(got.Profiles) != 1 || got.Profiles[0].Name != "day-job" {
		t.Fatalf("after rename: %+v", got.Profiles)
	}

	if err := a.Rename("day-job", "-bad"); err == nil {
		t.Error("a dash-prefixed name should be rejected")
	}
	if err := a.Rename("missing", "whatever"); err == nil {
		t.Error("renaming a missing profile should fail")
	}

	if err := a.Remove("day-job"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	got, _ = a.List()
	if len(got.Profiles) != 0 {
		t.Errorf("expected no profiles after removal, got %d", len(got.Profiles))
	}
	if err := a.Remove("day-job"); err == nil {
		t.Error("removing a missing profile should fail")
	}
}

func TestCaptureRefusesDuplicateAccount(t *testing.T) {
	dir := newEnv(t)
	signIn(t, dir, "acct-a", "a@example.invalid", "Org A")
	a := &api{}
	if _, err := a.Capture("work"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Capture("second-name"); err == nil {
		t.Error("storing one account under two names must be refused through the GUI too")
	}
}

// Diagnostics text is explicitly advertised in the UI as safe to share. If a
// token ever reached it, users would paste secrets into public bug reports.
func TestDoctorLeaksNoTokenMaterial(t *testing.T) {
	dir := newEnv(t)
	signIn(t, dir, "acct-a", "a@example.invalid", "Org A")
	a := &api{}
	if _, err := a.Capture("work"); err != nil {
		t.Fatal(err)
	}

	text, err := a.Doctor()
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{
		"FAKE-ACCESS-TOKEN-DO-NOT-USE",
		"FAKE-REFRESH-TOKEN-DO-NOT-USE",
		"FAKE-MCP-TOKEN",
		"accessToken",
		"refreshToken",
	} {
		if strings.Contains(text, secret) {
			t.Errorf("diagnostics contain %q; it is advertised as safe to paste into a bug report", secret)
		}
	}
	if !strings.Contains(text, "a@example.invalid") {
		t.Error("diagnostics should still identify the account, which is not secret")
	}
}

// The same guarantee for the payload the page receives. A token nested in any
// returned struct would be reachable from JavaScript.
func TestListLeaksNoTokenMaterial(t *testing.T) {
	dir := newEnv(t)
	signIn(t, dir, "acct-a", "a@example.invalid", "Org A")
	a := &api{}
	if _, err := a.Capture("work"); err != nil {
		t.Fatal(err)
	}

	got, err := a.List()
	if err != nil {
		t.Fatal(err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"FAKE-ACCESS-TOKEN", "FAKE-REFRESH-TOKEN", "FAKE-MCP-TOKEN"} {
		if strings.Contains(string(blob), secret) {
			t.Errorf("the payload sent to the page contains %q", secret)
		}
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	dir := newEnv(t)
	// The directory has to look like a real installation, because SettingsSet
	// validates it. Accepting an arbitrary path would let a user point ccm at
	// somewhere Claude Code never reads, producing switches that report success
	// and change nothing.
	signIn(t, dir, "acct-a", "a@example.invalid", "Org A")
	a := &api{}

	s, err := a.SettingsGet()
	if err != nil {
		t.Fatal(err)
	}
	if s.CredentialsBackend != "auto" {
		t.Errorf("an unset backend should read back as auto, got %q", s.CredentialsBackend)
	}
	if s.SettingsPath == "" {
		t.Error("SettingsPath should be reported so the user can find the file")
	}

	if err := a.SettingsSet(dir, "file", false); err != nil {
		t.Fatalf("SettingsSet: %v", err)
	}
	s2, _ := a.SettingsGet()
	if s2.CredentialsBackend != "file" {
		t.Errorf("backend = %q, want file", s2.CredentialsBackend)
	}
	if s2.RequireClosedSessions {
		t.Error("RequireClosedSessions should have been turned off")
	}
	if s2.ClaudeConfigDir != dir {
		t.Errorf("ClaudeConfigDir = %q, want %q", s2.ClaudeConfigDir, dir)
	}
}

func TestSettingsRejectsBadInput(t *testing.T) {
	newEnv(t)
	a := &api{}

	if err := a.SettingsSet(t.TempDir(), "auto", true); err == nil {
		t.Error("a directory with no Claude Code files should be rejected; accepting it would " +
			"produce switches that appear to work and change nothing")
	}
	if err := a.SettingsSet("", "sqlite", true); err == nil {
		t.Error("an unknown credentials backend should be rejected")
	}
}

// Turning a shortcut off has to work when the desktop app cannot be found,
// because that is exactly the state an uninstall leaves: the executables are
// gone and the shortcuts pointing at them still need clearing. Turning one on
// in the same state must fail loudly instead of writing a link to nothing.
func TestShortcutSetRemovesWithoutTheDesktopApp(t *testing.T) {
	root := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("USERPROFILE", root)
		t.Setenv("APPDATA", filepath.Join(root, "AppData", "Roaming"))
	default:
		t.Setenv("HOME", root)
		t.Setenv("XDG_DESKTOP_DIR", filepath.Join(root, "Desktop"))
		t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".local", "share"))
	}

	a := &api{}

	// The test binary has no ccm-gui beside it, so the app is not locatable.
	if err := a.ShortcutSet("desktop", false); err != nil {
		t.Errorf("removing a shortcut with no desktop app installed should succeed, got %v", err)
	}

	err := a.ShortcutSet("desktop", true)
	if err == nil {
		t.Error("adding a shortcut with no desktop app installed should fail")
	} else if !errors.Is(err, shortcut.ErrNoDesktopApp) {
		t.Errorf("got %v, want ErrNoDesktopApp", err)
	}
}

// A mismatch between a Go json tag and the property ui.html reads renders a
// blank field with no error anywhere. This pins the contract from both sides.
func TestJSONContractMatchesThePage(t *testing.T) {
	html, err := os.ReadFile("ui.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(html)

	check := func(label string, v any, fields []string) {
		blob, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		var m map[string]any
		if err := json.Unmarshal(blob, &m); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		for _, f := range fields {
			if _, ok := m[f]; !ok {
				t.Errorf("%s: ui.html reads %q but the Go struct does not emit it", label, f)
			}
		}
	}

	check("Overview", Overview{}, []string{
		"profiles", "loggedIn", "email", "organization", "plan",
		"activeName", "configDir", "runningCode", "runningUnknown", "runningError",
	})
	check("Profile", Profile{}, []string{"name", "email", "organization", "plan", "active", "expiry", "expired"})
	check("SwitchResult", SwitchResult{}, []string{
		"switched", "to", "toEmail", "capturedAs", "newProfile",
		"blocked", "blockedCount", "blockedPids", "undetermined", "message",
	})
	check("Settings", Settings{}, []string{
		"claudeConfigDir", "credentialsBackend", "requireClosedSessions", "settingsPath",
		"autostart", "autostartAvailable", "autostartMechanism",
		"desktopShortcut", "menuShortcut", "menuSupported", "shortcutAvailable",
	})

	// And the reverse direction: every goX function the page calls must be
	// bound in main.go, or the call rejects with "not a function" at runtime.
	called := regexp.MustCompile(`window\.(go[A-Za-z]+)\(`).FindAllStringSubmatch(page, -1)
	if len(called) == 0 {
		t.Fatal("found no window.goX calls in ui.html; the regex or the page changed")
	}
	main, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, m := range called {
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		if !strings.Contains(string(main), `"`+name+`"`) {
			t.Errorf("ui.html calls window.%s but main.go never binds it", name)
		}
	}
}
