//go:build !darwin

package store

import "fmt"

// newKeychainStore only exists so the platform-independent selector in
// store.go compiles everywhere. Reaching it off macOS means path resolution
// produced a backend the platform cannot serve.
func newKeychainStore() (Store, error) {
	return nil, fmt.Errorf("the macOS Keychain backend is not available on this platform")
}
