module github.com/MAbbasRaza/claude-code-multi-account-manager

// 1.25 is the floor because of GO-2026-4971, a net.Dial panic reachable from
// the tray app through systray.Run. It is fixed in the standard library at
// go1.25.10, and govulncheck gates the build on it.
go 1.25.0

require (
	fyne.io/systray v1.12.2
	github.com/tidwall/gjson v1.18.0
	github.com/tidwall/sjson v1.2.5
	github.com/webview/webview_go v0.0.0-20240831120633-6173450d4dd6
	golang.org/x/sys v0.47.0
)

require (
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
)
