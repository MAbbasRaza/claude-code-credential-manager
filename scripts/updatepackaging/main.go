// Command updatepackaging rewrites the Scoop manifest and Homebrew formula for
// a release.
//
//	go run ./scripts/updatepackaging <version> <path-to-SHA256SUMS>
//
// Written in Go rather than shell so it can be run and tested on the developer's
// machine instead of only ever executing on a tag, where a mistake means a
// broken published manifest that users install from. It also avoids depending
// on python3 or on GNU-specific sed behaviour.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: updatepackaging <version> <SHA256SUMS>")
		os.Exit(2)
	}
	version := strings.TrimPrefix(os.Args[1], "v")
	sumsPath := os.Args[2]

	root, err := repoRoot()
	if err != nil {
		fatal(err)
	}

	sumsData, err := os.ReadFile(sumsPath)
	if err != nil {
		fatal(fmt.Errorf("read %s: %w", sumsPath, err))
	}
	sums, err := parseSums(string(sumsData))
	if err != nil {
		fatal(err)
	}

	scoop := filepath.Join(root, "packaging", "scoop", "ccm.json")
	brew := filepath.Join(root, "packaging", "homebrew", "ccm.rb")

	if err := updateScoop(scoop, version, sums); err != nil {
		fatal(fmt.Errorf("scoop manifest: %w", err))
	}
	if err := updateHomebrew(brew, version, sums); err != nil {
		fatal(fmt.Errorf("homebrew formula: %w", err))
	}

	fmt.Printf("updated packaging manifests to %s\n", version)
	fmt.Printf("  %s\n", scoop)
	fmt.Printf("  %s\n", brew)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error: "+err.Error())
	os.Exit(1)
}

func repoRoot() (string, error) {
	// The tool is always invoked as `go run ./scripts/updatepackaging` from the
	// repository root, so the working directory is the root.
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(wd, "go.mod")); err != nil {
		return "", fmt.Errorf("run this from the repository root (no go.mod in %s)", wd)
	}
	return wd, nil
}

// sumsLine matches both the two-space and the space-star (binary mode) forms
// that sha256sum emits.
var sumsLine = regexp.MustCompile(`^([0-9a-fA-F]{64})\s+\*?(.+)$`)

// parseSums maps asset name to lowercase hex digest.
func parseSums(text string) (map[string]string, error) {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		m := sumsLine.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("unparseable checksum line: %q", line)
		}
		out[strings.TrimSpace(m[2])] = strings.ToLower(m[1])
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no checksums found")
	}
	return out, nil
}

func mustSum(sums map[string]string, asset string) (string, error) {
	h, ok := sums[asset]
	if !ok {
		return "", fmt.Errorf("no checksum published for %s", asset)
	}
	return h, nil
}

const downloadBase = "https://github.com/MAbbasRaza/claude-code-credential-manager/releases/download"

// updateScoop decodes and re-encodes the manifest so a malformed file fails
// here rather than after publication.
func updateScoop(path, version string, sums map[string]string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	arch, ok := m["architecture"].(map[string]any)
	if !ok {
		return fmt.Errorf("no architecture object")
	}

	for key, asset := range map[string]string{
		"64bit": "ccm-windows-amd64.exe",
		"arm64": "ccm-windows-arm64.exe",
	} {
		entry, ok := arch[key].(map[string]any)
		if !ok {
			return fmt.Errorf("no architecture.%s object", key)
		}
		h, err := mustSum(sums, asset)
		if err != nil {
			return err
		}
		entry["url"] = fmt.Sprintf("%s/v%s/%s#/ccm.exe", downloadBase, version, asset)
		entry["hash"] = h
	}

	m["version"] = version

	// An Encoder with escaping disabled, rather than json.MarshalIndent, which
	// would turn the "<name>" in the post_install text into "<name>".
	// That is still valid JSON, but the manifest is meant to be read by people.
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "    ")
	if err := enc.Encode(m); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(buf.String()), 0o644)
}

var (
	brewVersionRe = regexp.MustCompile(`(?m)^(\s*version\s+)"[^"]*"`)
	brewSha256Re  = regexp.MustCompile(`(?m)^(\s*sha256\s+)"[0-9a-fA-F]{64}"`)
)

// brewAssetOrder is the order the sha256 lines appear in the formula: macOS
// arm, macOS intel, Linux arm, Linux intel. Changing the formula's block order
// without changing this list would assign the wrong digests, so updateHomebrew
// verifies the count and there is a test pinning the order.
var brewAssetOrder = []string{
	"ccm-darwin-arm64",
	"ccm-darwin-amd64",
	"ccm-linux-arm64",
	"ccm-linux-amd64",
}

func updateHomebrew(path, version string, sums map[string]string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	src := string(data)

	if !brewVersionRe.MatchString(src) {
		return fmt.Errorf("could not find the version line")
	}
	src = brewVersionRe.ReplaceAllString(src, `${1}"`+version+`"`)

	found := brewSha256Re.FindAllString(src, -1)
	if len(found) != len(brewAssetOrder) {
		return fmt.Errorf("expected %d sha256 lines, found %d; the formula's block order changed",
			len(brewAssetOrder), len(found))
	}

	digests := make([]string, 0, len(brewAssetOrder))
	for _, asset := range brewAssetOrder {
		h, err := mustSum(sums, asset)
		if err != nil {
			return err
		}
		digests = append(digests, h)
	}

	i := 0
	src = brewSha256Re.ReplaceAllStringFunc(src, func(m string) string {
		sub := brewSha256Re.FindStringSubmatch(m)
		d := digests[i]
		i++
		return sub[1] + `"` + d + `"`
	})

	return os.WriteFile(path, []byte(src), 0o644)
}
