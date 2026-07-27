//go:build windows

package proc

import (
	"syscall"
	"unsafe"
)

// unsafe_Sizeof_ProcessEntry32 isolates the unsafe import so proc_windows.go
// stays readable. Process32First rejects the struct unless Size is set to the
// exact size of the struct the caller passes.
func unsafe_Sizeof_ProcessEntry32() uintptr {
	var e syscall.ProcessEntry32
	return unsafe.Sizeof(e)
}
