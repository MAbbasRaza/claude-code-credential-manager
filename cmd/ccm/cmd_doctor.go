package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/MAbbasRaza/claude-code-multi-account-manager/internal/config"
	"github.com/MAbbasRaza/claude-code-multi-account-manager/internal/manager"
	"github.com/MAbbasRaza/claude-code-multi-account-manager/internal/proc"
)

type doctorReport struct {
	Version    string            `json:"version"`
	Settings   string            `json:"settingsPath"`
	Candidates []doctorCandidate `json:"candidates"`
	Conflict   bool              `json:"conflict"`
	Resolved   manager.Status    `json:"resolved"`
	Files      []doctorFile      `json:"files"`
	Vault      doctorVault       `json:"vault"`
	Duplicates []doctorDuplicate `json:"duplicateAccounts,omitempty"`
	Running    []doctorProc      `json:"runningClaudeProcesses"`
	Warnings   []string          `json:"warnings"`
}

type doctorDuplicate struct {
	AccountUUID string   `json:"accountUuid"`
	Profiles    []string `json:"profiles"`
}

type doctorCandidate struct {
	Source string `json:"source"`
	Dir    string `json:"dir"`
	Set    bool   `json:"set"`
}

type doctorFile struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Size   int64  `json:"size"`
	Mode   string `json:"mode,omitempty"`
}

type doctorVault struct {
	Path     string `json:"path"`
	Sealer   string `json:"sealer"`
	Profiles int    `json:"profiles"`
	Expired  int    `json:"expiredProfiles"`
}

type doctorProc struct {
	PID  int    `json:"pid"`
	Name string `json:"name"`
}

func cmdDoctor(g globalOpts) error {
	rep := doctorReport{Version: version}

	s, err := config.LoadSettings()
	if err != nil {
		return err
	}
	rep.Settings = s.Path()

	r := config.NewResolver()
	cands := r.Candidates(g.configDir, s.ClaudeConfigDir)
	for _, c := range cands {
		rep.Candidates = append(rep.Candidates, doctorCandidate{
			Source: string(c.Source), Dir: c.Dir, Set: c.Set,
		})
	}
	if conflict := config.Disagreement(cands); len(conflict) > 0 {
		rep.Conflict = true
		rep.Warnings = append(rep.Warnings,
			"Precedence levels disagree about the Claude Code config directory. "+
				"A CLI run and a GUI-launched tray or extension can resolve differently. "+
				"Run `ccm init` to pin one directory for every surface.")
	}

	m, err := manager.Open(g.configDir)
	if err != nil {
		return err
	}
	st, err := m.Status()
	if err != nil {
		return err
	}
	rep.Resolved = st

	files := []string{m.Paths.ConfigJSONPath}
	if m.Paths.CredentialsPath != "" {
		files = append(files, m.Paths.CredentialsPath)
	}
	for _, f := range files {
		df := doctorFile{Path: f}
		if fi, err := os.Stat(f); err == nil {
			df.Exists = true
			df.Size = fi.Size()
			df.Mode = fi.Mode().Perm().String()
		}
		rep.Files = append(rep.Files, df)
	}

	rep.Vault = doctorVault{
		Path:     m.Vault.Path,
		Sealer:   m.Vault.SealerName(),
		Profiles: len(m.Vault.List()),
	}
	// Live-aware, so the active profile is judged by the credentials Claude Code
	// is actually using rather than by the snapshot taken at capture time.
	views, _, err := m.Profiles()
	if err != nil {
		return err
	}
	for _, v := range views {
		if v.Expired() {
			rep.Vault.Expired++
		}
	}
	// Duplicates predate the guard in Capture, so an older vault can still
	// hold them. They are worth flagging loudly: only one copy receives
	// refreshed tokens, and the other decays into a dead refresh token.
	for uuid, names := range m.Vault.DuplicateAccounts() {
		rep.Duplicates = append(rep.Duplicates, doctorDuplicate{AccountUUID: uuid, Profiles: names})
		rep.Warnings = append(rep.Warnings, fmt.Sprintf(
			"Profiles %s all hold the same account. Only one will receive refreshed tokens; "+
				"the others will go stale. Remove the extras with `ccm rm <name>`.",
			strings.Join(names, ", ")))
	}
	sort.Slice(rep.Duplicates, func(i, j int) bool {
		return rep.Duplicates[i].AccountUUID < rep.Duplicates[j].AccountUUID
	})

	// Counted above but deliberately not warned about. A parked profile's access
	// token is expected to be expired: it was captured at a moment in time and
	// Claude Code exchanges the refresh token for a new one on its next request.
	// The old warning reported a problem where there was none, and pointed at
	// /login, which destroys the refresh token that makes the profile work.

	if procs, err := proc.FindClaude(); err != nil {
		rep.Warnings = append(rep.Warnings, "Could not enumerate processes: "+err.Error())
	} else {
		for _, p := range procs {
			rep.Running = append(rep.Running, doctorProc{PID: p.PID, Name: p.Name})
		}
		if len(procs) > 0 {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf(
				"Claude Code is running (%d process(es)). Switching now would be undone when it exits.",
				len(procs)))
		}
	}

	if !st.LoggedIn {
		rep.Warnings = append(rep.Warnings, "No active subscription login found.")
	}
	if st.LoggedIn && st.ActiveProfile == "" {
		rep.Warnings = append(rep.Warnings,
			"The active account is not in the vault. Run `ccm add <name>` so it can be switched back to.")
	}

	if g.jsonOut {
		return emitJSON(rep)
	}
	printDoctor(rep)
	return nil
}

