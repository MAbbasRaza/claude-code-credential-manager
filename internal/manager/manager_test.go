package manager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MAbbasRaza/claude-code-credential-manager/internal/patch"
	"github.com/tidwall/gjson"
)

// credsDoc builds a credentials document for one account, always carrying the
// same mcpOAuth subtree so tests can assert it is never disturbed.
func credsDoc(access, refresh string) string {
	return `{
  "claudeAiOauth": {
    "accessToken": "` + access + `",
    "refreshToken": "` + refresh + `",
    "expiresAt": 1750000000000,
    "scopes": ["user:inference", "user:profile"],
    "subscriptionType": "max"
  },
  "mcpOAuth": {
    "gmail": {"serverName":"gmail","serverUrl":"https://example.invalid/g","accessToken":"MCP-G","clientId":"cg"},
    "vercel": {"serverName":"vercel","serverUrl":"https://example.invalid/v","accessToken":"MCP-V","clientId":"cv"}
  }
}`
}

func configDoc(uuid, email string) string {
	return `{
  "numStartups": 7,
  "userID": "user-` + uuid + `",
  "projects": {
    "F:/Repos/Portfolio": {"allowedTools":["Read"]},
    "f:/Repos/Portfolio": {"allowedTools":["Edit"]}
  },
  "hasCompletedOnboarding": true,
  "oauthAccount": {
    "accountUuid": "` + uuid + `",
    "emailAddress": "` + email + `",
    "organizationUuid": "org-` + uuid + `",
    "organizationRole": "admin",
    "workspaceRole": "member",
    "organizationName": "Org ` + uuid + `"
  },
  "mcpServers": {"weather": {"command": "node"}}
}`
}

