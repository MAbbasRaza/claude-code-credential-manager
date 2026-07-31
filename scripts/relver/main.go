// Command relver normalises a release tag into the forms the installers need.
//
//	go run ./scripts/relver v0.3.0-rc1
//	display=0.3.0-rc1
//	short=0.3.0
//	quad=0.3.0.0
//
// Both installers reject the tag we actually use. NSIS VIProductVersion
// requires exactly four numeric parts, and macOS CFBundleShortVersionString
// requires one to three. A tag like v0.3.0-rc1 therefore fails makensis outright
// and produces a bundle Finder shows with no version at all, and both failures
// arrive on a tag push, which is the worst moment to discover them.
//
// Written as a Go program rather than shell so the same normalisation serves
// PowerShell on Windows and bash on macOS, and so it can be tested.
package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: relver <version>")
		os.Exit(2)
	}
	v, err := Parse(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: "+err.Error())
		os.Exit(1)
	}
	// Shell-friendly key=value lines, so callers can read one field without a
	// JSON parser. PowerShell reads them with ConvertFrom-StringData, bash with
	// a read loop or eval.
	fmt.Printf("display=%s\n", v.Display)
	fmt.Printf("short=%s\n", v.Short)
	fmt.Printf("quad=%s\n", v.Quad)
}

// Version holds the three forms a release needs.
type Version struct {
	// Display is the human form, prerelease suffix intact: "0.3.0-rc1".
	Display string
	// Short is numeric, one to three parts: "0.3.0". Used for
	// CFBundleShortVersionString, which Finder refuses to show otherwise.
	Short string
	// Quad is exactly four numeric parts: "0.3.0.0". Used for NSIS
	// VIProductVersion, which will not compile without it.
	Quad string
}

// semverish matches a tag with an optional leading v, one to four numeric
// parts, and an optional prerelease or build suffix.
var semverish = regexp.MustCompile(`^v?([0-9]+(?:\.[0-9]+){0,3})([-+].*)?$`)

// Parse normalises a release tag.
//
// A development build has no tag at all. `git describe` then yields something
// like "0.3.0-4-gabc1234" or bare "gabc1234", and refusing those would mean a
// developer cannot build an installer without tagging first. Anything with no
// usable numeric prefix falls back to 0.0.0 rather than failing, since the
// version is cosmetic for a local build and fatal only for a release, which
// always has a tag.
func Parse(tag string) (Version, error) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return Version{}, fmt.Errorf("empty version")
	}

	m := semverish.FindStringSubmatch(tag)
	if m == nil {
		return Version{
			Display: strings.TrimPrefix(tag, "v"),
			Short:   "0.0.0",
			Quad:    "0.0.0.0",
		}, nil
	}

	numeric := m[1]
	parts := strings.Split(numeric, ".")

	// CFBundleShortVersionString accepts at most three parts.
	short := strings.Join(parts[:min(len(parts), 3)], ".")

	// VIProductVersion needs exactly four.
	quad := append([]string{}, parts...)
	for len(quad) < 4 {
		quad = append(quad, "0")
	}

	return Version{
		Display: strings.TrimPrefix(tag, "v"),
		Short:   short,
		Quad:    strings.Join(quad[:4], "."),
	}, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
