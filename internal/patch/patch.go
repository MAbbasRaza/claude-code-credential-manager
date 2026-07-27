// Package patch performs key-scoped edits on Claude Code's two auth documents.
//
// Both documents are shared with state that has nothing to do with the signed-in
// account, so neither may be replaced wholesale:
//
//   - .credentials.json holds one "claudeAiOauth" object alongside "mcpOAuth",
//     a map of per-MCP-server logins. Overwriting the file re-authorizes every
//     connector the user has ever linked.
//   - .claude.json holds "oauthAccount" and "userID" inside tens of thousands of
//     lines of per-project history, MCP configuration and trust decisions.
//
// Every edit therefore splices raw bytes with sjson rather than decoding and
// re-encoding. Beyond preserving formatting, this avoids two concrete hazards:
// a decode/re-encode cycle silently drops keys future Claude Code versions add,
// and .claude.json is not guaranteed to satisfy strict decoders. A live file was
// observed containing duplicate keys differing only in drive-letter case
// ("F:/..." and "f:/..."), which makes strict parsers reject the document
// outright. gjson and sjson operate on the text and tolerate it.
package patch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Keys ccm reads and writes. Everything else in both documents is untouched.
const (
	KeyClaudeAiOauth = "claudeAiOauth"
	KeyMCPOAuth      = "mcpOAuth"
	KeyOAuthAccount  = "oauthAccount"
	KeyUserID        = "userID"
)

// ErrNoCredentials means the credentials document has no claudeAiOauth object,
// so no subscription login is currently active.
var ErrNoCredentials = errors.New("no claudeAiOauth object in credentials document")

// ErrNoAccount means .claude.json has no oauthAccount object.
var ErrNoAccount = errors.New("no oauthAccount object in .claude.json")

// ErrUnrecognizedShape means a document parsed but did not look like what
// Claude Code writes. These formats are internal and unversioned, so ccm fails
// loudly here instead of writing something Claude Code cannot read.
var ErrUnrecognizedShape = errors.New("unrecognized document shape")

// Identity is the account-scoped slice of .claude.json.
type Identity struct {
	// OAuthAccount is the oauthAccount object verbatim. Stored as raw JSON so
	// fields Anthropic adds later survive a capture and restore cycle.
	OAuthAccount json.RawMessage `json:"oauthAccount"`
	// UserID is the top-level userID string.
	UserID string `json:"userID"`

	// Derived, for display and for matching a live login to a stored profile.
	AccountUUID      string `json:"accountUuid"`
	EmailAddress     string `json:"emailAddress"`
	OrganizationName string `json:"organizationName"`
	OrganizationUUID string `json:"organizationUuid"`
}

// Credentials is the account-scoped slice of the credentials document.
type Credentials struct {
	// ClaudeAiOauth is the claudeAiOauth object verbatim.
	ClaudeAiOauth json.RawMessage `json:"claudeAiOauth"`

	// Derived, for display only.
	ExpiresAt        int64    `json:"expiresAt"`
	SubscriptionType string   `json:"subscriptionType"`
	Scopes           []string `json:"scopes"`
}

// utf8BOM is emitted by several Windows tools, including PowerShell's
// Set-Content -Encoding utf8 and older Notepad. A BOM is not legal JSON, so a
// hand-edited config file would otherwise fail with a confusing "not valid
// JSON" error. Stripping it on read is cheap and the rewritten file simply
// loses the BOM, which is what every JSON consumer wants anyway.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// emptyDoc normalizes a document that is absent, blank, or BOM-prefixed.
func emptyDoc(b []byte) []byte {
	b = bytes.TrimPrefix(b, utf8BOM)
	if len(trimSpace(b)) == 0 {
		return []byte("{}")
	}
	return b
}

