package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/webview/webview_go"

	"github.com/MAbbasRaza/claude-code-credential-manager/internal/config"
	"github.com/MAbbasRaza/claude-code-credential-manager/internal/manager"
	"github.com/MAbbasRaza/claude-code-credential-manager/internal/proc"
)

// api is everything the page can call.
//
// A manager is opened per call rather than held. The CLI, the tray and the
// VS Code extension may all have changed the vault since the last look, and a
// cached handle would render state that is no longer true.
type api struct {
	win webview.WebView
}

// Profile is one saved account as the page renders it.
type Profile struct {
	Name         string `json:"name"`
	Email        string `json:"email"`
	Organization string `json:"organization"`
	Plan         string `json:"plan"`
	Active       bool   `json:"active"`
	Expiry       string `json:"expiry"`
	// Expired describes the access token only. For a parked profile this is
	// the normal resting state rather than a problem.
	Expired bool `json:"expired"`
	// ExpiryIsLive is true when Expiry came from the credentials Claude Code is
	// using right now, rather than the snapshot taken at capture time.
	ExpiryIsLive bool   `json:"expiryIsLive"`
	LastUsed     string `json:"lastUsed"`
}

// Overview is the whole window state in one round trip, so the page never
// renders a half-updated view assembled from several calls.
type Overview struct {
	Profiles []Profile `json:"profiles"`
	Version  string    `json:"version"`

	LoggedIn     bool   `json:"loggedIn"`
	Email        string `json:"email"`
	Organization string `json:"organization"`
	Plan         string `json:"plan"`
	ActiveName   string `json:"activeName"`

	ConfigDir string `json:"configDir"`
	VaultPath string `json:"vaultPath"`

	// RunningCode is meaningful only when RunningUnknown is false.
	RunningCode    int    `json:"runningCode"`
	RunningUnknown bool   `json:"runningUnknown"`
	RunningError   string `json:"runningError"`
}

func (a *api) List() (Overview, error) {
	m, err := manager.Open("")
	if err != nil {
		return Overview{}, err
	}
	views, st, err := m.Profiles()
	if err != nil {
		return Overview{}, err
	}

	out := Overview{
		Version:      version,
		LoggedIn:     st.LoggedIn,
		Email:        st.EmailAddress,
		Organization: st.Organization,
		Plan:         st.Subscription,
		ActiveName:   st.ActiveProfile,
		ConfigDir:    st.ConfigDir,
		VaultPath:    m.Vault.Path,
		Profiles:     []Profile{},
	}
	// Three states, not two. "Could not tell" must not render as "nothing is
	// running", because that is the state in which the running-session guard is
	// inoperative and the user most needs to know.
	if procs, err := proc.FindClaude(); err == nil {
		out.RunningCode = len(procs)
	} else {
		out.RunningUnknown = true
		out.RunningError = err.Error()
	}

	for _, v := range views {
		e := Profile{
			Name:         v.Name,
			Email:        v.EmailAddress,
			Organization: v.OrganizationName,
			Plan:         v.SubscriptionType(),
			Active:       v.Active,
			ExpiryIsLive: v.ExpiryIsLive,
		}
		if !v.ExpiresAt.IsZero() {
			e.Expiry = v.ExpiresAt.Local().Format("2 Jan 2006, 15:04")
			e.Expired = v.Expired()
		}
		if !v.LastUsedAt.IsZero() {
			e.LastUsed = v.LastUsedAt.Local().Format("2 Jan 2006, 15:04")
		}
		out.Profiles = append(out.Profiles, e)
	}
	return out, nil
}

// SwitchResult tells the page what happened, including the guard case so it can
// offer to override rather than just reporting a failure.
type SwitchResult struct {
	Switched   bool   `json:"switched"`
	To         string `json:"to"`
	ToEmail    string `json:"toEmail"`
	CapturedAs string `json:"capturedAs"`
	NewProfile bool   `json:"newProfile"`

	// Blocked means a live session was found and the user may override.
	Blocked      bool  `json:"blocked"`
	BlockedCount int   `json:"blockedCount"`
	BlockedPIDs  []int `json:"blockedPids"`
	// Undetermined means ccm could not tell what is running. The guard is
	// inoperative rather than triggered, which is a different thing to say.
	Undetermined bool   `json:"undetermined"`
	Message      string `json:"message"`
}

func (a *api) Switch(name string, force bool) (SwitchResult, error) {
	m, err := manager.Open("")
	if err != nil {
		return SwitchResult{}, err
	}

	res, err := m.Switch(name, force)
	if err != nil {
		// A refusal is not a failure to report and forget; it is a decision to
		// put back to the user, so it comes back as data rather than an error.
		//
		// Classified by type, never by message text. The detection-failure
		// message contains the words "Claude Code is running", so a substring
		// test reports a broken guard as a live session and invites the user to
		// override it with a process count of zero.
		var running *manager.ErrClaudeRunning
		if errors.As(err, &running) {
			return SwitchResult{
				Blocked:      true,
				BlockedCount: len(running.Procs),
				BlockedPIDs:  running.PIDs(),
				To:           name,
				Message:      err.Error(),
			}, nil
		}

		var undetermined *manager.ErrDetectionFailed
		if errors.As(err, &undetermined) {
			return SwitchResult{
				Undetermined: true,
				To:           name,
				Message:      err.Error(),
			}, nil
		}

		return SwitchResult{}, err
	}

	return SwitchResult{
		Switched:   true,
		To:         res.To,
		ToEmail:    res.ToEmail,
		CapturedAs: res.CapturedAs,
		NewProfile: res.CapturedNew,
	}, nil
}

