//go:build windows

package main

import "syscall"

// Windows draws the taskbar button, the Alt-Tab entry and the title bar from
// the *window's* icon, which is a different thing from the icon compiled into
// the executable. webview creates its own window class and leaves hIcon unset,
// so without this the window has no icon at all and the shell falls back to the
// generic default, even though Explorer shows the real icon for the same file.
//
// Verified before the fix: WM_GETICON returned 0 for ICON_SMALL, ICON_BIG and
// ICON_SMALL2, and both GCLP_HICON and GCLP_HICONSM were 0.
func applyWindowIcon(hwnd uintptr) {
	if hwnd == 0 {
		return
	}

	var (
		user32          = syscall.NewLazyDLL("user32.dll")
		kernel32        = syscall.NewLazyDLL("kernel32.dll")
		procLoadImage   = user32.NewProc("LoadImageW")
		procSendMessage = user32.NewProc("SendMessageW")
		procMetrics     = user32.NewProc("GetSystemMetrics")
		procGetModule   = kernel32.NewProc("GetModuleHandleW")
	)

	const (
		imageIcon  = 1
		lrShared   = 0x8000
		wmSetIcon  = 0x0080
		iconSmall  = 0
		iconBig    = 1
		smCXIcon   = 11
		smCYIcon   = 12
		smCXSmIcon = 49
		smCYSmIcon = 50

		// Resource id 1, matching the ICON statement in app.rc.
		iconResourceID = 1
	)

	hInst, _, _ := procGetModule.Call(0)

	metric := func(index uintptr) uintptr {
		v, _, _ := procMetrics.Call(index)
		return v
	}

	// Loaded at both sizes rather than letting the shell scale one. The title
	// bar and taskbar ask for different dimensions, and a scaled 32px icon in a
	// 16px slot is visibly soft.
	//
	// MAKEINTRESOURCE is just the integer id in the pointer slot, and Call
	// takes uintptr arguments, so it is passed directly. Laundering it through
	// unsafe.Pointer would be an invalid conversion, which go vet rightly
	// rejects.
	load := func(cx, cy uintptr) uintptr {
		h, _, _ := procLoadImage.Call(hInst, iconResourceID, imageIcon, cx, cy, lrShared)
		return h
	}

	if big := load(metric(smCXIcon), metric(smCYIcon)); big != 0 {
		procSendMessage.Call(hwnd, wmSetIcon, iconBig, big)
	}
	if small := load(metric(smCXSmIcon), metric(smCYSmIcon)); small != 0 {
		procSendMessage.Call(hwnd, wmSetIcon, iconSmall, small)
	}
}