// newEnv wires ccm entirely inside temp directories: a scratch Claude Code
// installation with synthetic tokens, plus an isolated CCM_HOME so the vault
// and settings never touch the developer's real state.
func newEnv(t *testing.T) (claudeDir string) {
	t.Helper()

	claudeDir = t.TempDir()
	ccmHome := t.TempDir()

	t.Setenv("CLAUDE_CONFIG_DIR", claudeDir)
	t.Setenv("CCM_CLAUDE_CONFIG_DIR", "")
	t.Setenv("CCM_HOME", ccmHome)

	// Force the file backend. Without this these tests pass on Windows and
	// Linux but fail on macOS, where the default backend is the Keychain: the
	// synthetic .credentials.json written below would be ignored and the
	// manager would read the developer's real Keychain instead. CI caught
	// exactly that.
	t.Setenv("CCM_CREDENTIALS_BACKEND", "file")

	// Real Claude Code processes are almost certainly running on a developer
	// machine, and that check is not what these tests exercise.
	settings := `{"claudeConfigDir":` + quote(claudeDir) + `,"requireClosedSessions":false}`
	if err := os.WriteFile(filepath.Join(ccmHome, "config.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	return claudeDir
}

// homeDirFor reports the CCM_HOME newEnv installed, for tests that need to
// rewrite the settings file.
func homeDirFor(t *testing.T) string {
	t.Helper()
	return os.Getenv("CCM_HOME")
}

// writeSettings replaces the settings file, chiefly to flip the
// running-session guard that newEnv disables.
func writeSettings(t *testing.T, ccmHome, claudeDir string, requireClosed bool) {
	t.Helper()
	body := `{"claudeConfigDir":` + quote(claudeDir) +
		`,"requireClosedSessions":` + map[bool]string{true: "true", false: "false"}[requireClosed] + `}`
	if err := os.WriteFile(filepath.Join(ccmHome, "config.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func quote(s string) string {
	b, _ := jsonMarshalString(s)
	return b
}

func jsonMarshalString(s string) (string, error) {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			sb.WriteString(`\\`)
		case '"':
			sb.WriteString(`\"`)
		default:
			sb.WriteRune(r)
		}
	}
	sb.WriteByte('"')
	return sb.String(), nil
}

func writeLive(t *testing.T, dir, creds, cfg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(creds), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readLiveFiles(t *testing.T, dir string) (creds, cfg []byte) {
	t.Helper()
	var err error
	creds, err = os.ReadFile(filepath.Join(dir, ".credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = os.ReadFile(filepath.Join(dir, ".claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	return creds, cfg
}

func TestSwitchRoundTripPreservesSharedState(t *testing.T) {
	dir := newEnv(t)

	// Account A is signed in and gets captured.
	writeLive(t, dir, credsDoc("A-ACCESS", "A-REFRESH"), configDoc("acct-a", "a@example.invalid"))
	mcpBefore := patch.MCPOAuthRaw([]byte(credsDoc("A-ACCESS", "A-REFRESH")))

	m, err := Open("")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := m.Capture("work"); err != nil {
		t.Fatalf("Capture work: %v", err)
	}

	// The user signs out and into account B, as /login would do.
	writeLive(t, dir, credsDoc("B-ACCESS", "B-REFRESH"), configDoc("acct-b", "b@example.invalid"))

	m2, err := Open("")
	if err != nil {
		t.Fatalf("Open 2: %v", err)
	}
	res, err := m2.Switch("work", false)
	if err != nil {
		t.Fatalf("Switch to work: %v", err)
	}
	if !res.Switched {
		t.Fatal("Switch reported no change")
	}
	// B was not in the vault, so it must have been parked rather than lost.
	if !res.CapturedNew || res.CapturedAs == "" {
		t.Errorf("outgoing account B should have been captured as a new profile, got %+v", res)
	}

	creds, cfg := readLiveFiles(t, dir)

	if got := gjson.GetBytes(creds, "claudeAiOauth.accessToken").String(); got != "A-ACCESS" {
		t.Errorf("accessToken = %q, want A-ACCESS", got)
	}
	if got := gjson.GetBytes(cfg, "oauthAccount.accountUuid").String(); got != "acct-a" {
		t.Errorf("accountUuid = %q, want acct-a", got)
	}
	if got := gjson.GetBytes(cfg, "userID").String(); got != "user-acct-a" {
		t.Errorf("userID = %q, want user-acct-a", got)
	}

	// The two shared subtrees must be untouched.
	if string(mcpBefore) != string(patch.MCPOAuthRaw(creds)) {
		t.Error("mcpOAuth changed during a switch; MCP connector logins would be lost")
	}
	if !strings.Contains(string(cfg), `"F:/Repos/Portfolio"`) || !strings.Contains(string(cfg), `"f:/Repos/Portfolio"`) {
		t.Error("duplicate case-differing project keys did not survive the switch")
	}
	for _, key := range []string{"numStartups", "hasCompletedOnboarding", "mcpServers"} {
		if gjson.GetBytes(cfg, key).Raw == "" {
			t.Errorf("unrelated key %q was lost", key)
		}
	}
}

// The property that makes the tool survive real use. Claude Code refreshes the
// access token in the background and rotates the refresh token with it. A
// switcher that only stores a snapshot at add time hands back a dead token the
// second time you use it. Capturing on the way out is what prevents that.
func TestCaptureOnSwitchPicksUpBackgroundRefresh(t *testing.T) {
	dir := newEnv(t)

	writeLive(t, dir, credsDoc("A-ACCESS", "A-REFRESH"), configDoc("acct-a", "a@example.invalid"))
	m, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Capture("work"); err != nil {
		t.Fatal(err)
	}

	writeLive(t, dir, credsDoc("B-ACCESS", "B-REFRESH"), configDoc("acct-b", "b@example.invalid"))
	m2, _ := Open("")
	if _, err := m2.Capture("personal"); err != nil {
		t.Fatal(err)
	}

	// Claude Code refreshes account B in the background. The vault copy of
	// "personal" is now stale, holding a refresh token the server has rotated.
	writeLive(t, dir, credsDoc("B-ACCESS-V2", "B-REFRESH-V2"), configDoc("acct-b", "b@example.invalid"))

	m3, _ := Open("")
	if _, err := m3.Switch("work", false); err != nil {
		t.Fatalf("switch to work: %v", err)
	}

	// Switching away must have re-read B's live tokens into "personal".
	m4, _ := Open("")
	p, err := m4.Vault.Get("personal")
	if err != nil {
		t.Fatalf("get personal: %v", err)
	}
	if got := gjson.ParseBytes(p.ClaudeAiOauth).Get("refreshToken").String(); got != "B-REFRESH-V2" {
		t.Errorf("stored refreshToken = %q, want B-REFRESH-V2; the refreshed token was not captured", got)
	}

	// And switching back must install the refreshed token, not the stale one.
	if _, err := m4.Switch("personal", false); err != nil {
		t.Fatalf("switch back to personal: %v", err)
	}
	creds, _ := readLiveFiles(t, dir)
	if got := gjson.GetBytes(creds, "claudeAiOauth.refreshToken").String(); got != "B-REFRESH-V2" {
		t.Errorf("installed refreshToken = %q, want B-REFRESH-V2", got)
	}
}

// Regression: one account must map to exactly one profile.
//
// Saving an account twice looks harmless and is easy to do by accident, but it
// silently breaks the tool's central guarantee. Capture-on-switch updates the
// profile matching the live account; with two matches only one is refreshed and
// the other decays into a dead refresh token, which forces the browser
// re-authorization this tool exists to eliminate.
func TestCaptureRefusesDuplicateAccountUnderAnotherName(t *testing.T) {
	dir := newEnv(t)
	writeLive(t, dir, credsDoc("A-ACCESS", "A-REFRESH"), configDoc("acct-a", "a@example.invalid"))

	m, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Capture("work"); err != nil {
		t.Fatalf("first capture: %v", err)
	}

	_, err = m.Capture("work-again")
	if err == nil {
		t.Fatal("expected the second capture of the same account to be refused")
	}
	if !strings.Contains(err.Error(), "work") {
		t.Errorf("error should name the existing profile, got: %v", err)
	}
	if n := len(m.Vault.List()); n != 1 {
		t.Errorf("vault holds %d profiles, want 1", n)
	}
}

func TestCaptureUnderSameNameRefreshesInPlace(t *testing.T) {
	dir := newEnv(t)
	writeLive(t, dir, credsDoc("A-ACCESS", "A-REFRESH"), configDoc("acct-a", "a@example.invalid"))

	m, _ := Open("")
	first, err := m.Capture("work")
	if err != nil {
		t.Fatal(err)
	}
	created := first.CreatedAt

	// Claude Code rotates the token, then the user re-runs `ccm add work`.
	writeLive(t, dir, credsDoc("A-ACCESS-V2", "A-REFRESH-V2"), configDoc("acct-a", "a@example.invalid"))

	m2, _ := Open("")
	second, err := m2.Capture("work")
	if err != nil {
		t.Fatalf("re-capturing under the same name should refresh, not fail: %v", err)
	}
	if n := len(m2.Vault.List()); n != 1 {
		t.Fatalf("vault holds %d profiles, want 1", n)
	}
	if got := gjson.ParseBytes(second.ClaudeAiOauth).Get("refreshToken").String(); got != "A-REFRESH-V2" {
		t.Errorf("refreshToken = %q, want A-REFRESH-V2", got)
	}
	if !second.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt should be preserved across a refresh, was %v now %v", created, second.CreatedAt)
	}
}

// Capturing with no name when the account is already stored must refresh the
// existing profile rather than mint "a-2", "a-3" and so on.
func TestCaptureWithoutNameRefreshesExistingProfile(t *testing.T) {
	dir := newEnv(t)
	writeLive(t, dir, credsDoc("A-ACCESS", "A-REFRESH"), configDoc("acct-a", "a@example.invalid"))

	m, _ := Open("")
	if _, err := m.Capture("work"); err != nil {
		t.Fatal(err)
	}

	m2, _ := Open("")
	p, err := m2.Capture("")
	if err != nil {
		t.Fatalf("unnamed capture of a known account: %v", err)
	}
	if p.Name != "work" {
		t.Errorf("refreshed profile name = %q, want work", p.Name)
	}
	if n := len(m2.Vault.List()); n != 1 {
		t.Errorf("vault holds %d profiles, want 1", n)
	}
}

// The mirror hazard: reusing a name that belongs to a different account would
// discard that account's only stored refresh token.
func TestCaptureRefusesToOverwriteADifferentAccount(t *testing.T) {
	dir := newEnv(t)
	writeLive(t, dir, credsDoc("A-ACCESS", "A-REFRESH"), configDoc("acct-a", "a@example.invalid"))
	m, _ := Open("")
	if _, err := m.Capture("shared"); err != nil {
		t.Fatal(err)
	}

	writeLive(t, dir, credsDoc("B-ACCESS", "B-REFRESH"), configDoc("acct-b", "b@example.invalid"))
	m2, _ := Open("")
	if _, err := m2.Capture("shared"); err == nil {
		t.Fatal("expected reusing a name held by another account to be refused")
	}

	// Account A's tokens must still be intact.
	m3, _ := Open("")
	p, err := m3.Vault.Get("shared")
	if err != nil {
		t.Fatal(err)
	}
	if got := gjson.ParseBytes(p.ClaudeAiOauth).Get("refreshToken").String(); got != "A-REFRESH" {
		t.Errorf("account A's token was clobbered: refreshToken = %q, want A-REFRESH", got)
	}
}

// Exactly one profile may report as active, which is what the CLI marker, the
// tray checkbox and the extension status bar all rely on.
func TestExactlyOneProfileIsActive(t *testing.T) {
	dir := newEnv(t)
	writeLive(t, dir, credsDoc("A-ACCESS", "A-REFRESH"), configDoc("acct-a", "a@example.invalid"))
	m, _ := Open("")
	if _, err := m.Capture("work"); err != nil {
		t.Fatal(err)
	}
	writeLive(t, dir, credsDoc("B-ACCESS", "B-REFRESH"), configDoc("acct-b", "b@example.invalid"))
	m2, _ := Open("")
	if _, err := m2.Capture("personal"); err != nil {
		t.Fatal(err)
	}

	m3, _ := Open("")
	st, err := m3.Status()
	if err != nil {
		t.Fatal(err)
	}
	active := 0
	for _, p := range m3.Vault.List() {
		if p.AccountUUID == st.AccountUUID {
			active++
		}
	}
	if active != 1 {
		t.Errorf("%d profiles match the live account, want exactly 1", active)
	}
	if len(m3.Vault.DuplicateAccounts()) != 0 {
		t.Errorf("vault reports duplicates: %v", m3.Vault.DuplicateAccounts())
	}
}

// A renamed profile must still be usable for switching. This is the end-to-end
// version of the reason rename exists at all: the credentials have to survive
// so the account can be switched back to without a browser sign-in.
func TestRenamedProfileStillSwitches(t *testing.T) {
	dir := newEnv(t)

	writeLive(t, dir, credsDoc("A-ACCESS", "A-REFRESH"), configDoc("acct-a", "a@example.invalid"))
	m, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Capture("work"); err != nil {
		t.Fatal(err)
	}

	m2, _ := Open("")
	if err := m2.Rename("work", "day-job"); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	// Sign in as a different account, then switch back using the new name.
	writeLive(t, dir, credsDoc("B-ACCESS", "B-REFRESH"), configDoc("acct-b", "b@example.invalid"))
	m3, _ := Open("")
	if _, err := m3.Switch("day-job", false); err != nil {
		t.Fatalf("switching to the renamed profile: %v", err)
	}

	creds, cfg := readLiveFiles(t, dir)
	if got := gjson.GetBytes(creds, "claudeAiOauth.accessToken").String(); got != "A-ACCESS" {
		t.Errorf("accessToken = %q, want A-ACCESS", got)
	}
	if got := gjson.GetBytes(cfg, "oauthAccount.accountUuid").String(); got != "acct-a" {
		t.Errorf("accountUuid = %q, want acct-a", got)
	}
}

// After a rename, capture-on-switch must find the profile by account and update
// the renamed entry rather than creating a duplicate under the old identity.
func TestCaptureOnSwitchFollowsARename(t *testing.T) {
	dir := newEnv(t)

	writeLive(t, dir, credsDoc("A-ACCESS", "A-REFRESH"), configDoc("acct-a", "a@example.invalid"))
	m, _ := Open("")
	if _, err := m.Capture("work"); err != nil {
		t.Fatal(err)
	}
	if err := m.Rename("work", "day-job"); err != nil {
		t.Fatal(err)
	}

	writeLive(t, dir, credsDoc("B-ACCESS", "B-REFRESH"), configDoc("acct-b", "b@example.invalid"))
	m2, _ := Open("")
	if _, err := m2.Capture("personal"); err != nil {
		t.Fatal(err)
	}

	// Account A signs back in and its token rotates, then we switch away.
	writeLive(t, dir, credsDoc("A-ACCESS-V2", "A-REFRESH-V2"), configDoc("acct-a", "a@example.invalid"))
	m3, _ := Open("")
	if _, err := m3.Switch("personal", false); err != nil {
		t.Fatalf("switch: %v", err)
	}

	m4, _ := Open("")
	if n := len(m4.Vault.List()); n != 2 {
		t.Errorf("vault holds %d profiles, want 2; a rename should not spawn a duplicate", n)
	}
	p, err := m4.Vault.Get("day-job")
	if err != nil {
		t.Fatalf("renamed profile is gone: %v", err)
	}
	if got := gjson.ParseBytes(p.ClaudeAiOauth).Get("refreshToken").String(); got != "A-REFRESH-V2" {
		t.Errorf("renamed profile refreshToken = %q, want the rotated A-REFRESH-V2", got)
	}
}

func TestSwitchToUnknownProfileFails(t *testing.T) {
	dir := newEnv(t)
	writeLive(t, dir, credsDoc("A-ACCESS", "A-REFRESH"), configDoc("acct-a", "a@example.invalid"))

	m, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Switch("nope", false); err == nil {
		t.Fatal("expected an error switching to an unknown profile")
	}
}

// A failed switch must not leave the installation half-modified.
func TestFailedSwitchLeavesLiveStateUntouched(t *testing.T) {
	dir := newEnv(t)
	writeLive(t, dir, credsDoc("A-ACCESS", "A-REFRESH"), configDoc("acct-a", "a@example.invalid"))

	credsBefore, cfgBefore := readLiveFiles(t, dir)

	m, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Switch("does-not-exist", false); err == nil {
		t.Fatal("expected failure")
	}

	credsAfter, cfgAfter := readLiveFiles(t, dir)
	if string(credsBefore) != string(credsAfter) {
		t.Error("credentials document changed despite a failed switch")
	}
	if string(cfgBefore) != string(cfgAfter) {
		t.Error(".claude.json changed despite a failed switch")
	}
}

func TestBackupIsWrittenBeforeSwitch(t *testing.T) {
	dir := newEnv(t)
	writeLive(t, dir, credsDoc("A-ACCESS", "A-REFRESH"), configDoc("acct-a", "a@example.invalid"))

	m, _ := Open("")
	if _, err := m.Capture("work"); err != nil {
		t.Fatal(err)
	}
	writeLive(t, dir, credsDoc("B-ACCESS", "B-REFRESH"), configDoc("acct-b", "b@example.invalid"))

	m2, _ := Open("")
	res, err := m2.Switch("work", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.BackupDir == "" {
		t.Fatal("no backup directory reported")
	}
	for _, name := range []string{"credentials.json", "claude.json", "meta.json"} {
		if _, err := os.Stat(filepath.Join(res.BackupDir, name)); err != nil {
			t.Errorf("backup missing %s: %v", name, err)
		}
	}
	// The backup must hold the state as it was before the switch, so it can
	// actually be used to recover.
	b, err := os.ReadFile(filepath.Join(res.BackupDir, "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := gjson.GetBytes(b, "claudeAiOauth.accessToken").String(); got != "B-ACCESS" {
		t.Errorf("backup accessToken = %q, want the pre-switch value B-ACCESS", got)
	}
}

func TestStatusReportsActiveProfile(t *testing.T) {
	dir := newEnv(t)
	writeLive(t, dir, credsDoc("A-ACCESS", "A-REFRESH"), configDoc("acct-a", "a@example.invalid"))

	m, _ := Open("")
	if _, err := m.Capture("work"); err != nil {
		t.Fatal(err)
	}

	m2, _ := Open("")
	st, err := m2.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !st.LoggedIn {
		t.Error("expected LoggedIn")
	}
	if st.ActiveProfile != "work" {
		t.Errorf("ActiveProfile = %q, want work", st.ActiveProfile)
	}
	if st.EmailAddress != "a@example.invalid" {
		t.Errorf("EmailAddress = %q", st.EmailAddress)
	}
	if st.Subscription != "max" {
		t.Errorf("Subscription = %q, want max", st.Subscription)
	}
}
