// Command genpng writes one PNG rendering from internal/icon to a file.
//
//	go run ./scripts/genpng 48 /tmp/ccm-48.png
//
// The Linux package needs the icon on disk at each hicolor theme size. Taking
// them from internal/icon rather than re-cutting the master artwork keeps the
// menu entry, the tray icon and the Windows executable icon from drifting
// apart, which is the same reason scripts/genico exists.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/MAbbasRaza/claude-code-multi-account-manager/internal/icon"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: genpng <size> <output path>")
		os.Exit(2)
	}

	size, err := strconv.Atoi(os.Args[1])
	if err != nil || size <= 0 {
		fatal(fmt.Errorf("invalid size %q", os.Args[1]))
	}
	out := os.Args[2]

	data := icon.PNGSize(size)
	if len(data) == 0 {
		fatal(fmt.Errorf("internal/icon produced no data for size %d", size))
	}

	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error: "+err.Error())
	os.Exit(1)
}
