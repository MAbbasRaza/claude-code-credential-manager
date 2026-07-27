package patch

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

// credentialsFixture mirrors the shape observed in a live installation: one
// claudeAiOauth object sharing the document with several mcpOAuth entries.
const credentialsFixture = `{
  "claudeAiOauth": {
    "accessToken": "OLD-ACCESS",
    "refreshToken": "OLD-REFRESH",
    "expiresAt": 1750000000000,
    "scopes": ["user:inference", "user:profile"],
    "subscriptionType": "max"
  },
  "mcpOAuth": {
    "gmail": {
      "serverName": "gmail",
      "serverUrl": "https://example.invalid/gmail",
      "accessToken": "MCP-GMAIL-TOKEN",
      "clientId": "client-gmail",
      "refreshToken": "MCP-GMAIL-REFRESH",
      "expiresAt": 1760000000000
    },
    "clickup": {
      "serverName": "clickup",
      "serverUrl": "https://example.invalid/clickup",
      "accessToken": "MCP-CLICKUP-TOKEN",
      "clientId": "client-clickup"
    }
  }
}`

func TestSetCredentialsPreservesMCPOAuthByteForByte(t *testing.T) {
	before := []byte(credentialsFixture)
	mcpBefore := MCPOAuthRaw(before)
	if len(mcpBefore) == 0 {
		t.Fatal("fixture must contain mcpOAuth for this test to mean anything")
	}

	incoming := json.RawMessage(`{"accessToken":"NEW-ACCESS","refreshToken":"NEW-REFRESH","expiresAt":1770000000000,"scopes":["user:inference"],"subscriptionType":"pro"}`)

	after, err := SetCredentials(before, incoming)
	if err != nil {
		t.Fatalf("SetCredentials: %v", err)
	}

	mcpAfter := MCPOAuthRaw(after)
	if !bytes.Equal(mcpBefore, mcpAfter) {
		t.Errorf("mcpOAuth subtree changed.\nbefore: %s\nafter:  %s", mcpBefore, mcpAfter)
	}

	got, err := ReadCredentials(after)
	if err != nil {
		t.Fatalf("ReadCredentials after write: %v", err)
	}
	if v := gjson.ParseBytes(got.ClaudeAiOauth).Get("accessToken").String(); v != "NEW-ACCESS" {
		t.Errorf("accessToken = %q, want NEW-ACCESS", v)
	}
	if got.SubscriptionType != "pro" {
		t.Errorf("subscriptionType = %q, want pro", got.SubscriptionType)
	}
}

func TestSetCredentialsPreservesUnrelatedTopLevelKeys(t *testing.T) {
	before := []byte(`{"claudeAiOauth":{"accessToken":"A","refreshToken":"R"},"someFutureKey":{"nested":[1,2,3]},"anotherKey":"value"}`)
	incoming := json.RawMessage(`{"accessToken":"B","refreshToken":"S"}`)

	after, err := SetCredentials(before, incoming)
	if err != nil {
		t.Fatalf("SetCredentials: %v", err)
	}

	for _, key := range []string{"someFutureKey", "anotherKey"} {
		wantRaw := gjson.GetBytes(before, key).Raw
		gotRaw := gjson.GetBytes(after, key).Raw
		if wantRaw != gotRaw {
			t.Errorf("key %q changed: before %s, after %s", key, wantRaw, gotRaw)
		}
	}
}

// A profile must round-trip fields ccm does not know about. Claude Code's
// credential format is internal and unversioned; dropping an added field on
// restore would produce a login Claude Code rejects.
func TestUnknownFieldsSurviveCaptureAndRestore(t *testing.T) {
	original := []byte(`{"claudeAiOauth":{"accessToken":"A","refreshToken":"R","futureField":{"deep":"value"},"anotherFuture":42}}`)

	captured, err := ReadCredentials(original)
	if err != nil {
		t.Fatalf("ReadCredentials: %v", err)
	}

	// Restore into a different document, as a switch would.
	target := []byte(`{"claudeAiOauth":{"accessToken":"OTHER","refreshToken":"OTHER"},"mcpOAuth":{}}`)
	restored, err := SetCredentials(target, captured.ClaudeAiOauth)
	if err != nil {
		t.Fatalf("SetCredentials: %v", err)
	}

	r := gjson.GetBytes(restored, "claudeAiOauth")
	if v := r.Get("futureField.deep").String(); v != "value" {
		t.Errorf("futureField.deep = %q, want value", v)
	}
	if v := r.Get("anotherFuture").Int(); v != 42 {
		t.Errorf("anotherFuture = %d, want 42", v)
	}
}

