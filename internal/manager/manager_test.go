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

	// Real Claude Code processes are almost certainly running on a developer
	// machine, and that check is not what these tests exercise.
	settings := `{"claudeConfigDir":` + quote(claudeDir) + `,"requireClosedSessions":false}`
	if err := os.WriteFile(filepath.Join(ccmHome, "config.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	return claudeDir
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
