// Command genico writes assets/icon.ico from the embedded renderings in
// internal/icon.
//
// Windows needs the icon as a file on disk so windres can compile it into a
// resource object, but the multi-size ICO container is already assembled and
// tested in internal/icon. Generating the file from that single source keeps
// the tray icon, the executable icon and the container layout from drifting
// apart.
//
//	go run ./scripts/genico
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/MAbbasRaza/claude-code-multi-account-manager/internal/icon"
)

func main() {
	wd, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	if _, err := os.Stat(filepath.Join(wd, "go.mod")); err != nil {
		fatal(fmt.Errorf("run this from the repository root (no go.mod in %s)", wd))
	}

	out := filepath.Join(wd, "assets", "icon.ico")
	data := icon.ICO()
	if len(data) == 0 {
		fatal(fmt.Errorf("internal/icon produced no data"))
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("wrote %s (%d bytes)\n", out, len(data))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error: "+err.Error())
	os.Exit(1)
}
