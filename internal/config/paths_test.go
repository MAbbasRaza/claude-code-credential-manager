package config

import (
	"path/filepath"
	"testing"
)

// fakeResolver builds a Resolver with a controlled environment so every
// precedence level can be exercised without touching the real process env.
func fakeResolver(goos, home string, env map[string]string) *Resolver {
	return &Resolver{
		Getenv:  func(k string) string { return env[k] },
		HomeDir: func() (string, error) { return home, nil },
		GOOS:    goos,
	}
}

func TestResolvePrecedence(t *testing.T) {
	const home = "/home/u"

	cases := []struct {
		name       string
		flag       string
		settings   string
		env        map[string]string
		wantDir    string
		wantSource Source
	}{
		{
			name:       "flag beats everything",
			flag:       "/from/flag",
			settings:   "/from/settings",
			env:        map[string]string{EnvCCMConfigDir: "/from/ccmenv", EnvClaudeConfigDir: "/from/claudeenv"},
			wantDir:    "/from/flag",
			wantSource: SourceFlag,
		},
		{
			name:       "CCM env beats settings and claude env",
			settings:   "/from/settings",
			env:        map[string]string{EnvCCMConfigDir: "/from/ccmenv", EnvClaudeConfigDir: "/from/claudeenv"},
			wantDir:    "/from/ccmenv",
			wantSource: SourceEnvCCM,
		},
		{
			name:       "settings beats claude env",
			settings:   "/from/settings",
			env:        map[string]string{EnvClaudeConfigDir: "/from/claudeenv"},
			wantDir:    "/from/settings",
			wantSource: SourceSettings,
		},
		{
			name:       "claude env beats default",
			env:        map[string]string{EnvClaudeConfigDir: "/from/claudeenv"},
			wantDir:    "/from/claudeenv",
			wantSource: SourceEnvClaude,
		},
		{
			name:       "default when nothing is set",
			env:        map[string]string{},
			wantDir:    filepath.Join(home, ".claude"),
			wantSource: SourceDefault,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := fakeResolver("linux", home, tc.env)
			got, err := r.Resolve(tc.flag, tc.settings)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got.Dir != tc.wantDir {
				t.Errorf("Dir = %q, want %q", got.Dir, tc.wantDir)
			}
			if got.Source != tc.wantSource {
				t.Errorf("Source = %q, want %q", got.Source, tc.wantSource)
			}
		})
	}
}

// The two auth documents do not live together in the default layout: the
// credentials file sits inside ~/.claude while .claude.json sits beside it at
// ~/.claude.json. Once CLAUDE_CONFIG_DIR is set, both move inside that
// directory. Assuming they are always siblings targets a file Claude Code
// never reads.
func TestConfigJSONLocationAsymmetry(t *testing.T) {
	const home = "/home/u"

	t.Run("default layout keeps .claude.json at home root", func(t *testing.T) {
		r := fakeResolver("linux", home, map[string]string{})
		p, err := r.Resolve("", "")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if want := filepath.Join(home, ".claude.json"); p.ConfigJSONPath != want {
			t.Errorf("ConfigJSONPath = %q, want %q", p.ConfigJSONPath, want)
		}
		if want := filepath.Join(home, ".claude", ".credentials.json"); p.CredentialsPath != want {
			t.Errorf("CredentialsPath = %q, want %q", p.CredentialsPath, want)
		}
	})

	t.Run("explicit dir puts both inside it", func(t *testing.T) {
		r := fakeResolver("linux", home, map[string]string{EnvClaudeConfigDir: "/opt/claude"})
		p, err := r.Resolve("", "")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if want := filepath.Join("/opt/claude", ".claude.json"); p.ConfigJSONPath != want {
			t.Errorf("ConfigJSONPath = %q, want %q", p.ConfigJSONPath, want)
		}
		if want := filepath.Join("/opt/claude", ".credentials.json"); p.CredentialsPath != want {
			t.Errorf("CredentialsPath = %q, want %q", p.CredentialsPath, want)
		}
	})
}

