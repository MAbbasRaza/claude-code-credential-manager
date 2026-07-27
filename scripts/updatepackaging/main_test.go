package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSumsAcceptsBothFormats(t *testing.T) {
	// GNU sha256sum emits two spaces in text mode and " *" in binary mode.
	// Windows and macOS runners differ, so both must parse.
	text := "" +
		"786aa288f03f409d3ec97560081dbbec419c1af825d37cc87f3bd5e48bbbe647  ccm-darwin-amd64\n" +
		"410EA2E0A0B455F65F0CF78CAE15046DBF2BCAE174CFA3D465BF305FE9B2C974 *ccm-darwin-arm64\n" +
		"\n"

	got, err := parseSums(text)
	if err != nil {
		t.Fatalf("parseSums: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2: %v", len(got), got)
	}
	if got["ccm-darwin-amd64"] != "786aa288f03f409d3ec97560081dbbec419c1af825d37cc87f3bd5e48bbbe647" {
		t.Errorf("two-space form parsed wrong: %q", got["ccm-darwin-amd64"])
	}
	// Digests must be normalized to lowercase, since the manifests are compared
	// against lowercase values by Scoop and Homebrew.
	if got["ccm-darwin-arm64"] != "410ea2e0a0b455f65f0cf78cae15046dbf2bcae174cfa3d465bf305fe9b2c974" {
		t.Errorf("binary form not lowercased: %q", got["ccm-darwin-arm64"])
	}
}

func TestParseSumsRejectsGarbage(t *testing.T) {
	if _, err := parseSums("not a checksum line\n"); err == nil {
		t.Fatal("expected an error for an unparseable line")
	}
	if _, err := parseSums("\n\n"); err == nil {
		t.Fatal("expected an error when no checksums are present")
	}
}

func fakeSums() map[string]string {
	return map[string]string{
		"ccm-windows-amd64.exe": strings.Repeat("a", 64),
		"ccm-windows-arm64.exe": strings.Repeat("b", 64),
		"ccm-darwin-arm64":      strings.Repeat("c", 64),
		"ccm-darwin-amd64":      strings.Repeat("d", 64),
		"ccm-linux-arm64":       strings.Repeat("e", 64),
		"ccm-linux-amd64":       strings.Repeat("f", 64),
	}
}

// copyRepoFile stages a real manifest in a temp dir so the test exercises the
// file actually shipped rather than a simplified fixture.
func copyRepoFile(t *testing.T, rel string) string {
	t.Helper()
	src := filepath.Join("..", "..", rel)
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	dst := filepath.Join(t.TempDir(), filepath.Base(rel))
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

func TestUpdateScoop(t *testing.T) {
	path := copyRepoFile(t, "packaging/scoop/ccm.json")

	if err := updateScoop(path, "1.2.3", fakeSums()); err != nil {
		t.Fatalf("updateScoop: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	if m["version"] != "1.2.3" {
		t.Errorf("version = %v, want 1.2.3", m["version"])
	}
	arch := m["architecture"].(map[string]any)
	amd := arch["64bit"].(map[string]any)
	if amd["hash"] != strings.Repeat("a", 64) {
		t.Errorf("64bit hash = %v", amd["hash"])
	}
	wantURL := downloadBase + "/v1.2.3/ccm-windows-amd64.exe#/ccm.exe"
	if amd["url"] != wantURL {
		t.Errorf("64bit url = %v, want %v", amd["url"], wantURL)
	}
	arm := arch["arm64"].(map[string]any)
	if arm["hash"] != strings.Repeat("b", 64) {
		t.Errorf("arm64 hash = %v", arm["hash"])
	}

	// The manifest is read by humans, so angle brackets must survive as
	// themselves. Go's default JSON encoder escapes them to the six-character
	// unicode form, which is valid but unreadable.
	escapedLT := "\\u003c"
	escapedGT := "\\u003e"
	if strings.Contains(string(data), escapedLT) || strings.Contains(string(data), escapedGT) {
		t.Error("output contains HTML-escaped angle brackets; the Encoder must have SetEscapeHTML(false)")
	}
	if !strings.Contains(string(data), "ccm add <name>") {
		t.Error("expected the post_install text to keep its angle brackets verbatim")
	}

	// Fields the updater does not own must survive.
	if m["bin"] != "ccm.exe" {
		t.Errorf("bin field was lost: %v", m["bin"])
	}
	if _, ok := m["autoupdate"]; !ok {
		t.Error("autoupdate block was lost")
	}
	if _, ok := m["post_install"]; !ok {
		t.Error("post_install block was lost")
	}
}

func TestUpdateHomebrewAssignsDigestsInBlockOrder(t *testing.T) {
	path := copyRepoFile(t, "packaging/homebrew/ccm.rb")

	if err := updateHomebrew(path, "1.2.3", fakeSums()); err != nil {
		t.Fatalf("updateHomebrew: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)

	if !strings.Contains(src, `version "1.2.3"`) {
		t.Error("version was not updated")
	}

	// The digests are assigned positionally, so a reordered formula would
	// silently ship the wrong hash for a platform. Assert each digest lands in
	// the block whose URL names the matching asset.
	checks := []struct {
		asset  string
		digest string
	}{
		{"ccm-darwin-arm64", strings.Repeat("c", 64)},
		{"ccm-darwin-amd64", strings.Repeat("d", 64)},
		{"ccm-linux-arm64", strings.Repeat("e", 64)},
		{"ccm-linux-amd64", strings.Repeat("f", 64)},
	}
	for _, c := range checks {
		idx := strings.Index(src, c.asset+"\"")
		if idx < 0 {
			t.Fatalf("formula no longer references %s", c.asset)
		}
		rest := src[idx:]
		end := strings.Index(rest, "end")
		if end < 0 {
			end = len(rest)
		}
		if !strings.Contains(rest[:end], c.digest) {
			t.Errorf("%s block does not carry its digest; positional assignment is misaligned", c.asset)
		}
	}
}

func TestUpdateHomebrewFailsIfBlockCountChanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ccm.rb")
	// Only two sha256 lines instead of four.
	body := "class Ccm < Formula\n  version \"0.0.0\"\n" +
		"  sha256 \"" + strings.Repeat("0", 64) + "\"\n" +
		"  sha256 \"" + strings.Repeat("0", 64) + "\"\nend\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	err := updateHomebrew(path, "1.2.3", fakeSums())
	if err == nil {
		t.Fatal("expected an error when the formula's block count changes")
	}
	if !strings.Contains(err.Error(), "block order changed") {
		t.Errorf("error should explain the mismatch, got: %v", err)
	}
}

func TestMissingChecksumIsAnError(t *testing.T) {
	path := copyRepoFile(t, "packaging/scoop/ccm.json")
	partial := map[string]string{"ccm-windows-amd64.exe": strings.Repeat("a", 64)}

	if err := updateScoop(path, "1.2.3", partial); err == nil {
		t.Fatal("expected an error when a required checksum is absent")
	}
}