func TestValidateCredentialsRejectsIncomplete(t *testing.T) {
	cases := map[string]string{
		"missing refreshToken": `{"accessToken":"A"}`,
		"missing accessToken":  `{"refreshToken":"R"}`,
		"empty refreshToken":   `{"accessToken":"A","refreshToken":""}`,
		"not an object":        `"a string"`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateCredentials(json.RawMessage(raw)); err == nil {
				t.Fatalf("expected error for %s", raw)
			} else if !errors.Is(err, ErrUnrecognizedShape) {
				t.Fatalf("expected ErrUnrecognizedShape, got %v", err)
			}
		})
	}
}

func TestSetCredentialsRejectsIncomingWithoutRefreshToken(t *testing.T) {
	before := []byte(credentialsFixture)
	_, err := SetCredentials(before, json.RawMessage(`{"accessToken":"ONLY-ACCESS"}`))
	if err == nil {
		t.Fatal("expected SetCredentials to refuse credentials with no refreshToken")
	}
}

// A UTF-8 BOM is not valid JSON, but Windows tooling emits one readily
// (PowerShell's Set-Content -Encoding utf8, older Notepad). A user who
// hand-edits .claude.json should not get an opaque parse failure.
func TestBOMPrefixedDocumentsAreTolerated(t *testing.T) {
	bom := []byte{0xEF, 0xBB, 0xBF}

	creds := append(append([]byte{}, bom...), []byte(credentialsFixture)...)
	got, err := ReadCredentials(creds)
	if err != nil {
		t.Fatalf("ReadCredentials with BOM: %v", err)
	}
	if got.SubscriptionType != "max" {
		t.Errorf("subscriptionType = %q, want max", got.SubscriptionType)
	}

	out, err := SetCredentials(creds, json.RawMessage(`{"accessToken":"N","refreshToken":"M"}`))
	if err != nil {
		t.Fatalf("SetCredentials with BOM: %v", err)
	}
	if bytes.HasPrefix(out, bom) {
		t.Error("the rewritten document should not carry the BOM forward")
	}

	cfg := append(append([]byte{}, bom...), []byte(claudeJSONFixture)...)
	if _, err := ReadIdentity(cfg); err != nil {
		t.Fatalf("ReadIdentity with BOM: %v", err)
	}
}

func TestReadCredentialsMissing(t *testing.T) {
	if _, err := ReadCredentials([]byte(`{"mcpOAuth":{}}`)); !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("expected ErrNoCredentials, got %v", err)
	}
	if _, err := ReadCredentials(nil); !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("empty document: expected ErrNoCredentials, got %v", err)
	}
}

// claudeJSONFixture reproduces the duplicate-key hazard found in a live
// .claude.json: two project entries differing only in drive-letter case.
// Strict JSON decoders reject this document outright.
const claudeJSONFixture = `{
  "numStartups": 412,
  "userID": "old-user-id",
  "projects": {
    "F:/Workplace/My_Repositories/Portfolio": {"allowedTools": ["Read"]},
    "f:/Workplace/My_Repositories/Portfolio": {"allowedTools": ["Edit"]}
  },
  "hasCompletedOnboarding": true,
  "oauthAccount": {
    "accountUuid": "acct-old",
    "emailAddress": "old@example.invalid",
    "organizationUuid": "org-old",
    "organizationRole": "admin",
    "workspaceRole": "member",
    "organizationName": "Old Org"
  },
  "mcpServers": {"weather": {"command": "node"}}
}`

