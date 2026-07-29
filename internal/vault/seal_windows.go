//go:build windows

package vault

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	crypt32                = syscall.NewLazyDLL("crypt32.dll")
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procCryptProtectData   = crypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	procLocalFree          = kernel32.NewProc("LocalFree")
)

// cryptprotectUIForbidden fails rather than showing a prompt, which matters
// for the tray app and for any non-interactive use.
const cryptprotectUIForbidden = 0x1

type dataBlob struct {
	cbData uint32
	pbData *byte
}

func newBlob(b []byte) dataBlob {
	if len(b) == 0 {
		return dataBlob{}
	}
	return dataBlob{cbData: uint32(len(b)), pbData: &b[0]}
}

func (b dataBlob) bytes() []byte {
	if b.pbData == nil || b.cbData == 0 {
		return nil
	}
	out := make([]byte, b.cbData)
	copy(out, unsafe.Slice(b.pbData, b.cbData))
	return out
}

func (b dataBlob) free() {
	if b.pbData != nil {
		procLocalFree.Call(uintptr(unsafe.Pointer(b.pbData)))
	}
}

// NewSealer returns the DPAPI sealer on Windows.
//
// Claude Code writes its own credentials in plaintext here, so binding the
// vault to the current user account via DPAPI is strictly stronger than what
// it protects. It also means the vault cannot be read by another user on the
// same machine, and cannot be copied to a different machine.
//
// Unlike macOS, there is nothing to escape from: DPAPI needs no unlocked store
// and works in every session, including services and scheduled tasks. A "file"
// setting here would therefore be a pure downgrade bought for nothing, so it is
// refused rather than honoured.
func NewSealer() (Sealer, error) {
	switch pref := vaultBackendPref(); pref {
	case "", "dpapi":
		return dpapiSealer{}, nil
	default:
		return nil, fmt.Errorf("%w %q: DPAPI is available in every Windows session, "+
			"so ccm offers no weaker vault here (valid: auto, dpapi)", ErrBadVaultBackend, pref)
	}
}

type dpapiSealer struct{}

func (dpapiSealer) Name() string { return "windows-dpapi" }
func (dpapiSealer) Describe() string {
	return "Windows DPAPI, bound to the current user account"
}

func (dpapiSealer) Seal(plain []byte) ([]byte, error) {
	in := newBlob(plain)
	var out dataBlob
	desc, err := syscall.UTF16PtrFromString("ccm vault")
	if err != nil {
		return nil, err
	}
	r, _, errno := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(&in)),
		uintptr(unsafe.Pointer(desc)),
		0, 0, 0,
		uintptr(cryptprotectUIForbidden),
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, fmt.Errorf("CryptProtectData: %w", errno)
	}
	defer out.free()
	return out.bytes(), nil
}

func (dpapiSealer) Unseal(sealed []byte) ([]byte, error) {
	in := newBlob(sealed)
	var out dataBlob
	r, _, errno := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0, 0, 0, 0,
		uintptr(cryptprotectUIForbidden),
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, fmt.Errorf("CryptUnprotectData (vault belongs to a different user or machine): %w", errno)
	}
	defer out.free()
	return out.bytes(), nil
}
