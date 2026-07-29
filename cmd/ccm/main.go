// Command ccm switches between Claude Code accounts without re-authenticating.
//
// Claude Code keeps one account's auth state at a time, so signing into a
// second account destroys the first one's refresh token and returning to it
// requires a full browser authorization. ccm parks each account's state and
// swaps it back in on demand.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/MAbbasRaza/claude-code-credential-manager/internal/manager"
	"github.com/MAbbasRaza/claude-code-credential-manager/internal/vault"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

type globalOpts struct {
	configDir string
	jsonOut   bool
	force     bool
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ccm: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	var g globalOpts
	var rest []string

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--json":
			g.jsonOut = true
		case a == "--force" || a == "-f":
			g.force = true
		case a == "--config-dir":
			if i+1 >= len(args) {
				return errors.New("--config-dir requires a path")
			}
			i++
			g.configDir = args[i]
		case strings.HasPrefix(a, "--config-dir="):
			g.configDir = strings.TrimPrefix(a, "--config-dir=")
		case a == "--version" || a == "-v":
			fmt.Println("ccm " + version)
			return nil
		case a == "--help" || a == "-h" || a == "help":
			usage()
			return nil
		default:
			rest = append(rest, a)
		}
	}

	if len(rest) == 0 {
		return cmdPicker(g)
	}

	switch rest[0] {
	case "init":
		return cmdInit(g)
	case "list", "ls":
		return cmdList(g)
	case "use", "switch":
		if len(rest) < 2 {
			return errors.New("usage: ccm use <profile>")
		}
		return cmdUse(g, rest[1])
	case "add":
		name := ""
		if len(rest) > 1 {
			name = rest[1]
		}
		return cmdAdd(g, name)
	case "rm", "remove", "delete":
		if len(rest) < 2 {
			return errors.New("usage: ccm rm <profile>")
		}
		return cmdRemove(g, rest[1])
	case "rename", "mv":
		if len(rest) < 3 {
			return errors.New("usage: ccm rename <old-name> <new-name>")
		}
		return cmdRename(g, rest[1], rest[2])
	case "status":
		return cmdStatus(g)
	case "config":
		return cmdConfig(g, rest[1:])
	case "doctor":
		return cmdDoctor(g)
	default:
		return fmt.Errorf("unknown command %q (try: ccm --help)", rest[0])
	}
}

func usage() {
	fmt.Print(`ccm - Claude Code credential manager

Switch between Claude Code accounts without signing in again.

USAGE
  ccm                            interactive account picker
  ccm init                       detect and pin the Claude Code config directory
  ccm list                       list profiles, marking the active one
  ccm use <profile>              switch to a profile
  ccm add [name]                 capture the current login as a profile
  ccm rename <old> <new>         rename a profile, keeping its credentials
  ccm rm <profile>               remove a profile
  ccm status                     show the active account
  ccm config get|set|path        read or change ccm's own settings
  ccm doctor                     diagnose path resolution and vault health

FLAGS
  --config-dir <path>   override the Claude Code config directory for one run
  --json                machine-readable output
  --force, -f           proceed even if Claude Code is running
  --version, -v         print version
  --help, -h            this message

NOTES
  A switch takes effect when Claude Code next starts. Credentials are read at
  startup, so a running session keeps using the previous account.
`)
}

func emitJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func cmdStatus(g globalOpts) error {
	m, err := manager.Open(g.configDir)
	if err != nil {
		return err
	}
	st, err := m.Status()
	if err != nil {
		return err
	}
	if g.jsonOut {
		return emitJSON(st)
	}

	fmt.Printf("Config dir:  %s  (from %s)\n", st.ConfigDir, st.ConfigDirSource)
	fmt.Printf("Credentials: %s\n", m.Store.Describe())
	fmt.Printf("Account map: %s\n", st.ConfigJSONPath)
	if !st.LoggedIn {
		fmt.Println("\nNo active subscription login. Run /login in Claude Code.")
		return nil
	}
	fmt.Println()
	if st.EmailAddress != "" {
		fmt.Printf("Active:      %s\n", st.EmailAddress)
	}
	if st.Organization != "" {
		fmt.Printf("Org:         %s\n", st.Organization)
	}
	if st.Subscription != "" {
		fmt.Printf("Plan:        %s\n", st.Subscription)
	}
	if st.ExpiresAt != "" {
		fmt.Printf("Token expiry:%s %s\n", "", st.ExpiresAt)
	}
	if st.ActiveProfile != "" {
		fmt.Printf("Profile:     %s\n", st.ActiveProfile)
	} else {
		fmt.Println("Profile:     (not captured yet - run `ccm add <name>`)")
	}
	return nil
}

type listEntry struct {
	Name         string `json:"name"`
	Email        string `json:"email,omitempty"`
	Organization string `json:"organization,omitempty"`
	Subscription string `json:"subscription,omitempty"`
	Active       bool   `json:"active"`
	ExpiresAt    string `json:"expiresAt,omitempty"`
	Expired      bool   `json:"expired"`
	ExpiryIsLive bool   `json:"expiryIsLive"`
	LastUsedAt   string `json:"lastUsedAt,omitempty"`
}