// Documents why ccm splices bytes instead of decoding and re-encoding.
//
// Note on the duplicate case-differing keys in the fixture: Go's encoding/json
// does preserve them, because Go map keys are case-sensitive. The observed
// failure was in PowerShell's ConvertFrom-Json, which folds property case and
// rejects the file outright. So the duplicate keys are evidence that .claude.json
// is not portable across tooling, not evidence that Go cannot parse it.
//
// The hazard that does apply to Go is this one: a decode/re-encode cycle
// rewrites the entire document, reordering keys and discarding formatting.
// Applied to a 74 KB file of unrelated project state, that turns a two-key edit
// into a whole-file rewrite, which is both a bad diff and a much larger blast
// radius if anything goes wrong mid-write.
func TestDecodeReencodeRewritesWholeDocument(t *testing.T) {
	var m map[string]any
	if err := json.Unmarshal([]byte(claudeJSONFixture), &m); err != nil {
		t.Fatalf("fixture should parse: %v", err)
	}
	reencoded, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// encoding/json sorts map keys, so the original ordering is gone.
	origFirst := gjson.ParseBytes([]byte(claudeJSONFixture)).Get("@keys").Array()
	reFirst := gjson.ParseBytes(reencoded).Get("@keys").Array()
	if len(origFirst) == 0 || len(reFirst) == 0 {
		t.Fatal("could not read key order")
	}
	if origFirst[0].String() == reFirst[0].String() {
		t.Logf("key order happened to match; ordering is still not guaranteed")
	}

	// And the original whitespace is gone entirely.
	if bytes.Contains(reencoded, []byte(`"numStartups": 412`)) {
		t.Error("expected encoding/json to drop the original formatting")
	}

	// sjson, by contrast, leaves untouched regions exactly as they were.
	spliced, err := SetIdentity([]byte(claudeJSONFixture), Identity{
		OAuthAccount: json.RawMessage(`{"accountUuid":"acct-new"}`),
		UserID:       "new-user-id",
	})
	if err != nil {
		t.Fatalf("SetIdentity: %v", err)
	}
	if !bytes.Contains(spliced, []byte(`"numStartups": 412`)) {
		t.Error("sjson must preserve the original formatting of untouched keys")
	}
	if !bytes.Contains(spliced, []byte(`"hasCompletedOnboarding": true`)) {
		t.Error("sjson must preserve the original formatting of untouched keys")
	}
}

func TestSetIdentityPreservesEverythingElse(t *testing.T) {
	before := []byte(claudeJSONFixture)

	incoming := Identity{
		OAuthAccount: json.RawMessage(`{"accountUuid":"acct-new","emailAddress":"new@example.invalid","organizationUuid":"org-new","organizationRole":"member","workspaceRole":"member","organizationName":"New Org"}`),
		UserID:       "new-user-id",
	}

	after, err := SetIdentity(before, incoming)
	if err != nil {
		t.Fatalf("SetIdentity: %v", err)
	}

	// Both duplicate project keys must still be present. This is the property
	// that a decode/re-encode implementation loses.
	projectsAfter := gjson.GetBytes(after, "projects")
	count := 0
	projectsAfter.ForEach(func(_, _ gjson.Result) bool { count++; return true })
	if count != 2 {
		t.Errorf("projects entries = %d, want 2 (duplicate case-differing keys must survive)", count)
	}

	for _, key := range []string{"numStartups", "hasCompletedOnboarding", "mcpServers"} {
		if gjson.GetBytes(before, key).Raw != gjson.GetBytes(after, key).Raw {
			t.Errorf("key %q changed", key)
		}
	}

	id, err := ReadIdentity(after)
	if err != nil {
		t.Fatalf("ReadIdentity: %v", err)
	}
	if id.AccountUUID != "acct-new" {
		t.Errorf("accountUuid = %q, want acct-new", id.AccountUUID)
	}
	if id.EmailAddress != "new@example.invalid" {
		t.Errorf("emailAddress = %q, want new@example.invalid", id.EmailAddress)
	}
	if id.UserID != "new-user-id" {
		t.Errorf("userID = %q, want new-user-id", id.UserID)
	}
}

