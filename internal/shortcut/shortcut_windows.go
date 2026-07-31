//go:build windows

package shortcut

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

// A .lnk is a documented but fiddly binary format, and a subtly malformed one
// produces a shortcut that exists and does not work. Explorer builds them
// through IShellLink, so this does too.
//
// Driven through syscall rather than cgo, the same way internal/vault reaches
// DPAPI. That keeps the CLI's CGO_ENABLED=0 cross-compile intact, which is what
// lets one Linux runner build every Windows binary in the release.
var (
	ole32              = syscall.NewLazyDLL("ole32.dll")
	procCoInitializeEx = ole32.NewProc("CoInitializeEx")
	procCoUninitialize = ole32.NewProc("CoUninitialize")
	procCoCreateInst   = ole32.NewProc("CoCreateInstance")
)

const (
	coinitApartmentThreaded = 0x2
	coinitDisableOLE1DDE    = 0x4
	clsctxInprocServer      = 0x1

	// COM was already initialised on this thread in a different mode. Not a
	// failure for our purposes, and the balancing CoUninitialize is still due.
	rpcEChangedMode = 0x80010106
)

var (
	clsidShellLink = guid{0x00021401, 0x0000, 0x0000, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidShellLinkW  = guid{0x000214F9, 0x0000, 0x0000, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidPersistFile = guid{0x0000010B, 0x0000, 0x0000, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
)

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

// The full vtable in declaration order. Every slot is listed, including the
// ones never called: the offsets of the ones that are depend on it.
type iShellLinkWVtbl struct {
	QueryInterface      uintptr
	AddRef              uintptr
	Release             uintptr
	GetPath             uintptr
	GetIDList           uintptr
	SetIDList           uintptr
	GetDescription      uintptr
	SetDescription      uintptr
	GetWorkingDirectory uintptr
	SetWorkingDirectory uintptr
	GetArguments        uintptr
	SetArguments        uintptr
	GetHotkey           uintptr
	SetHotkey           uintptr
	GetShowCmd          uintptr
	SetShowCmd          uintptr
	GetIconLocation     uintptr
	SetIconLocation     uintptr
	SetRelativePath     uintptr
	Resolve             uintptr
	SetPath             uintptr
}

type iShellLinkW struct{ vtbl *iShellLinkWVtbl }

type iPersistFileVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
	GetClassID     uintptr
	IsDirty        uintptr
	Load           uintptr
	Save           uintptr
	SaveCompleted  uintptr
	GetCurFile     uintptr
}

type iPersistFile struct{ vtbl *iPersistFileVtbl }

func hresult(r uintptr, op string) error {
	if int32(r) < 0 {
		return fmt.Errorf("%s: HRESULT 0x%08X", op, uint32(r))
	}
	return nil
}

func supported(Kind) bool { return true }

func describe(k Kind) string {
	if k == Desktop {
		return "a .lnk on your desktop"
	}
	return "a .lnk in the Start Menu"
}

// kindDir returns the directory a kind lives in.
//
// Read from the environment rather than SHGetKnownFolderPath. USERPROFILE and
// APPDATA are set in every interactive session, and a user whose Desktop has
// been redirected by OneDrive still has a correct USERPROFILE, so the simpler
// route is not meaningfully less accurate here.
func kindDir(k Kind) (string, error) {
	if k == Desktop {
		home := os.Getenv("USERPROFILE")
		if home == "" {
			var err error
			home, err = os.UserHomeDir()
			if err != nil {
				return "", err
			}
		}
		return filepath.Join(home, "Desktop"), nil
	}

	appdata := os.Getenv("APPDATA")
	if appdata == "" {
		return "", errors.New("APPDATA is not set, so the Start Menu location is unknown")
	}
	// A folder of our own, so an uninstall can take the whole group without
	// touching anything else the user keeps in Programs.
	return filepath.Join(appdata, "Microsoft", "Windows", "Start Menu", "Programs",
		"Claude Code Multi-Account Manager"), nil
}

func location(k Kind, name string) string {
	dir, err := kindDir(k)
	if err != nil {
		return ""
	}
	return filepath.Join(dir, name+".lnk")
}

func exists(k Kind, name string) (bool, error) {
	p := location(k, name)
	if p == "" {
		return false, nil
	}
	info, err := os.Stat(p)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !info.IsDir(), nil
}

func add(k Kind, e Entry) error {
	dir, err := kindDir(k)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	// Named from Name, never DisplayName, so Exists and Remove find exactly
	// what Add wrote.
	path := location(k, e.Name)

	// COM is thread-affine: the object must be created, used and released on
	// the thread that initialised it, so the goroutine is pinned throughout.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hr, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded|coinitDisableOLE1DDE)
	if int32(hr) < 0 && uint32(hr) != rpcEChangedMode {
		return fmt.Errorf("CoInitializeEx: HRESULT 0x%08X", uint32(hr))
	}
	defer procCoUninitialize.Call()

	var link *iShellLinkW
	hr, _, _ = procCoCreateInst.Call(
		uintptr(unsafe.Pointer(&clsidShellLink)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidShellLinkW)), uintptr(unsafe.Pointer(&link)))
	if err := hresult(hr, "CoCreateInstance(ShellLink)"); err != nil {
		return err
	}
	defer syscall.SyscallN(link.vtbl.Release, uintptr(unsafe.Pointer(link)))

	if err := setStr(link.vtbl.SetPath, unsafe.Pointer(link), e.Target, "SetPath"); err != nil {
		return err
	}
	// So the desktop app starts in its own directory rather than wherever
	// Explorer happened to be.
	if err := setStr(link.vtbl.SetWorkingDirectory, unsafe.Pointer(link),
		filepath.Dir(e.Target), "SetWorkingDirectory"); err != nil {
		return err
	}
	if e.Description != "" {
		if err := setStr(link.vtbl.SetDescription, unsafe.Pointer(link),
			e.Description, "SetDescription"); err != nil {
			return err
		}
	}
	if len(e.Args) > 0 {
		if err := setStr(link.vtbl.SetArguments, unsafe.Pointer(link),
			joinArgs(e.Args), "SetArguments"); err != nil {
			return err
		}
	}
	if e.Icon != "" {
		p, err := syscall.UTF16PtrFromString(e.Icon)
		if err != nil {
			return err
		}
		hr, _, _ := syscall.SyscallN(link.vtbl.SetIconLocation,
			uintptr(unsafe.Pointer(link)), uintptr(unsafe.Pointer(p)), 0)
		if err := hresult(hr, "SetIconLocation"); err != nil {
			return err
		}
	}

	var pf *iPersistFile
	hr, _, _ = syscall.SyscallN(link.vtbl.QueryInterface,
		uintptr(unsafe.Pointer(link)),
		uintptr(unsafe.Pointer(&iidPersistFile)),
		uintptr(unsafe.Pointer(&pf)))
	if err := hresult(hr, "QueryInterface(IPersistFile)"); err != nil {
		return err
	}
	defer syscall.SyscallN(pf.vtbl.Release, uintptr(unsafe.Pointer(pf)))

	wpath, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	hr, _, _ = syscall.SyscallN(pf.vtbl.Save,
		uintptr(unsafe.Pointer(pf)), uintptr(unsafe.Pointer(wpath)), 1)
	if err := hresult(hr, "IPersistFile::Save"); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

func setStr(method uintptr, this unsafe.Pointer, value, op string) error {
	p, err := syscall.UTF16PtrFromString(value)
	if err != nil {
		return err
	}
	hr, _, _ := syscall.SyscallN(method, uintptr(this), uintptr(unsafe.Pointer(p)))
	return hresult(hr, op)
}

// joinArgs quotes any argument containing whitespace. That is all the quoting a
// shortcut needs here: ccm takes profile names, and vault.ValidateName already
// refuses whitespace in those.
func joinArgs(args []string) string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if strings.ContainsAny(a, " \t") {
			a = `"` + a + `"`
		}
		out = append(out, a)
	}
	return strings.Join(out, " ")
}

func remove(k Kind, name string) error {
	p := location(k, name)
	if p == "" {
		return nil
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", p, err)
	}

	// Take the Start Menu folder with it once empty, so an uninstall leaves no
	// stray group. Harmlessly fails while other shortcuts remain.
	if k == Menu {
		if dir, err := kindDir(k); err == nil {
			_ = os.Remove(dir)
		}
	}
	return nil
}