func trimSpace(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && isSpace(b[i]) {
		i++
	}
	for j > i && isSpace(b[j-1]) {
		j--
	}
	return b[i:j]
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// ReadCredentials extracts claudeAiOauth from a credentials document.
func ReadCredentials(blob []byte) (Credentials, error) {
	blob = emptyDoc(blob)
	if !gjson.ValidBytes(blob) {
		return Credentials{}, fmt.Errorf("%w: credentials document is not valid JSON", ErrUnrecognizedShape)
	}
	r := gjson.GetBytes(blob, KeyClaudeAiOauth)
	if !r.Exists() {
		return Credentials{}, ErrNoCredentials
	}
	if !r.IsObject() {
		return Credentials{}, fmt.Errorf("%w: %s is not an object", ErrUnrecognizedShape, KeyClaudeAiOauth)
	}
	c := Credentials{
		ClaudeAiOauth:    json.RawMessage(r.Raw),
		ExpiresAt:        r.Get("expiresAt").Int(),
		SubscriptionType: r.Get("subscriptionType").String(),
	}
	for _, s := range r.Get("scopes").Array() {
		c.Scopes = append(c.Scopes, s.String())
	}
	return c, nil
}

// ValidateCredentials checks that a claudeAiOauth object carries the fields
// Claude Code needs. Writing a profile that lacks a refresh token would look
// like a successful switch and then force a browser login on the next request.
func ValidateCredentials(raw json.RawMessage) error {
	if !gjson.ValidBytes(raw) {
		return fmt.Errorf("%w: claudeAiOauth is not valid JSON", ErrUnrecognizedShape)
	}
	r := gjson.ParseBytes(raw)
	if !r.IsObject() {
		return fmt.Errorf("%w: claudeAiOauth is not an object", ErrUnrecognizedShape)
	}
	for _, f := range []string{"accessToken", "refreshToken"} {
		v := r.Get(f)
		if !v.Exists() || v.String() == "" {
			return fmt.Errorf("%w: claudeAiOauth is missing %s", ErrUnrecognizedShape, f)
		}
	}
	return nil
}

// SetCredentials replaces claudeAiOauth and leaves every other key, including
// the entire mcpOAuth subtree, byte-identical.
func SetCredentials(blob []byte, raw json.RawMessage) ([]byte, error) {
	if err := ValidateCredentials(raw); err != nil {
		return nil, err
	}
	blob = emptyDoc(blob)
	if !gjson.ValidBytes(blob) {
		return nil, fmt.Errorf("%w: credentials document is not valid JSON", ErrUnrecognizedShape)
	}
	out, err := sjson.SetRawBytes(blob, KeyClaudeAiOauth, raw)
	if err != nil {
		return nil, fmt.Errorf("set %s: %w", KeyClaudeAiOauth, err)
	}
	return out, nil
}

// ReadIdentity extracts oauthAccount and userID from .claude.json.
func ReadIdentity(cfg []byte) (Identity, error) {
	cfg = emptyDoc(cfg)
	if !gjson.ValidBytes(cfg) {
		return Identity{}, fmt.Errorf("%w: .claude.json is not valid JSON", ErrUnrecognizedShape)
	}
	acct := gjson.GetBytes(cfg, KeyOAuthAccount)
	if !acct.Exists() {
		return Identity{}, ErrNoAccount
	}
	if !acct.IsObject() {
		return Identity{}, fmt.Errorf("%w: %s is not an object", ErrUnrecognizedShape, KeyOAuthAccount)
	}
	return Identity{
		OAuthAccount:     json.RawMessage(acct.Raw),
		UserID:           gjson.GetBytes(cfg, KeyUserID).String(),
		AccountUUID:      acct.Get("accountUuid").String(),
		EmailAddress:     acct.Get("emailAddress").String(),
		OrganizationName: acct.Get("organizationName").String(),
		OrganizationUUID: acct.Get("organizationUuid").String(),
	}, nil
}

// SetIdentity replaces oauthAccount and userID and leaves everything else in
// .claude.json byte-identical.
//
// userID is only written when the profile carries one, so a profile captured
// before ccm tracked it cannot blank out a value Claude Code is using.
func SetIdentity(cfg []byte, id Identity) ([]byte, error) {
	if len(id.OAuthAccount) == 0 {
		return nil, fmt.Errorf("%w: profile has no oauthAccount", ErrUnrecognizedShape)
	}
	if !gjson.ValidBytes(id.OAuthAccount) {
		return nil, fmt.Errorf("%w: oauthAccount is not valid JSON", ErrUnrecognizedShape)
	}
	cfg = emptyDoc(cfg)
	if !gjson.ValidBytes(cfg) {
		return nil, fmt.Errorf("%w: .claude.json is not valid JSON", ErrUnrecognizedShape)
	}

	out, err := sjson.SetRawBytes(cfg, KeyOAuthAccount, id.OAuthAccount)
	if err != nil {
		return nil, fmt.Errorf("set %s: %w", KeyOAuthAccount, err)
	}
	if id.UserID != "" {
		out, err = sjson.SetBytes(out, KeyUserID, id.UserID)
		if err != nil {
			return nil, fmt.Errorf("set %s: %w", KeyUserID, err)
		}
	}
	return out, nil
}

// MCPOAuthRaw returns the mcpOAuth subtree, or nil when absent.
// Used by tests and `ccm doctor` to prove a switch left connector logins alone.
func MCPOAuthRaw(blob []byte) []byte {
	blob = emptyDoc(blob)
	r := gjson.GetBytes(blob, KeyMCPOAuth)
	if !r.Exists() {
		return nil
	}
	return []byte(r.Raw)
}
