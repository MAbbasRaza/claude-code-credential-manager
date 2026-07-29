// Package manager implements the switch algorithm.
//
// The ordering in Switch is the whole point of the tool and is not incidental:
// the outgoing account is re-captured from live state *before* the incoming one
// is installed. Claude Code refreshes its OAuth token in the background and
// refresh tokens rotate, so a profile written once at add time holds a
// superseded token within hours. Capturing on the way out is what keeps parked
// accounts usable, and is the difference between a switcher that works
// indefinitely and one that works once.
package manager

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MAbbasRaza/claude-code-multi-account-manager/internal/config"
	"github.com/MAbbasRaza/claude-code-multi-account-manager/internal/lock"
	"github.com/MAbbasRaza/claude-code-multi-account-manager/internal/patch"
	"github.com/MAbbasRaza/claude-code-multi-account-manager/internal/proc"
	"github.com/MAbbasRaza/claude-code-multi-account-manager/internal/store"
	"github.com/MAbbasRaza/claude-code-multi-account-manager/internal/vault"
)

// Manager owns one resolved Claude Code installation and its vault.
type Manager struct {
	Paths    config.Paths
	Settings *config.Settings
	Store    store.Store
	Vault    *vault.Vault
}

// Open resolves paths, opens the credentials store and the vault.
func Open(flagConfigDir string) (*Manager, error) {
	s, err := config.LoadSettings()
	if err != nil {
		return nil, err
	}
	r := config.NewResolver()
	paths, err := r.Resolve(flagConfigDir, s.ClaudeConfigDir, s.CredentialsBackend)
	if err != nil {
		return nil, err
	}
	st, err := store.New(paths)
	if err != nil {
		return nil, err
	}
	v, err := vault.Open(s.VaultPath)
	if err != nil {
		return nil, err
	}
	return &Manager{Paths: paths, Settings: s, Store: st, Vault: v}, nil
}

// Status describes the currently active account.
type Status struct {
	ConfigDir       string `json:"configDir"`
	ConfigDirSource string `json:"configDirSource"`
	Backend         string `json:"backend"`
	CredentialsPath string `json:"credentialsPath,omitempty"`
	ConfigJSONPath  string `json:"configJsonPath"`

	// ActiveProfile is the vault profile matching the live account, empty
	// when the live account has never been captured.
	ActiveProfile string `json:"activeProfile,omitempty"`
	EmailAddress  string `json:"emailAddress,omitempty"`
	AccountUUID   string `json:"accountUuid,omitempty"`
	Organization  string `json:"organization,omitempty"`
	Subscription  string `json:"subscription,omitempty"`
	ExpiresAt     string `json:"expiresAt,omitempty"`

	LoggedIn bool `json:"loggedIn"`
}

// Status reads live state without modifying anything.
func (m *Manager) Status() (Status, error) {
	st := Status{
		ConfigDir:       m.Paths.Dir,
		ConfigDirSource: string(m.Paths.Source),
		Backend:         string(m.Paths.Backend),
		CredentialsPath: m.Paths.CredentialsPath,
		ConfigJSONPath:  m.Paths.ConfigJSONPath,
	}

	blob, err := m.Store.LoadBlob()
	if err != nil {
		return st, err
	}
	creds, credErr := patch.ReadCredentials(blob)
	if credErr == nil {
		st.LoggedIn = true
		st.Subscription = creds.SubscriptionType
		if creds.ExpiresAt > 0 {
			st.ExpiresAt = time.UnixMilli(creds.ExpiresAt).UTC().Format(time.RFC3339)
		}
	} else if !errors.Is(credErr, patch.ErrNoCredentials) {
		return st, credErr
	}

	cfg, err := m.readConfigJSON()
	if err != nil {
		return st, err
	}
	id, idErr := patch.ReadIdentity(cfg)
	if idErr == nil {
		st.EmailAddress = id.EmailAddress
		st.AccountUUID = id.AccountUUID
		st.Organization = id.OrganizationName
		if p, ok := m.Vault.FindByAccountUUID(id.AccountUUID); ok {
			st.ActiveProfile = p.Name
		}
	} else if !errors.Is(idErr, patch.ErrNoAccount) {
		return st, idErr
	}
	return st, nil
}

