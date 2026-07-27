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

	"github.com/MAbbasRaza/claude-code-credential-manager/internal/config"
	"github.com/MAbbasRaza/claude-code-credential-manager/internal/lock"
	"github.com/MAbbasRaza/claude-code-credential-manager/internal/patch"
	"github.com/MAbbasRaza/claude-code-credential-manager/internal/proc"
	"github.com/MAbbasRaza/claude-code-credential-manager/internal/store"
	"github.com/MAbbasRaza/claude-code-credential-manager/internal/vault"
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
	paths, err := r.Resolve(flagConfigDir, s.ClaudeConfigDir)
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

// EnsureClosed refuses to proceed while Claude Code is running.
//
// A detection failure is treated as unsafe rather than safe: if ccm cannot
// tell whether Claude Code is running, it declines instead of risking a
// clobbered switch.
func (m *Manager) EnsureClosed(force bool) error {
	if force || !m.Settings.ShouldRequireClosedSessions() {
		return nil
	}
	procs, err := proc.FindClaude()
	if err != nil {
		return fmt.Errorf("could not determine whether Claude Code is running (%w); "+
			"close Claude Code and retry, or pass --force", err)
	}
	if len(procs) == 0 {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Claude Code is running (%d process", len(procs))
	if len(procs) != 1 {
		b.WriteString("es")
	}
	b.WriteString("): ")
	for i, p := range procs {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "pid %d", p.PID)
	}
	b.WriteString(".\nA running session keeps using the old account and rewrites .claude.json when it exits, " +
		"which would undo the switch. Close Claude Code and retry, or pass --force to override.")
	return errors.New(b.String())
}

// Capture stores the live account into the vault under name.
//
// When name is empty the profile is named after the account's email local part,
// which is how an unrecognised login is parked rather than discarded.
func (m *Manager) Capture(name string) (*vault.Profile, error) {
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

	if name == "" {
		name = m.Vault.UniqueName(defaultProfileName(id.EmailAddress))
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
	if existing, err := m.Vault.Get(name); err == nil {
		p.CreatedAt = existing.CreatedAt
	}
	m.Vault.Put(p)
	return p, m.Vault.Save()
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

	tp, err := m.Vault.Get(target)
	if err != nil {
		return res, err
	}
	if err := patch.ValidateCredentials(tp.ClaudeAiOauth); err != nil {
		return res, fmt.Errorf("profile %q is unusable: %w", target, err)
	}
	if err := m.EnsureClosed(force); err != nil {
		return res, err
	}

	lk, err := lock.Acquire(m.lockPath(), "switch to "+target)
	if err != nil {
		return res, err
	}
	defer lk.Release()

	blob, err := m.Store.LoadBlob()
	if err != nil {
		return res, err
	}
	cfg, err := m.readConfigJSON()
	if err != nil {
		return res, err
	}

	backupDir, err := m.backup(blob, cfg)
	if err != nil {
		return res, fmt.Errorf("backup before switch: %w", err)
	}
	res.BackupDir = backupDir

	// Capture the outgoing account. Failures here are fatal by design: losing
	// the live refresh token is exactly the problem this tool exists to solve,
	// so ccm will not trade it away to complete a switch.
	if capturedName, isNew, email, err := m.captureOutgoing(blob, cfg, target); err != nil {
		return res, err
	} else {
		res.CapturedAs, res.CapturedNew, res.FromEmail = capturedName, isNew, email
		res.From = capturedName
	}

	newBlob, err := patch.SetCredentials(blob, tp.ClaudeAiOauth)
	if err != nil {
		return res, err
	}
	newCfg, err := patch.SetIdentity(cfg, patch.Identity{
		OAuthAccount: tp.OAuthAccount,
		UserID:       tp.UserID,
	})
	if err != nil {
		return res, err
	}

	if err := m.Store.SaveBlob(newBlob); err != nil {
		return res, err
	}
	if err := config.WriteFileAtomic(m.Paths.ConfigJSONPath, newCfg, 0o600); err != nil {
		// The credentials document is already updated. Say so plainly rather
		// than leaving the user to discover a half-applied switch.
		return res, fmt.Errorf("credentials were updated but %s could not be written (%w); "+
			"state is inconsistent, restore from %s", m.Paths.ConfigJSONPath, err, backupDir)
	}

	tp.LastUsedAt = time.Now().UTC()
	m.Vault.Put(tp)
	if err := m.Vault.Save(); err != nil {
		return res, fmt.Errorf("switch applied but vault could not be saved: %w", err)
	}

	res.Switched = true
	res.ToEmail = tp.EmailAddress
	res.RestartWarning = "Restart Claude Code for the new account to take effect."
	return res, nil
}

// captureOutgoing writes the live account back to its profile.
func (m *Manager) captureOutgoing(blob, cfg []byte, target string) (name string, isNew bool, email string, err error) {
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
func (m *Manager) backup(blob, cfg []byte) (string, error) {
	stamp := time.Now().UTC().Format("20060102T150405Z")
	dir := filepath.Join(filepath.Dir(m.Vault.Path), "backups", stamp)
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
