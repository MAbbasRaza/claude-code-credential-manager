// Package vault stores the parked account profiles.
//
// A profile is the account-scoped slice of Claude Code's two auth documents,
// kept as raw JSON so that fields Anthropic adds later survive a capture and
// restore cycle untouched.
//
// Protection rule: the vault is never less protected than Claude Code's own
// credential store on the same platform. Claude Code writes plaintext 0600 on
// Linux and Windows and uses the Keychain on macOS, so ccm uses DPAPI on
// Windows (strictly better), AES-GCM under a Keychain-held key on macOS
// (equivalent), and 0600 on Linux (equivalent).
package vault

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MAbbasRaza/claude-code-multi-account-manager/internal/config"
	"github.com/MAbbasRaza/claude-code-multi-account-manager/internal/patch"
)

// SchemaVersion guards against silently misreading a vault written by a
// future ccm. These formats are ours, so we can afford to be strict.
const SchemaVersion = 1

// Profile is one parked account.
type Profile struct {
	Name  string `json:"name"`
	Label string `json:"label"`

	AccountUUID      string `json:"accountUuid"`
	EmailAddress     string `json:"emailAddress"`
	OrganizationName string `json:"organizationName,omitempty"`

	// ClaudeAiOauth is the credentials object verbatim.
	ClaudeAiOauth json.RawMessage `json:"claudeAiOauth"`
	// OAuthAccount is the .claude.json account object verbatim.
	OAuthAccount json.RawMessage `json:"oauthAccount"`
	UserID       string          `json:"userID,omitempty"`

	CreatedAt  time.Time `json:"createdAt"`
	LastUsedAt time.Time `json:"lastUsedAt,omitempty"`
}

// ExpiresAt reports the access token expiry as recorded in the profile.
// Zero means the profile carries no expiry field.
func (p *Profile) ExpiresAt() time.Time {
	c, err := patch.ReadCredentials(wrapCreds(p.ClaudeAiOauth))
	if err != nil || c.ExpiresAt == 0 {
		return time.Time{}
	}
	return time.UnixMilli(c.ExpiresAt)
}

// SubscriptionType reports the plan recorded in the profile, if any.
func (p *Profile) SubscriptionType() string {
	c, err := patch.ReadCredentials(wrapCreds(p.ClaudeAiOauth))
	if err != nil {
		return ""
	}
	return c.SubscriptionType
}

func wrapCreds(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte(`{}`)
	}
	b, err := json.Marshal(map[string]json.RawMessage{patch.KeyClaudeAiOauth: raw})
	if err != nil {
		return []byte(`{}`)
	}
	return b
}

// document is the on-disk shape once unsealed.
type document struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Profiles      map[string]*Profile `json:"profiles"`
}

// envelope wraps the sealed bytes so a vault written under one protection
// scheme is not silently misread under another.
type envelope struct {
	Sealer string `json:"sealer"`
	Data   string `json:"data"`
}

// Vault is a set of profiles persisted at Path.
type Vault struct {
	Path   string
	sealer Sealer
	doc    *document
}

// ErrNotFound reports an unknown profile name.
var ErrNotFound = errors.New("profile not found")

// Open loads the vault at path, creating an empty one in memory when absent.
// An empty path selects the per-platform default location.
func Open(path string) (*Vault, error) {
	if path == "" {
		dir, err := config.DefaultVaultDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(dir, "vault.json")
	}
	sealer, err := NewSealer()
	if err != nil {
		return nil, err
	}
	v := &Vault{Path: path, sealer: sealer}

	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		v.doc = &document{SchemaVersion: SchemaVersion, Profiles: map[string]*Profile{}}
		return v, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read vault %s: %w", path, err)
	}

	var env envelope
	if err := json.Unmarshal(b, &env); err != nil {
		return nil, fmt.Errorf("parse vault envelope %s: %w", path, err)
	}
	// Reached either by copying a vault between machines, or, now that the
	// sealer is selectable, by changing CCM_VAULT_BACKEND on one machine. The
	// second case is recoverable and by far the more likely mistake, so the
	// message names it rather than only reporting non-portability.
	if env.Sealer != v.sealer.Name() {
		return nil, fmt.Errorf("vault %s was sealed with %q but this ccm is using %q. "+
			"If you changed %s, set it back to read this vault; otherwise the vault was "+
			"written on a different machine or platform and cannot be moved",
			path, env.Sealer, v.sealer.Name(), config.EnvVaultBackend)
	}
	sealed, err := base64.StdEncoding.DecodeString(env.Data)
	if err != nil {
		return nil, fmt.Errorf("decode vault payload: %w", err)
	}
	plain, err := v.sealer.Unseal(sealed)
	if err != nil {
		return nil, fmt.Errorf("unseal vault %s: %w", path, err)
	}

	var doc document
	if err := json.Unmarshal(plain, &doc); err != nil {
		return nil, fmt.Errorf("parse vault contents: %w", err)
	}
	if doc.SchemaVersion > SchemaVersion {
		return nil, fmt.Errorf("vault %s has schema version %d but this ccm understands %d; upgrade ccm",
			path, doc.SchemaVersion, SchemaVersion)
	}
	if doc.Profiles == nil {
		doc.Profiles = map[string]*Profile{}
	}
	v.doc = &doc
	return v, nil
}

