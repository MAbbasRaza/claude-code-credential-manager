// Package config resolves which Claude Code installation ccm is managing.
//
// Getting this wrong is the most damaging failure mode in the tool: writing an
// account's credentials into the wrong directory both fails to switch and can
// resurrect a stale login. Resolution is therefore explicit and reports which
// precedence level supplied the answer, so `ccm doctor` can prove that the CLI,
// the tray app and the VS Code extension all agree.
package config

import (
	"os"
	"path/filepath"
	"runtime"
)

// Source identifies which precedence level supplied the config directory.
type Source string

const (
	SourceFlag      Source = "--config-dir flag"
	SourceEnvCCM    Source = "CCM_CLAUDE_CONFIG_DIR"
	SourceSettings  Source = "ccm settings file"
	SourceEnvClaude Source = "CLAUDE_CONFIG_DIR"
	SourceDefault   Source = "platform default"
)

// Backend identifies where Claude Code keeps the credentials document.
type Backend string

const (
	BackendFile     Backend = "file"
	BackendKeychain Backend = "macos-keychain"
)

// EnvClaudeConfigDir is Claude Code's own environment variable. Per the
// official docs it relocates .credentials.json on Linux and Windows only;
// macOS always uses the Keychain.
const EnvClaudeConfigDir = "CLAUDE_CONFIG_DIR"

// EnvCCMConfigDir is ccm's override, used when a user wants to point ccm at a
// different installation than the ambient CLAUDE_CONFIG_DIR names.
const EnvCCMConfigDir = "CCM_CLAUDE_CONFIG_DIR"

// KeychainService is the macOS Keychain generic-password item Claude Code uses.
const KeychainService = "Claude Code-credentials"

// Paths is a fully resolved view of one Claude Code installation.
type Paths struct {
	// Dir is the resolved Claude Code config directory.
	Dir string
	// Source records which precedence level supplied Dir.
	Source Source
	// Backend is where the credentials document lives.
	Backend Backend
	// CredentialsPath is empty when Backend is BackendKeychain.
	CredentialsPath string
	// ConfigJSONPath is the .claude.json holding oauthAccount and userID.
	ConfigJSONPath string
}

// Resolver reads the ambient environment. It is an interface so tests can drive
// every precedence level without mutating the real process environment.
type Resolver struct {
	// Getenv defaults to os.Getenv.
	Getenv func(string) string
	// HomeDir defaults to os.UserHomeDir.
	HomeDir func() (string, error)
	// GOOS defaults to runtime.GOOS.
	GOOS string
}

// NewResolver returns a Resolver bound to the real process environment.
func NewResolver() *Resolver {
	return &Resolver{Getenv: os.Getenv, HomeDir: os.UserHomeDir, GOOS: runtime.GOOS}
}

func (r *Resolver) getenv(k string) string {
	if r.Getenv == nil {
		return os.Getenv(k)
	}
	return r.Getenv(k)
}

func (r *Resolver) home() (string, error) {
	if r.HomeDir == nil {
		return os.UserHomeDir()
	}
	return r.HomeDir()
}

func (r *Resolver) goos() string {
	if r.GOOS == "" {
		return runtime.GOOS
	}
	return r.GOOS
}

// Resolve walks the five precedence levels, highest first:
//
//  1. --config-dir flag
//  2. CCM_CLAUDE_CONFIG_DIR
//  3. claudeConfigDir in ccm's settings file
//  4. CLAUDE_CONFIG_DIR, when inherited
//  5. platform default
//
// settingsDir may be empty when no settings file is present or it does not pin
// a directory. Levels 1 and 2 are per-invocation escape hatches; level 3 is what
// makes the CLI, tray and extension agree even when they inherit different
// environments.
func (r *Resolver) Resolve(flagDir, settingsDir string) (Paths, error) {
	dir, src, err := r.resolveDir(flagDir, settingsDir)
	if err != nil {
		return Paths{}, err
	}
	return r.pathsFor(dir, src)
}