func printDoctor(rep doctorReport) {
	fmt.Printf("ccm %s\n\n", rep.Version)

	fmt.Println("Config directory resolution (highest precedence first)")
	for _, c := range rep.Candidates {
		mark := "  "
		if c.Set {
			mark = "->"
		}
		val := c.Dir
		if !c.Set || val == "" {
			val = "(not set)"
		}
		fmt.Printf("  %s %-24s %s\n", mark, c.Source, val)
	}
	fmt.Printf("\n  Resolved: %s  (from %s)\n", rep.Resolved.ConfigDir, rep.Resolved.ConfigDirSource)
	fmt.Printf("  Settings: %s\n", rep.Settings)

	fmt.Println("\nFiles")
	for _, f := range rep.Files {
		if f.Exists {
			fmt.Printf("  ok      %s  (%d bytes, mode %s)\n", f.Path, f.Size, f.Mode)
		} else {
			fmt.Printf("  missing %s\n", f.Path)
		}
	}
	fmt.Printf("  backend %s\n", rep.Resolved.Backend)

	fmt.Println("\nVault")
	fmt.Printf("  path      %s\n", rep.Vault.Path)
	fmt.Printf("  protection %s\n", rep.Vault.Sealer)
	fmt.Printf("  profiles  %d (%d with an expired token)\n", rep.Vault.Profiles, rep.Vault.Expired)

	fmt.Println("\nActive account")
	if rep.Resolved.LoggedIn {
		fmt.Printf("  %s", orNone(rep.Resolved.EmailAddress))
		if rep.Resolved.Organization != "" {
			fmt.Printf("  [%s]", rep.Resolved.Organization)
		}
		if rep.Resolved.Subscription != "" {
			fmt.Printf("  plan=%s", rep.Resolved.Subscription)
		}
		fmt.Println()
		if rep.Resolved.ActiveProfile != "" {
			fmt.Printf("  profile: %s\n", rep.Resolved.ActiveProfile)
		}
	} else {
		fmt.Println("  not signed in")
	}

	if len(rep.Running) > 0 {
		fmt.Println("\nRunning Claude Code processes")
		for _, p := range rep.Running {
			fmt.Printf("  pid %d  %s\n", p.PID, p.Name)
		}
	}

	if len(rep.Warnings) == 0 {
		fmt.Println("\nNo warnings.")
		return
	}
	fmt.Println("\nWarnings")
	for _, w := range rep.Warnings {
		fmt.Printf("  - %s\n", w)
	}
}

func orNone(s string) string {
	if s == "" {
		return "(unknown)"
	}
	return s
}