// Save seals and writes the vault atomically.
func (v *Vault) Save() error {
	v.doc.SchemaVersion = SchemaVersion
	plain, err := json.MarshalIndent(v.doc, "", "  ")
	if err != nil {
		return err
	}
	sealed, err := v.sealer.Seal(plain)
	if err != nil {
		return fmt.Errorf("seal vault: %w", err)
	}
	env := envelope{Sealer: v.sealer.Name(), Data: base64.StdEncoding.EncodeToString(sealed)}
	b, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return config.WriteFileAtomic(v.Path, b, 0o600)
}

// SealerName reports how this vault protects its contents, for doctor output.
func (v *Vault) SealerName() string { return v.sealer.Describe() }

// List returns profiles sorted by name.
func (v *Vault) List() []*Profile {
	out := make([]*Profile, 0, len(v.doc.Profiles))
	for _, p := range v.doc.Profiles {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get returns a profile by name.
func (v *Vault) Get(name string) (*Profile, error) {
	p, ok := v.doc.Profiles[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, name)
	}
	return p, nil
}

// FindByAccountUUID locates the profile holding a given account, which is how
// a live login is matched back to its parked slot during capture.
//
// Iteration is over sorted names rather than the map directly. Go randomizes
// map order, so if a vault ever does contain two profiles for one account, a
// map-order match would write the refreshed tokens into a different profile on
// each run and let the other silently rot. Capture prevents such duplicates
// from being created, but a deterministic lookup means a vault that already has
// them behaves predictably instead of losing a token at random.
func (v *Vault) FindByAccountUUID(uuid string) (*Profile, bool) {
	matches := v.FindAllByAccountUUID(uuid)
	if len(matches) == 0 {
		return nil, false
	}
	return matches[0], true
}

// FindAllByAccountUUID returns every profile holding an account, sorted by
// name. More than one means the vault has duplicates, which `ccm doctor`
// reports so the user can remove the extras.
func (v *Vault) FindAllByAccountUUID(uuid string) []*Profile {
	if uuid == "" {
		return nil
	}
	var out []*Profile
	for _, p := range v.List() {
		if p.AccountUUID == uuid {
			out = append(out, p)
		}
	}
	return out
}

// DuplicateAccounts returns the account UUIDs stored under more than one
// profile name, mapped to those names.
func (v *Vault) DuplicateAccounts() map[string][]string {
	byUUID := map[string][]string{}
	for _, p := range v.List() {
		if p.AccountUUID != "" {
			byUUID[p.AccountUUID] = append(byUUID[p.AccountUUID], p.Name)
		}
	}
	for uuid, names := range byUUID {
		if len(names) < 2 {
			delete(byUUID, uuid)
		}
	}
	return byUUID
}

// Put inserts or replaces a profile.
func (v *Vault) Put(p *Profile) {
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
	}
	v.doc.Profiles[p.Name] = p
}

// Delete removes a profile.
func (v *Vault) Delete(name string) error {
	if _, ok := v.doc.Profiles[name]; !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, name)
	}
	delete(v.doc.Profiles, name)
	return nil
}

// ErrBadName reports a profile name that cannot be used.
var ErrBadName = errors.New("invalid profile name")

// ValidateName rejects names that would be confusing or unusable.
//
// A name is typed as a bare CLI argument, so anything starting with a dash
// would be parsed as a flag, and whitespace makes it awkward to pass at all.
func ValidateName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: must not be empty", ErrBadName)
	}
	if name != strings.TrimSpace(name) {
		return fmt.Errorf("%w: %q has leading or trailing whitespace", ErrBadName, name)
	}
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("%w: %q starts with a dash, which the CLI reads as a flag", ErrBadName, name)
	}
	if strings.ContainsAny(name, " \t\n\r") {
		return fmt.Errorf("%w: %q contains whitespace", ErrBadName, name)
	}
	return nil
}

// Rename moves a profile to a new name, keeping its stored credentials.
//
// This exists so renaming never has to go through delete-then-recapture.
// Recapturing only works for the account currently signed into Claude Code, so
// for a parked profile that sequence would destroy the only copy of its refresh
// token and leave a browser sign-in as the only way back.
func (v *Vault) Rename(oldName, newName string) error {
	if err := ValidateName(newName); err != nil {
		return err
	}
	p, ok := v.doc.Profiles[oldName]
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, oldName)
	}
	if oldName == newName {
		return nil
	}
	if _, taken := v.doc.Profiles[newName]; taken {
		return fmt.Errorf("profile %q already exists; pick another name or remove it first", newName)
	}

	p.Name = newName
	if p.Label == "" {
		p.Label = p.EmailAddress
	}
	delete(v.doc.Profiles, oldName)
	v.doc.Profiles[newName] = p
	return nil
}

// UniqueName returns base, or base-2, base-3... when already taken. Used when
// capturing an account ccm has not seen before, so an unknown login is parked
// rather than discarded.
func (v *Vault) UniqueName(base string) string {
	if base == "" {
		base = "captured"
	}
	if _, taken := v.doc.Profiles[base]; !taken {
		return base
	}
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s-%d", base, i)
		if _, taken := v.doc.Profiles[cand]; !taken {
			return cand
		}
	}
}