func (r *Resolver) resolveDir(flagDir, settingsDir string) (string, Source, error) {
	if flagDir != "" {
		return flagDir, SourceFlag, nil
	}
	if v := r.getenv(EnvCCMConfigDir); v != "" {
		return v, SourceEnvCCM, nil
	}
	if settingsDir != "" {
		return settingsDir, SourceSettings, nil
	}
	if v := r.getenv(EnvClaudeConfigDir); v != "" {
		return v, SourceEnvClaude, nil
	}
	home, err := r.home()
	if err != nil {
		return "", "", err
	}
	return filepath.Join(home, ".claude"), SourceDefault, nil
}

// pathsFor derives file locations from a config directory.
//
// There is a real asymmetry here, verified against a live install. In the
// default layout the two files do NOT sit together: credentials live inside
// ~/.claude/ while .claude.json sits beside it at ~/.claude.json. Once
// CLAUDE_CONFIG_DIR is set, both move inside that directory. Treating .claude.json
// as always living next to .credentials.json silently targets a file Claude Code
// never reads.
func (r *Resolver) pathsFor(dir string, src Source) (Paths, error) {
	p := Paths{Dir: dir, Source: src}

	if src == SourceDefault {
		home, err := r.home()
		if err != nil {
			return Paths{}, err
		}
		p.ConfigJSONPath = filepath.Join(home, ".claude.json")
	} else {
		p.ConfigJSONPath = filepath.Join(dir, ".claude.json")
	}

	if r.goos() == "darwin" {
		// macOS keeps credentials in the Keychain regardless of
		// CLAUDE_CONFIG_DIR, but .claude.json is still an ordinary file.
		p.Backend = BackendKeychain
		p.CredentialsPath = ""
	} else {
		p.Backend = BackendFile
		p.CredentialsPath = filepath.Join(dir, ".credentials.json")
	}
	return p, nil
}

// Candidate is one precedence level's answer, used by `ccm doctor` to show
// disagreement between levels rather than silently taking the winner.
type Candidate struct {
	Source Source
	Dir    string
	Set    bool
}

// Candidates reports every precedence level independently. When two levels name
// different directories, a CLI run and a GUI-launched tray can diverge, which is
// exactly the misconfiguration doctor must surface.
func (r *Resolver) Candidates(flagDir, settingsDir string) []Candidate {
	out := []Candidate{
		{Source: SourceFlag, Dir: flagDir, Set: flagDir != ""},
		{Source: SourceEnvCCM, Dir: r.getenv(EnvCCMConfigDir), Set: r.getenv(EnvCCMConfigDir) != ""},
		{Source: SourceSettings, Dir: settingsDir, Set: settingsDir != ""},
		{Source: SourceEnvClaude, Dir: r.getenv(EnvClaudeConfigDir), Set: r.getenv(EnvClaudeConfigDir) != ""},
	}
	if home, err := r.home(); err == nil {
		out = append(out, Candidate{Source: SourceDefault, Dir: filepath.Join(home, ".claude"), Set: true})
	}
	return out
}

// Disagreement reports the set-but-conflicting directories among candidates.
// An empty result means every level that asserts a directory agrees.
//
// SourceDefault is excluded deliberately. It is a fallback, not a claim about
// where Claude Code lives, and it differs from an explicit setting on every
// machine that has one. Counting it would make the conflict warning fire for
// every user who sets CLAUDE_CONFIG_DIR, which is precisely the audience whose
// real conflicts the warning needs to stay credible for.
func Disagreement(cands []Candidate) []Candidate {
	var set []Candidate
	for _, c := range cands {
		if c.Set && c.Dir != "" && c.Source != SourceDefault {
			set = append(set, c)
		}
	}
	if len(set) < 2 {
		return nil
	}
	first := normalize(set[0].Dir)
	for _, c := range set[1:] {
		if normalize(c.Dir) != first {
			return set
		}
	}
	return nil
}

func normalize(p string) string {
	c := filepath.Clean(p)
	if runtime.GOOS == "windows" {
		// Windows paths are case-insensitive, including the drive letter.
		// A live .claude.json on this platform was observed holding both
		// "F:/..." and "f:/..." spellings of one directory, so folding the
		// whole path (not just the tail) is required.
		return lower(c)
	}
	return c
}

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