// ProfileView is a stored profile enriched with live state.
//
// It exists because the vault's copy of a profile is a snapshot taken at
// capture time, and Claude Code refreshes the access token continuously
// afterwards. For the account currently signed in, the live credentials are
// authoritative and sitting right there, so reporting the snapshot's expiry
// says "expired" about a token that is working fine.
type ProfileView struct {
	*vault.Profile

	// Active reports whether this profile is the account Claude Code is
	// currently using.
	Active bool

	// ExpiresAt is the live expiry for the active profile and the stored
	// snapshot's expiry for the rest. Zero when unknown.
	ExpiresAt time.Time

	// ExpiryIsLive distinguishes the two, so callers can avoid presenting a
	// snapshot as though it were current truth.
	ExpiryIsLive bool
}

// Expired reports whether the access token this view describes has lapsed.
//
// For a parked profile this is expected rather than a problem: Claude Code
// exchanges the refresh token for a new access token on its next request. Only
// the refresh token lapsing requires a new sign-in, and nothing here can
// determine that without attempting it.
func (v ProfileView) Expired() bool {
	return !v.ExpiresAt.IsZero() && time.Now().After(v.ExpiresAt)
}

// Profiles returns every stored profile with live state applied, alongside the
// status it was computed from. Every surface renders from this so the CLI, the
// tray, the desktop app and the extension cannot disagree.
func (m *Manager) Profiles() ([]ProfileView, Status, error) {
	st, err := m.Status()
	if err != nil {
		return nil, st, err
	}

	var live time.Time
	if st.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, st.ExpiresAt); err == nil {
			live = t
		}
	}

	stored := m.Vault.List()
	out := make([]ProfileView, 0, len(stored))
	for _, p := range stored {
		v := ProfileView{
			Profile: p,
			Active:  p.AccountUUID != "" && p.AccountUUID == st.AccountUUID,
		}
		if v.Active && !live.IsZero() {
			v.ExpiresAt = live
			v.ExpiryIsLive = true
		} else {
			v.ExpiresAt = p.ExpiresAt()
		}
		out = append(out, v)
	}
	return out, st, nil
}

func (m *Manager) readConfigJSON() ([]byte, error) {
	b, err := os.ReadFile(m.Paths.ConfigJSONPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", m.Paths.ConfigJSONPath, err)
	}
	return b, nil
}

// readLive returns the account-scoped slice of both documents.
func (m *Manager) readLive() (patch.Credentials, patch.Identity, error) {
	blob, err := m.Store.LoadBlob()
	if err != nil {
		return patch.Credentials{}, patch.Identity{}, err
	}
	creds, err := patch.ReadCredentials(blob)
	if err != nil {
		return patch.Credentials{}, patch.Identity{}, err
	}
	cfg, err := m.readConfigJSON()
	if err != nil {
		return patch.Credentials{}, patch.Identity{}, err
	}
	id, err := patch.ReadIdentity(cfg)
	if err != nil {
		return creds, patch.Identity{}, err
	}
	return creds, id, nil
}

// lockPath keeps the lock beside the vault so all surfaces contend on one file.
func (m *Manager) lockPath() string {
	return filepath.Join(filepath.Dir(m.Vault.Path), "ccm.lock")
}

// withVaultLock serializes a vault mutation across every ccm surface.
//
// Two things happen here, and the second is easy to miss. The lock stops the
// CLI, the tray and the VS Code extension from mutating at once. The re-read
// then discards the copy loaded during Open, which was read before this lock
// existed: without it, a switch begun at T0 would save a vault snapshot that
// silently reverts an `ccm add` completed at T1. Every mutating entry point
// goes through here so the single-writer guarantee in SECURITY.md is real
// rather than aspirational.
func (m *Manager) withVaultLock(operation string, fn func() error) error {
	lk, err := lock.Acquire(m.lockPath(), operation)
	if err != nil {
		return err
	}
	defer lk.Release()

	fresh, err := vault.Open(m.Settings.VaultPath)
	if err != nil {
		return fmt.Errorf("re-read vault under lock: %w", err)
	}
	m.Vault = fresh

	return fn()
}

