package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// EnvCCMHome relocates ccm's own settings and default vault location.
const EnvCCMHome = "CCM_HOME"

// Settings is ccm's own persisted configuration. It exists so that the
// resolved Claude Code directory is pinned rather than re-derived from the
// environment by each surface: a CLI started from a shell and a tray app
// started from Explorer do not inherit the same variables.
type Settings struct {
	// ClaudeConfigDir pins the Claude Code installation to manage.
	// Empty means "fall through to CLAUDE_CONFIG_DIR, then the default".
	ClaudeConfigDir string `json:"claudeConfigDir"`

	// VaultPath overrides where profiles are stored.
	// Empty means "use the per-platform default".
	VaultPath string `json:"vaultPath"`

	// RequireClosedSessions refuses a switch while Claude Code is running.
	// A pointer so an absent key defaults to true rather than false.
	RequireClosedSessions *bool `json:"requireClosedSessions,omitempty"`

	// CredentialsBackend forces "file" or "keychain" instead of the platform
	// default. Mainly for macOS over SSH or in tmux, where the Keychain is
	// locked but Claude Code still reads ~/.claude/.credentials.json.
	// Empty or "auto" uses the platform default.
	CredentialsBackend string `json:"credentialsBackend,omitempty"`

	path string
}

// SettingsPath returns the location of ccm's settings file.
func SettingsPath() (string, error) {
	if h := os.Getenv(EnvCCMHome); h != "" {
		return filepath.Join(h, "config.json"), nil
	}
	dir, err := ccmConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func ccmConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "windows":
		if v := os.Getenv("APPDATA"); v != "" {
			return filepath.Join(v, "ccm"), nil
		}
		return filepath.Join(home, "AppData", "Roaming", "ccm"), nil
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "ccm"), nil
	default:
		if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
			return filepath.Join(v, "ccm"), nil
		}
		return filepath.Join(home, ".config", "ccm"), nil
	}
}

// DefaultVaultDir returns the per-platform vault directory used when
// Settings.VaultPath is empty.
func DefaultVaultDir() (string, error) {
	if h := os.Getenv(EnvCCMHome); h != "" {
		return filepath.Join(h, "vault"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "windows":
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			return filepath.Join(v, "ccm"), nil
		}
		return filepath.Join(home, "AppData", "Local", "ccm"), nil
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "ccm"), nil
	default:
		if v := os.Getenv("XDG_DATA_HOME"); v != "" {
			return filepath.Join(v, "ccm"), nil
		}
		return filepath.Join(home, ".local", "share", "ccm"), nil
	}
}

// LoadSettings reads the settings file. A missing file is not an error; it
// yields zero-valued settings so first run works with no setup.
func LoadSettings() (*Settings, error) {
	p, err := SettingsPath()
	if err != nil {
		return nil, err
	}
	return LoadSettingsFrom(p)
}

// LoadSettingsFrom reads settings from an explicit path.
func LoadSettingsFrom(p string) (*Settings, error) {
	s := &Settings{path: p}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read settings %s: %w", p, err)
	}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, fmt.Errorf("parse settings %s: %w", p, err)
	}
	s.path = p
	return s, nil
}

// Path reports where these settings were loaded from or will be saved to.
func (s *Settings) Path() string { return s.path }

// ShouldRequireClosedSessions defaults to true when the key is absent.
// Switching under a live Claude Code process is the documented way to corrupt
// a switch, so the safe value must be the default.
func (s *Settings) ShouldRequireClosedSessions() bool {
	if s.RequireClosedSessions == nil {
		return true
	}
	return *s.RequireClosedSessions
}

// Save writes the settings file atomically, creating parent directories.
func (s *Settings) Save() error {
	if s.path == "" {
		p, err := SettingsPath()
		if err != nil {
			return err
		}
		s.path = p
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create settings dir: %w", err)
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return WriteFileAtomic(s.path, b, 0o600)
}

// ErrNotClaudeDir reports a directory that does not look like a Claude Code
// installation.
var ErrNotClaudeDir = errors.New("not a Claude Code config directory")

// ValidateClaudeDir refuses a directory that holds neither of the two files
// Claude Code maintains. Accepting an arbitrary path would let ccm create an
// empty config that Claude Code never reads, which looks like a successful
// switch but silently does nothing.
func ValidateClaudeDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("%w: empty path", ErrNotClaudeDir)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrNotClaudeDir, dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %s is not a directory", ErrNotClaudeDir, dir)
	}
	for _, name := range []string{".claude.json", ".credentials.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return nil
		}
	}
	// The default layout keeps .claude.json beside the directory, not inside it.
	if home, err := os.UserHomeDir(); err == nil {
		if filepath.Clean(dir) == filepath.Join(home, ".claude") {
			if _, err := os.Stat(filepath.Join(home, ".claude.json")); err == nil {
				return nil
			}
		}
	}
	return fmt.Errorf("%w: %s contains neither .claude.json nor .credentials.json", ErrNotClaudeDir, dir)
}
