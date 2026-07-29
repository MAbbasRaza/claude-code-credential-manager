// Command ccm-gui is the desktop application for managing Claude Code accounts.
//
// It exists because the other surfaces each have a hard limit. A tray menu
// cannot take text input, so renaming is impossible there, and the VS Code
// extension only helps people working inside VS Code. This is the standalone
// manager: list, switch, capture, rename, remove, plus diagnostics and settings.
//
// Like the tray it links internal/manager directly rather than shelling out, so
// there is one implementation of the switch algorithm across every surface.
//
// # Why a webview and not a drawn toolkit
//
// The first version used Fyne, which renders through OpenGL. It crashed at
// window creation on the development machine, and a ten-line Fyne program
// crashed identically, so the fault was the toolkit against that graphics
// setup rather than this code. Shipping a GUI that cannot start is worse than
// shipping none, so this uses the platform webview instead: WebView2 on
// Windows, WKWebView on macOS, WebKitGTK on Linux. None of them need OpenGL,
// they are already present on a normal desktop, and the binary is a tenth of
// the size.
package main

import (
	_ "embed"
	"fmt"
	"os"
	"strings"

	"github.com/webview/webview_go"
)

var version = "dev"

//go:embed ui.html
var uiHTML string

func main() {
	w := webview.New(os.Getenv("CCM_GUI_DEBUG") == "1")
	defer w.Destroy()

	w.SetTitle("Claude Code Accounts")
	w.SetSize(940, 640, webview.HintNone)

	api := &api{win: w}
	bind(w, api)

	// Must come after SetSize, which is what actually realises the window on
	// Windows; sending WM_SETICON to a handle that does not exist yet is a
	// silent no-op.
	applyWindowIcon(uintptr(w.Window()))

	// The page is injected rather than served over a local port. A desktop app
	// that opens a listening socket to draw its own window is a needless
	// increase in attack surface for something holding OAuth credentials.
	w.SetHtml(strings.Replace(uiHTML, "{{VERSION}}", version, 1))
	w.Run()
}

// bind exposes the Go API to the page. Every binding returns a value that
// marshals to JSON, and errors surface as rejected promises the page reports.
func bind(w webview.WebView, a *api) {
	must := func(name string, fn any) {
		if err := w.Bind(name, fn); err != nil {
			fmt.Fprintf(os.Stderr, "failed to bind %s: %v\n", name, err)
			os.Exit(1)
		}
	}

	must("goList", a.List)
	must("goSwitch", a.Switch)
	must("goCapture", a.Capture)
	must("goRename", a.Rename)
	must("goRemove", a.Remove)
	must("goDoctor", a.Doctor)
	must("goSettingsGet", a.SettingsGet)
	must("goSettingsSet", a.SettingsSet)
	must("goAutostartSet", a.AutostartSet)
}
