package main

import (
	"regexp"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		tag                     string
		display, short, quadant string
	}{
		{"v0.3.0", "0.3.0", "0.3.0", "0.3.0.0"},
		{"0.3.0", "0.3.0", "0.3.0", "0.3.0.0"},

		// The case that motivated this: a prerelease tag compiles to nothing
		// under NSIS and shows blank in Finder.
		{"v0.3.0-rc1", "0.3.0-rc1", "0.3.0", "0.3.0.0"},
		{"v1.2.3-beta.4", "1.2.3-beta.4", "1.2.3", "1.2.3.0"},
		{"v1.2.3+build9", "1.2.3+build9", "1.2.3", "1.2.3.0"},

		// Short and long numeric forms.
		{"v1", "1", "1", "1.0.0.0"},
		{"v1.2", "1.2", "1.2", "1.2.0.0"},
		{"v1.2.3.4", "1.2.3.4", "1.2.3", "1.2.3.4"},

		// git describe output on an untagged commit. Must not fail: a developer
		// has to be able to build an installer without tagging first.
		{"0.3.0-4-gabc1234", "0.3.0-4-gabc1234", "0.3.0", "0.3.0.0"},
		{"gabc1234", "gabc1234", "0.0.0", "0.0.0.0"},
		{"dev", "dev", "0.0.0", "0.0.0.0"},
	}

	for _, tc := range tests {
		t.Run(tc.tag, func(t *testing.T) {
			v, err := Parse(tc.tag)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.tag, err)
			}
			if v.Display != tc.display {
				t.Errorf("Display = %q, want %q", v.Display, tc.display)
			}
			if v.Short != tc.short {
				t.Errorf("Short = %q, want %q", v.Short, tc.short)
			}
			if v.Quad != tc.quadant {
				t.Errorf("Quad = %q, want %q", v.Quad, tc.quadant)
			}
		})
	}
}

func TestParseRejectsEmpty(t *testing.T) {
	if _, err := Parse("  "); err == nil {
		t.Error("an empty version should be an error, not a silent 0.0.0")
	}
}

// The whole point of Quad is that makensis accepts it. Assert the shape rather
// than trusting the loop above, since a regression here fails only on a tag.
func TestQuadIsAlwaysFourNumericParts(t *testing.T) {
	shape := regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$`)
	for _, tag := range []string{"v1", "v1.2", "v1.2.3", "v1.2.3.4", "v0.3.0-rc1", "dev", "gabc1234"} {
		v, err := Parse(tag)
		if err != nil {
			t.Fatal(err)
		}
		if !shape.MatchString(v.Quad) {
			t.Errorf("Parse(%q).Quad = %q, which VIProductVersion will reject", tag, v.Quad)
		}
	}
}

// CFBundleShortVersionString accepts at most three parts; Finder shows nothing
// for a four-part value.
func TestShortIsAtMostThreeNumericParts(t *testing.T) {
	shape := regexp.MustCompile(`^[0-9]+(\.[0-9]+){0,2}$`)
	for _, tag := range []string{"v1", "v1.2", "v1.2.3", "v1.2.3.4", "v0.3.0-rc1", "dev"} {
		v, err := Parse(tag)
		if err != nil {
			t.Fatal(err)
		}
		if !shape.MatchString(v.Short) {
			t.Errorf("Parse(%q).Short = %q, which Finder will not display", tag, v.Short)
		}
		if strings.Count(v.Short, ".") > 2 {
			t.Errorf("Parse(%q).Short = %q has too many parts", tag, v.Short)
		}
	}
}