// EnsureClosed refuses to proceed while Claude Code is running.
//
// A detection failure is treated as unsafe rather than safe: if ccm cannot
// tell whether Claude Code is running, it declines instead of risking a
// clobbered switch.
// The two refusals are returned as distinct types so callers can classify with
// errors.As rather than by inspecting the message. See errors.go for why that
// distinction is load-bearing.
func (m *Manager) EnsureClosed(force bool) error {
	if force || !m.Settings.ShouldRequireClosedSessions() {
		return nil
	}
	procs, err := proc.FindClaude()
	if err != nil {
		return &ErrDetectionFailed{Err: err}
	}
	if len(procs) == 0 {
		return nil
	}
	return &ErrClaudeRunning{Procs: procs}
}

// Capture stores the live account into the vault under name.
//
// When name is empty the profile is named after the account's email local part,
// which is how an unrecognised login is parked rather than discarded.
func (m *Manager) Capture(name string) (*vault.Profile, error) {
	var out *vault.Profile
	err := m.withVaultLock("capture "+name, func() error {
		p, err := m.captureLocked(name)
		out = p
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Remove deletes a profile under the vault lock.
func (m *Manager) Remove(name string) error {
	return m.withVaultLock("remove "+name, func() error {
		if err := m.Vault.Delete(name); err != nil {
			return err
		}
		return m.Vault.Save()
	})
}

// Rename changes a profile's name, keeping its credentials.
//
// Renaming needs a first-class operation because the obvious alternative,
// removing and re-capturing, only works for the account currently signed into
// Claude Code. Applied to a parked profile it would delete the only copy of a
// refresh token that nothing can regenerate without a browser sign-in.
func (m *Manager) Rename(oldName, newName string) error {
	return m.withVaultLock("rename "+oldName, func() error {
		if err := m.Vault.Rename(oldName, newName); err != nil {
			return err
		}
		return m.Vault.Save()
	})
}

// captureLocked requires the vault lock to be held and the vault freshly read.
func (m *Manager) captureLocked(name string) (*vault.Profile, error) {
	creds, id, err := m.readLive()
	if err != nil {
		if errors.Is(err, patch.ErrNoCredentials) {
			return nil, errors.New("no active login found; run /login in Claude Code first")
		}
		if errors.Is(err, patch.ErrNoAccount) {
			return nil, fmt.Errorf("credentials found but %s has no oauthAccount; "+
				"start Claude Code once so it records the account, then retry", m.Paths.ConfigJSONPath)
		}
		return nil, err
	}
	if err := patch.ValidateCredentials(creds.ClaudeAiOauth); err != nil {
		return nil, err
	}

	// One account must map to exactly one profile. Storing the same account
	// twice looks harmless but breaks capture-on-switch: only one of the
	// duplicates receives the refreshed tokens, and the other silently decays
	// into a dead refresh token that forces a browser re-authorization.
	if existing, found := m.Vault.FindByAccountUUID(id.AccountUUID); found {
		if name != "" && name != existing.Name {
			return nil, fmt.Errorf(
				"account %s is already saved as profile %q.\n"+
					"Storing it twice would break switching: only one copy would receive refreshed\n"+
					"tokens and the other would go stale.\n\n"+
					"  ccm rename %s %s   rename it, keeping the stored credentials\n"+
					"  ccm add %s          refresh the existing profile in place",
				orUnknownEmail(id.EmailAddress), existing.Name, existing.Name, name, existing.Name)
		}
		// Same profile, or no name given: refresh it in place.
		existing.ClaudeAiOauth = creds.ClaudeAiOauth
		existing.OAuthAccount = id.OAuthAccount
		existing.UserID = id.UserID
		existing.EmailAddress = id.EmailAddress
		existing.OrganizationName = id.OrganizationName
		if existing.Label == "" {
			existing.Label = id.EmailAddress
		}
		m.Vault.Put(existing)
		return existing, m.Vault.Save()
	}

	if name == "" {
		name = m.Vault.UniqueName(defaultProfileName(id.EmailAddress))
	} else if clash, err := m.Vault.Get(name); err == nil && clash.AccountUUID != id.AccountUUID {
		// The name is taken by a different account. Overwriting would discard
		// that account's only stored refresh token.
		return nil, fmt.Errorf(
			"profile %q already holds a different account (%s).\n"+
				"Pick another name, or run `ccm rm %s` first if you no longer need it.",
			name, orUnknownEmail(clash.EmailAddress), name)
	}

	p := &vault.Profile{
		Name:             name,
		Label:            id.EmailAddress,
		AccountUUID:      id.AccountUUID,
		EmailAddress:     id.EmailAddress,
		OrganizationName: id.OrganizationName,
		ClaudeAiOauth:    creds.ClaudeAiOauth,
		OAuthAccount:     id.OAuthAccount,
		UserID:           id.UserID,
		CreatedAt:        time.Now().UTC(),
	}
	m.Vault.Put(p)
	return p, m.Vault.Save()
}

func orUnknownEmail(s string) string {
	if s == "" {
		return "unknown account"
	}
	return s
}

func defaultProfileName(email string) string {
	if email == "" {
		return "captured"
	}
	local := email
	if i := strings.IndexByte(email, '@'); i > 0 {
		local = email[:i]
	}
	var b strings.Builder
	for _, r := range strings.ToLower(local) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.' || r == '-' || r == '_' || r == '+':
			b.WriteByte('-')
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "captured"
	}
	return s
}

// Result reports what a switch did.
type Result struct {
	Switched       bool   `json:"switched"`
	From           string `json:"from,omitempty"`
	FromEmail      string `json:"fromEmail,omitempty"`
	To             string `json:"to"`
	ToEmail        string `json:"toEmail,omitempty"`
	CapturedAs     string `json:"capturedAs,omitempty"`
	CapturedNew    bool   `json:"capturedNew"`
	BackupDir      string `json:"backupDir,omitempty"`
	RestartWarning string `json:"restartWarning,omitempty"`
}

// Switch installs the named profile, capturing the outgoing account first.
func (m *Manager) Switch(target string, force bool) (Result, error) {
	var res Result
	res.To = target

	// Checked before taking the lock: it is a read, and refusing early avoids
	// holding the lock while the user reads a multi-line explanation.
	if err := m.EnsureClosed(force); err != nil {
		return res, err
	}

	err := m.withVaultLock("switch to "+target, func() error {
		return m.switchLocked(target, &res)
	})
	return res, err
}

// switchLocked requires the vault lock to be held and the vault freshly read.
func (m *Manager) switchLocked(target string, res *Result) error {
	tp, err := m.Vault.Get(target)
	if err != nil {
		return err
	}
	if err := patch.ValidateCredentials(tp.ClaudeAiOauth); err != nil {
		return fmt.Errorf("profile %q is unusable: %w", target, err)
	}

	blob, err := m.Store.LoadBlob()
	if err != nil {
		return err
	}
	cfg, err := m.readConfigJSON()
	if err != nil {
		return err
	}

	backupDir, err := m.backup(blob, cfg)
	if err != nil {
		return fmt.Errorf("backup before switch: %w", err)
	}
	res.BackupDir = backupDir

	// Capture the outgoing account. Failures here are fatal by design: losing
	// the live refresh token is exactly the problem this tool exists to solve,
	// so ccm will not trade it away to complete a switch.
	capturedName, isNew, email, err := m.captureOutgoing(blob, cfg)
	if err != nil {
		return err
	}
	res.CapturedAs, res.CapturedNew, res.FromEmail = capturedName, isNew, email
	res.From = capturedName

	// Persist the captured outgoing tokens BEFORE overwriting the live
	// documents that hold them. If the process dies between these steps, the
	// worst case is a vault that is merely ahead of the files on disk; the
	// reverse ordering would destroy a rotated refresh token outright, which
	// is unrecoverable without a browser sign-in.
	if capturedName != "" {
		if err := m.Vault.Save(); err != nil {
			return fmt.Errorf("captured the outgoing account but could not save the vault (%w); "+
				"nothing was changed", err)
		}
	}

	newBlob, err := patch.SetCredentials(blob, tp.ClaudeAiOauth)
	if err != nil {
		return err
	}
	newCfg, err := patch.SetIdentity(cfg, patch.Identity{
		OAuthAccount: tp.OAuthAccount,
		UserID:       tp.UserID,
	})
	if err != nil {
		return err
	}

	if err := m.Store.SaveBlob(newBlob); err != nil {
		return err
	}
	if err := config.WriteFileAtomic(m.Paths.ConfigJSONPath, newCfg, 0o600); err != nil {
		// The credentials document is already updated. Say so plainly rather
		// than leaving the user to discover a half-applied switch.
		return fmt.Errorf("credentials were updated but %s could not be written (%w); "+
			"state is inconsistent, restore from %s", m.Paths.ConfigJSONPath, err, backupDir)
	}

	tp.LastUsedAt = time.Now().UTC()
	m.Vault.Put(tp)
	if err := m.Vault.Save(); err != nil {
		return fmt.Errorf("switch applied but vault could not be saved: %w", err)
	}

	res.Switched = true
	res.ToEmail = tp.EmailAddress
	res.RestartWarning = "Restart Claude Code for the new account to take effect."
	return nil
}

// captureOutgoing writes the live account back to its profile in memory. The
// caller persists the vault before touching the live documents.
func (m *Manager) captureOutgoing(blob, cfg []byte) (name string, isNew bool, email string, err error) {
	creds, credErr := patch.ReadCredentials(blob)
	if errors.Is(credErr, patch.ErrNoCredentials) {
		return "", false, "", nil // nothing signed in, nothing to preserve
	}
	if credErr != nil {
		return "", false, "", credErr
	}
	id, idErr := patch.ReadIdentity(cfg)
	if errors.Is(idErr, patch.ErrNoAccount) {
		return "", false, "", nil
	}
	if idErr != nil {
		return "", false, "", idErr
	}
	if err := patch.ValidateCredentials(creds.ClaudeAiOauth); err != nil {
		// A malformed live login is not worth preserving and must not block
		// the switch the user asked for.
		return "", false, id.EmailAddress, nil
	}

	existing, found := m.Vault.FindByAccountUUID(id.AccountUUID)
	if found {
		existing.ClaudeAiOauth = creds.ClaudeAiOauth
		existing.OAuthAccount = id.OAuthAccount
		existing.UserID = id.UserID
		existing.EmailAddress = id.EmailAddress
		existing.OrganizationName = id.OrganizationName
		if existing.Label == "" {
			existing.Label = id.EmailAddress
		}
		m.Vault.Put(existing)
		return existing.Name, false, id.EmailAddress, nil
	}

	newName := m.Vault.UniqueName(defaultProfileName(id.EmailAddress))
	m.Vault.Put(&vault.Profile{
		Name:             newName,
		Label:            id.EmailAddress,
		AccountUUID:      id.AccountUUID,
		EmailAddress:     id.EmailAddress,
		OrganizationName: id.OrganizationName,
		ClaudeAiOauth:    creds.ClaudeAiOauth,
		OAuthAccount:     id.OAuthAccount,
		UserID:           id.UserID,
		CreatedAt:        time.Now().UTC(),
	})
	return newName, true, id.EmailAddress, nil
}

// backup copies both documents before any write.
//
// The timestamp has one-second resolution, so two switches within the same
// second would collide. Rather than overwrite what may be the only copy of a
// working credential set, a colliding name gets a suffix.
func (m *Manager) backup(blob, cfg []byte) (string, error) {
	stamp := time.Now().UTC().Format("20060102T150405Z")
	base := filepath.Join(filepath.Dir(m.Vault.Path), "backups", stamp)

	dir := base
	for i := 2; ; i++ {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			break
		}
		dir = fmt.Sprintf("%s-%d", base, i)
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if len(blob) > 0 {
		if err := config.WriteFileAtomic(filepath.Join(dir, "credentials.json"), blob, 0o600); err != nil {
			return "", err
		}
	}
	if len(cfg) > 0 {
		if err := config.WriteFileAtomic(filepath.Join(dir, "claude.json"), cfg, 0o600); err != nil {
			return "", err
		}
	}
	meta, _ := json.MarshalIndent(map[string]string{
		"configDir":       m.Paths.Dir,
		"credentialsFrom": m.Store.Describe(),
		"configJsonFrom":  m.Paths.ConfigJSONPath,
		"takenAt":         time.Now().UTC().Format(time.RFC3339),
	}, "", "  ")
	if err := config.WriteFileAtomic(filepath.Join(dir, "meta.json"), append(meta, '\n'), 0o600); err != nil {
		return "", err
	}
	return dir, nil
}
