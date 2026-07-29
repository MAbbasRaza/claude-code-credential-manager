package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/MAbbasRaza/claude-code-multi-account-manager/internal/config"
)

func cmdInit(g globalOpts) error {
	s, err := config.LoadSettings()
	if err != nil {
		return err
	}
	r := config.NewResolver()

	// Deliberately ignore any already-pinned value so init re-detects and can
	// correct a directory that was pinned wrongly or has since moved.
	paths, err := r.Resolve(g.configDir, "", s.CredentialsBackend)
	if err != nil {
		return err
	}
	if err := config.ValidateClaudeDir(paths.Dir); err != nil {
		return fmt.Errorf("%w\n\nPass --config-dir <path> if Claude Code lives somewhere else", err)
	}

	s.ClaudeConfigDir = paths.Dir
	if err := s.Save(); err != nil {
		return err
	}

	if g.jsonOut {
		return emitJSON(map[string]any{
			"claudeConfigDir": paths.Dir,
			"detectedFrom":    string(paths.Source),
			"settingsPath":    s.Path(),
		})
	}
	fmt.Printf("Detected Claude Code config directory from %s:\n  %s\n\n", paths.Source, paths.Dir)
	fmt.Printf("Pinned it in %s\n", s.Path())
	fmt.Println("The CLI, tray app and VS Code extension will now all use this directory,")
	fmt.Println("even when they do not inherit CLAUDE_CONFIG_DIR from your shell.")
	return nil
}

func cmdConfig(g globalOpts, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: ccm config get|set|path [key] [value]")
	}
	s, err := config.LoadSettings()
	if err != nil {
		return err
	}

	switch args[0] {
	case "path":
		if g.jsonOut {
			return emitJSON(map[string]string{"settingsPath": s.Path()})
		}
		fmt.Println(s.Path())
		return nil

	case "get":
		if len(args) < 2 {
			if g.jsonOut {
				return emitJSON(s)
			}
			fmt.Printf("claudeConfigDir       = %s\n", orUnset(s.ClaudeConfigDir))
			fmt.Printf("vaultPath             = %s\n", orUnset(s.VaultPath))
			fmt.Printf("requireClosedSessions = %t\n", s.ShouldRequireClosedSessions())
			backend := s.CredentialsBackend
			if backend == "" {
				backend = "auto (platform default)"
			}
			fmt.Printf("credentialsBackend    = %s\n", backend)
			return nil
		}
		v, err := getKey(s, args[1])
		if err != nil {
			return err
		}
		if g.jsonOut {
			return emitJSON(map[string]string{args[1]: v})
		}
		fmt.Println(v)
		return nil

	case "set":
		if len(args) < 3 {
			return errors.New("usage: ccm config set <key> <value>")
		}
		if err := setKey(s, args[1], args[2]); err != nil {
			return err
		}
		if err := s.Save(); err != nil {
			return err
		}
		if g.jsonOut {
			return emitJSON(map[string]string{args[1]: args[2], "settingsPath": s.Path()})
		}
		fmt.Printf("Set %s = %s in %s\n", args[1], args[2], s.Path())
		return nil

	default:
		return fmt.Errorf("unknown config subcommand %q", args[0])
	}
}

func orUnset(v string) string {
	if v == "" {
		return "(unset - falls through to CLAUDE_CONFIG_DIR, then the platform default)"
	}
	return v
}

func getKey(s *config.Settings, key string) (string, error) {
	switch key {
	case "claudeConfigDir":
		return s.ClaudeConfigDir, nil
	case "vaultPath":
		return s.VaultPath, nil
	case "requireClosedSessions":
		return strconv.FormatBool(s.ShouldRequireClosedSessions()), nil
	case "credentialsBackend":
		if s.CredentialsBackend == "" {
			return "auto", nil
		}
		return s.CredentialsBackend, nil
	default:
		return "", fmt.Errorf("unknown key %q (valid: claudeConfigDir, vaultPath, requireClosedSessions, credentialsBackend)", key)
	}
}

func setKey(s *config.Settings, key, value string) error {
	switch key {
	case "claudeConfigDir":
		if value == "" {
			s.ClaudeConfigDir = ""
			return nil
		}
		abs, err := filepath.Abs(value)
		if err != nil {
			return err
		}
		// Refuse a directory Claude Code does not actually use. Accepting one
		// would produce switches that appear to succeed and change nothing.
		if err := config.ValidateClaudeDir(abs); err != nil {
			return err
		}
		s.ClaudeConfigDir = abs
		return nil

	case "vaultPath":
		if value == "" {
			s.VaultPath = ""
			return nil
		}
		abs, err := filepath.Abs(value)
		if err != nil {
			return err
		}
		s.VaultPath = abs
		return nil

	case "requireClosedSessions":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("requireClosedSessions must be true or false: %w", err)
		}
		s.RequireClosedSessions = &b
		return nil

	case "credentialsBackend":
		if _, err := config.ParseBackendPref(value); err != nil {
			return err
		}
		if value == "auto" {
			s.CredentialsBackend = ""
		} else {
			s.CredentialsBackend = value
		}
		return nil

	default:
		return fmt.Errorf("unknown key %q (valid: claudeConfigDir, vaultPath, requireClosedSessions, credentialsBackend)", key)
	}
}