func (a *api) Capture(name string) (Profile, error) {
	m, err := manager.Open("")
	if err != nil {
		return Profile{}, err
	}
	p, err := m.Capture(strings.TrimSpace(name))
	if err != nil {
		return Profile{}, err
	}
	return Profile{Name: p.Name, Email: p.EmailAddress, Organization: p.OrganizationName}, nil
}

func (a *api) Rename(oldName, newName string) error {
	m, err := manager.Open("")
	if err != nil {
		return err
	}
	return m.Rename(oldName, strings.TrimSpace(newName))
}

func (a *api) Remove(name string) error {
	m, err := manager.Open("")
	if err != nil {
		return err
	}
	return m.Remove(name)
}

// Doctor returns a plain-text report. It is deliberately free of token
// material so it can be pasted into a bug report unedited.
func (a *api) Doctor() (string, error) {
	m, err := manager.Open("")
	if err != nil {
		return "", err
	}
	st, err := m.Status()
	if err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "ccm %s\n\n", version)
	fmt.Fprintf(&b, "Config directory   %s\n", st.ConfigDir)
	fmt.Fprintf(&b, "  resolved from    %s\n", st.ConfigDirSource)
	fmt.Fprintf(&b, "Credentials        %s\n", m.Store.Describe())
	fmt.Fprintf(&b, "Account map        %s\n", st.ConfigJSONPath)
	fmt.Fprintf(&b, "Vault              %s\n", m.Vault.Path)
	fmt.Fprintf(&b, "  protection       %s\n", m.Vault.SealerName())
	fmt.Fprintf(&b, "  profiles         %d\n\n", len(m.Vault.List()))

	if st.LoggedIn {
		fmt.Fprintf(&b, "Active account     %s\n", orText(st.EmailAddress, "unknown"))
		if st.Organization != "" {
			fmt.Fprintf(&b, "  organization     %s\n", st.Organization)
		}
		if st.Subscription != "" {
			fmt.Fprintf(&b, "  plan             %s\n", st.Subscription)
		}
		if st.ExpiresAt != "" {
			fmt.Fprintf(&b, "  token expiry     %s\n", st.ExpiresAt)
		}
	} else {
		b.WriteString("Active account     not signed in\n")
	}

	var warnings []string
	for _, names := range m.Vault.DuplicateAccounts() {
		warnings = append(warnings, "Duplicate profiles for one account: "+strings.Join(names, ", "))
	}
	// Matches what the CLI's doctor reports. A swallowed enumeration error made
	// the pasteable report claim "No warnings" on precisely the machines where
	// the running-session guard does not work.
	if procs, err := proc.FindClaude(); err != nil {
		warnings = append(warnings, "Could not enumerate processes: "+err.Error()+
			"\n    The running-session guard cannot work on this machine; switches will be refused.")
	} else if len(procs) > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"Claude Code is running (%d process(es)); switching now would be undone when it exits.", len(procs)))
	}
	if st.LoggedIn && st.ActiveProfile == "" {
		warnings = append(warnings, "The active account is not saved yet. Use Capture current login.")
	}

	if len(warnings) > 0 {
		b.WriteString("\nWarnings\n")
		for _, w := range warnings {
			fmt.Fprintf(&b, "  - %s\n", w)
		}
	} else {
		b.WriteString("\nNo warnings.\n")
	}
	return b.String(), nil
}

// Settings mirrors the settings file for the page.
type Settings struct {
	ClaudeConfigDir       string `json:"claudeConfigDir"`
	CredentialsBackend    string `json:"credentialsBackend"`
	RequireClosedSessions bool   `json:"requireClosedSessions"`
	SettingsPath          string `json:"settingsPath"`
}

func (a *api) SettingsGet() (Settings, error) {
	s, err := config.LoadSettings()
	if err != nil {
		return Settings{}, err
	}
	backend := s.CredentialsBackend
	if backend == "" {
		backend = "auto"
	}
	return Settings{
		ClaudeConfigDir:       s.ClaudeConfigDir,
		CredentialsBackend:    backend,
		RequireClosedSessions: s.ShouldRequireClosedSessions(),
		SettingsPath:          s.Path(),
	}, nil
}

func (a *api) SettingsSet(dir, backend string, requireClosed bool) error {
	s, err := config.LoadSettings()
	if err != nil {
		return err
	}

	dir = strings.TrimSpace(dir)
	if dir != "" {
		// Refuse a directory Claude Code does not use. Accepting one would
		// produce switches that appear to succeed and change nothing.
		if err := config.ValidateClaudeDir(dir); err != nil {
			return err
		}
	}
	s.ClaudeConfigDir = dir

	if _, err := config.ParseBackendPref(backend); err != nil {
		return err
	}
	if backend == "auto" {
		s.CredentialsBackend = ""
	} else {
		s.CredentialsBackend = backend
	}

	s.RequireClosedSessions = &requireClosed
	return s.Save()
}

func orText(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
