//go:build !windows

package main

// applyWindowIcon is a no-op away from Windows.
//
// macOS takes the icon from the .app bundle's Info.plist and Linux from the
// .desktop entry, so neither needs the window to carry one.
func applyWindowIcon(uintptr) {}
