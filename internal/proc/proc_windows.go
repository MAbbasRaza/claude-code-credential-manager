//go:build windows

package proc

import (
	"fmt"
	"syscall"
)

// findClaude walks the process list with the Toolhelp32 snapshot API. This
// avoids shelling out to tasklist, which is slow and locale-dependent.
func findClaude() ([]Process, error) {
	snap, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("process snapshot: %w", err)
	}
	defer syscall.CloseHandle(snap)

	var entry syscall.ProcessEntry32
	entry.Size = uint32(unsafe_Sizeof_ProcessEntry32())

	if err := syscall.Process32First(snap, &entry); err != nil {
		return nil, fmt.Errorf("first process: %w", err)
	}

	var out []Process
	for {
		name := syscall.UTF16ToString(entry.ExeFile[:])
		if isClaude(name) {
			out = append(out, Process{PID: int(entry.ProcessID), Name: name})
		}
		if err := syscall.Process32Next(snap, &entry); err != nil {
			if err == syscall.ERROR_NO_MORE_FILES {
				break
			}
			return nil, fmt.Errorf("next process: %w", err)
		}
	}
	return out, nil
}