func buildList(m *manager.Manager) ([]listEntry, manager.Status, error) {
	views, st, err := m.Profiles()
	if err != nil {
		return nil, st, err
	}
	var out []listEntry
	for _, v := range views {
		e := listEntry{
			Name:         v.Name,
			Email:        v.EmailAddress,
			Organization: v.OrganizationName,
			Subscription: v.SubscriptionType(),
			Active:       v.Active,
			ExpiryIsLive: v.ExpiryIsLive,
		}
		if !v.ExpiresAt.IsZero() {
			e.ExpiresAt = v.ExpiresAt.UTC().Format(time.RFC3339)
			e.Expired = v.Expired()
		}
		if !v.LastUsedAt.IsZero() {
			e.LastUsedAt = v.LastUsedAt.UTC().Format(time.RFC3339)
		}
		out = append(out, e)
	}
	return out, st, nil
}

func cmdList(g globalOpts) error {
	m, err := manager.Open(g.configDir)
	if err != nil {
		return err
	}
	entries, st, err := buildList(m)
	if err != nil {
		return err
	}
	if g.jsonOut {
		return emitJSON(map[string]any{"profiles": entries, "status": st})
	}
	if len(entries) == 0 {
		fmt.Println("No profiles yet.")
		fmt.Println("Sign in to Claude Code with /login, then run `ccm add <name>` to capture it.")
		return nil
	}
	for _, e := range entries {
		marker := " "
		if e.Active {
			marker = "*"
		}
		// An expired access token on a parked profile is the normal resting
		// state, not a problem: Claude Code exchanges the refresh token for a
		// new one on its next request. Only the active profile's expiry
		// describes something live, so only that one is worth reporting.
		note := ""
		switch {
		case e.ExpiryIsLive && e.Expired:
			note = "  access token lapsed, Claude Code will refresh it"
		case e.ExpiryIsLive:
			note = "  token valid until " + e.ExpiresAt
		case e.LastUsedAt != "":
			note = "  last used " + e.LastUsedAt
		}
		fmt.Printf("%s %-16s %-32s %-8s%s\n", marker, e.Name, e.Email, e.Subscription, note)
	}
	fmt.Println("\n* = currently active")
	return nil
}

func cmdAdd(g globalOpts, name string) error {
	m, err := manager.Open(g.configDir)
	if err != nil {
		return err
	}
	p, err := m.Capture(name)
	if err != nil {
		return err
	}
	if g.jsonOut {
		return emitJSON(map[string]any{"captured": p.Name, "email": p.EmailAddress})
	}
	fmt.Printf("Captured %s as profile %q.\n", p.EmailAddress, p.Name)
	fmt.Println("To add another account, run /logout then /login in Claude Code, and run `ccm add <name>` again.")
	return nil
}

func cmdRemove(g globalOpts, name string) error {
	m, err := manager.Open(g.configDir)
	if err != nil {
		return err
	}
	if err := m.Remove(name); err != nil {
		return err
	}
	if g.jsonOut {
		return emitJSON(map[string]any{"removed": name})
	}
	fmt.Printf("Removed profile %q.\n", name)
	return nil
}

func cmdRename(g globalOpts, oldName, newName string) error {
	m, err := manager.Open(g.configDir)
	if err != nil {
		return err
	}
	if err := m.Rename(oldName, newName); err != nil {
		if errors.Is(err, vault.ErrNotFound) {
			return fmt.Errorf("%w\nRun `ccm list` to see available profiles", err)
		}
		return err
	}
	if g.jsonOut {
		return emitJSON(map[string]any{"renamed": oldName, "to": newName})
	}
	fmt.Printf("Renamed %q to %q. Its stored credentials are unchanged.\n", oldName, newName)
	return nil
}

func cmdUse(g globalOpts, target string) error {
	m, err := manager.Open(g.configDir)
	if err != nil {
		return err
	}
	res, err := m.Switch(target, g.force)
	if err != nil {
		if errors.Is(err, vault.ErrNotFound) {
			return fmt.Errorf("%w\nRun `ccm list` to see available profiles", err)
		}
		return err
	}
	if g.jsonOut {
		return emitJSON(res)
	}
	if res.CapturedAs != "" {
		verb := "updated"
		if res.CapturedNew {
			verb = "saved as new profile"
		}
		fmt.Printf("Captured outgoing account %s (%s %q).\n", res.FromEmail, verb, res.CapturedAs)
	}
	fmt.Printf("Switched to %q", res.To)
	if res.ToEmail != "" {
		fmt.Printf(" (%s)", res.ToEmail)
	}
	fmt.Println(".")
	fmt.Println(res.RestartWarning)
	return nil
}

func cmdPicker(g globalOpts) error {
	m, err := manager.Open(g.configDir)
	if err != nil {
		return err
	}
	entries, _, err := buildList(m)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Println("No profiles yet.")
		fmt.Println("Sign in to Claude Code with /login, then run `ccm add <name>` to capture it.")
		return nil
	}

	fmt.Println("Select an account:")
	for i, e := range entries {
		marker := " "
		if e.Active {
			marker = "*"
		}
		note := ""
		if e.Expired {
			note = "  (token expired)"
		}
		fmt.Printf("  %s %d) %-16s %s%s\n", marker, i+1, e.Name, e.Email, note)
	}
	fmt.Print("\nNumber (or blank to cancel): ")

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return nil
	}
	// Windows shells readily prepend a UTF-8 BOM when input is piped rather
	// than typed, which would otherwise be read as a bogus selection.
	line = strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
	if line == "" {
		fmt.Println("Cancelled.")
		return nil
	}
	n, err := strconv.Atoi(line)
	if err != nil || n < 1 || n > len(entries) {
		return fmt.Errorf("invalid selection %q", line)
	}
	return cmdUse(g, entries[n-1].Name)
}