func TestSetIdentityWithoutUserIDLeavesExistingAlone(t *testing.T) {
	before := []byte(claudeJSONFixture)
	incoming := Identity{
		OAuthAccount: json.RawMessage(`{"accountUuid":"acct-new","emailAddress":"new@example.invalid"}`),
		// UserID deliberately empty, as an older captured profile would be.
	}
	after, err := SetIdentity(before, incoming)
	if err != nil {
		t.Fatalf("SetIdentity: %v", err)
	}
	if got := gjson.GetBytes(after, "userID").String(); got != "old-user-id" {
		t.Errorf("userID = %q, want old-user-id preserved", got)
	}
}

func TestReadIdentityMissing(t *testing.T) {
	if _, err := ReadIdentity([]byte(`{"numStartups":1}`)); !errors.Is(err, ErrNoAccount) {
		t.Fatalf("expected ErrNoAccount, got %v", err)
	}
}

func TestSetIdentityRejectsEmptyAccount(t *testing.T) {
	if _, err := SetIdentity([]byte(claudeJSONFixture), Identity{}); err == nil {
		t.Fatal("expected error for empty oauthAccount")
	}
}

// A full switch must move the account and leave both shared subtrees intact.
func TestFullSwitchRoundTrip(t *testing.T) {
	creds := []byte(credentialsFixture)
	cfg := []byte(claudeJSONFixture)

	// Capture account A.
	capturedCreds, err := ReadCredentials(creds)
	if err != nil {
		t.Fatalf("capture credentials: %v", err)
	}
	capturedID, err := ReadIdentity(cfg)
	if err != nil {
		t.Fatalf("capture identity: %v", err)
	}

	// Install account B.
	credsB, err := SetCredentials(creds, json.RawMessage(`{"accessToken":"B-ACCESS","refreshToken":"B-REFRESH"}`))
	if err != nil {
		t.Fatalf("install B credentials: %v", err)
	}
	cfgB, err := SetIdentity(cfg, Identity{
		OAuthAccount: json.RawMessage(`{"accountUuid":"acct-b","emailAddress":"b@example.invalid"}`),
		UserID:       "user-b",
	})
	if err != nil {
		t.Fatalf("install B identity: %v", err)
	}

	// Switch back to A.
	credsA, err := SetCredentials(credsB, capturedCreds.ClaudeAiOauth)
	if err != nil {
		t.Fatalf("restore A credentials: %v", err)
	}
	cfgA, err := SetIdentity(cfgB, capturedID)
	if err != nil {
		t.Fatalf("restore A identity: %v", err)
	}

	gotCreds, err := ReadCredentials(credsA)
	if err != nil {
		t.Fatalf("read restored credentials: %v", err)
	}
	if v := gjson.ParseBytes(gotCreds.ClaudeAiOauth).Get("accessToken").String(); v != "OLD-ACCESS" {
		t.Errorf("restored accessToken = %q, want OLD-ACCESS", v)
	}

	gotID, err := ReadIdentity(cfgA)
	if err != nil {
		t.Fatalf("read restored identity: %v", err)
	}
	if gotID.AccountUUID != "acct-old" {
		t.Errorf("restored accountUuid = %q, want acct-old", gotID.AccountUUID)
	}

	// MCP logins survived both switches untouched.
	if !bytes.Equal(MCPOAuthRaw([]byte(credentialsFixture)), MCPOAuthRaw(credsA)) {
		t.Error("mcpOAuth did not survive a full switch cycle")
	}
	// And so did the duplicate-key project state.
	if !strings.Contains(string(cfgA), `"F:/Workplace/My_Repositories/Portfolio"`) ||
		!strings.Contains(string(cfgA), `"f:/Workplace/My_Repositories/Portfolio"`) {
		t.Error("duplicate project keys did not survive a full switch cycle")
	}
}