func TestBackendPerPlatform(t *testing.T) {
	const home = "/home/u"

	t.Run("darwin uses the keychain and ignores CLAUDE_CONFIG_DIR for credentials", func(t *testing.T) {
		r := fakeResolver("darwin", home, map[string]string{EnvClaudeConfigDir: "/opt/claude"})
		p, err := r.Resolve("", "")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if p.Backend != BackendKeychain {
			t.Errorf("Backend = %q, want %q", p.Backend, BackendKeychain)
		}
		if p.CredentialsPath != "" {
			t.Errorf("CredentialsPath = %q, want empty on darwin", p.CredentialsPath)
		}
		// .claude.json is still an ordinary file even on macOS.
		if want := filepath.Join("/opt/claude", ".claude.json"); p.ConfigJSONPath != want {
			t.Errorf("ConfigJSONPath = %q, want %q", p.ConfigJSONPath, want)
		}
	})

	for _, goos := range []string{"windows", "linux"} {
		t.Run(goos+" uses a file", func(t *testing.T) {
			r := fakeResolver(goos, home, map[string]string{EnvClaudeConfigDir: "/opt/claude"})
			p, err := r.Resolve("", "")
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if p.Backend != BackendFile {
				t.Errorf("Backend = %q, want %q", p.Backend, BackendFile)
			}
			if p.CredentialsPath == "" {
				t.Error("CredentialsPath must be set for the file backend")
			}
		})
	}
}

// The failure this pinning exists to prevent: a CLI started from a shell
// inherits CLAUDE_CONFIG_DIR, a tray app started from the desktop does not.
// Without a pinned setting they resolve different directories; with one they
// agree.
func TestSplitResolutionBetweenCLIAndGUI(t *testing.T) {
	const home = "/home/u"
	const actual = "/opt/claude"

	cliEnv := map[string]string{EnvClaudeConfigDir: actual} // shell-launched
	guiEnv := map[string]string{}                           // desktop-launched

	t.Run("without a pinned setting the surfaces disagree", func(t *testing.T) {
		cli, err := fakeResolver("linux", home, cliEnv).Resolve("", "")
		if err != nil {
			t.Fatal(err)
		}
		gui, err := fakeResolver("linux", home, guiEnv).Resolve("", "")
		if err != nil {
			t.Fatal(err)
		}
		if cli.Dir == gui.Dir {
			t.Fatalf("expected divergence, both resolved %q", cli.Dir)
		}
		if gui.Dir != filepath.Join(home, ".claude") {
			t.Errorf("GUI resolved %q, want the stale default %q", gui.Dir, filepath.Join(home, ".claude"))
		}
	})

	t.Run("after ccm init pins the directory both agree", func(t *testing.T) {
		pinned := actual
		cli, err := fakeResolver("linux", home, cliEnv).Resolve("", pinned)
		if err != nil {
			t.Fatal(err)
		}
		gui, err := fakeResolver("linux", home, guiEnv).Resolve("", pinned)
		if err != nil {
			t.Fatal(err)
		}
		if cli.Dir != gui.Dir {
			t.Fatalf("CLI resolved %q but GUI resolved %q", cli.Dir, gui.Dir)
		}
		if cli.Dir != actual {
			t.Errorf("resolved %q, want %q", cli.Dir, actual)
		}
		if cli.Source != SourceSettings || gui.Source != SourceSettings {
			t.Errorf("both should report the settings file as the source, got %q and %q", cli.Source, gui.Source)
		}
	})
}

func TestDisagreementDetection(t *testing.T) {
	const home = "/home/u"

	t.Run("reports conflict", func(t *testing.T) {
		r := fakeResolver("linux", home, map[string]string{EnvClaudeConfigDir: "/opt/claude"})
		cands := r.Candidates("", "/somewhere/else")
		if got := Disagreement(cands); len(got) == 0 {
			t.Fatal("expected a conflict to be reported")
		}
	})

	t.Run("silent when the asserting levels agree", func(t *testing.T) {
		r := fakeResolver("linux", home, map[string]string{EnvClaudeConfigDir: "/opt/claude"})
		cands := r.Candidates("", "/opt/claude")
		if got := Disagreement(cands); len(got) != 0 {
			t.Fatalf("expected no conflict, got %+v", got)
		}
	})

	// Regression: the platform default differs from an explicit setting on
	// every machine that has one. Counting it made the warning fire for every
	// user with CLAUDE_CONFIG_DIR set, which is noise rather than a diagnosis.
	t.Run("platform default alone is not a conflict", func(t *testing.T) {
		r := fakeResolver("linux", home, map[string]string{EnvClaudeConfigDir: "/opt/claude"})
		cands := r.Candidates("", "")
		if got := Disagreement(cands); len(got) != 0 {
			t.Fatalf("the default fallback must not count as a disagreement, got %+v", got)
		}
	})

	t.Run("default is ignored even when it is the only level set", func(t *testing.T) {
		r := fakeResolver("linux", home, map[string]string{})
		cands := r.Candidates("", "")
		if got := Disagreement(cands); len(got) != 0 {
			t.Fatalf("expected no conflict with nothing configured, got %+v", got)
		}
	})
}
