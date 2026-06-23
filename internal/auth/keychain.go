package auth

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

var errKeychainNotFound = errors.New("auth: keychain item not found")

// Service/account names mirror CC (utils/secureStorage/macOsKeychainHelpers.ts).
const keychainServiceName = "Claude Code-credentials"

func keychainAccount() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return "claude-code-user"
}

// KeychainStore is a minimal secret store. macOSKeychainStore is the only real
// backend today; other platforms fall back to a file.
type KeychainStore interface {
	Available() bool
	Get(account, service string) (string, error)
	Set(account, service, value string) error
	Delete(account, service string) error
}

// macOSKeychainStore drives /usr/bin/security. run is a seam for tests.
type macOSKeychainStore struct {
	run func(stdin string, args ...string) (string, error)
}

func newMacOSKeychainStore() *macOSKeychainStore {
	return &macOSKeychainStore{run: runSecurity}
}

// runSecurity executes /usr/bin/security, optionally feeding stdin.
func runSecurity(stdin string, args ...string) (string, error) {
	cmd := exec.Command("/usr/bin/security", args...) //nolint:gosec
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.Output()
	return string(out), err
}

func (m *macOSKeychainStore) Available() bool { return runtime.GOOS == "darwin" }

// Get retrieves a keychain item, returning errKeychainNotFound if absent.
func (m *macOSKeychainStore) Get(account, service string) (string, error) {
	out, err := m.run("", "find-generic-password", "-a", account, "-s", service, "-w")
	if err != nil {
		return "", errKeychainNotFound
	}
	return strings.TrimRight(out, "\n"), nil
}

// Set stores value via stdin (using security -i) so the secret never appears
// in argv / process listings. The secret is hex-encoded with -X to eliminate
// all quoting concerns: hex is [0-9a-f] only, with no special characters that
// could be mis-parsed by security's POSIX-shell-like stdin tokenizer. This
// matches CC's approach in macOsKeychainStorage.ts (Buffer.from(...).toString('hex')
// with -X flag). Account and service are double-quoted strings (simple ASCII).
func (m *macOSKeychainStore) Set(account, service, value string) error {
	// Hex-encode the secret value so it is safe for any tokenizer and contains
	// no embedded quotes, backslashes, or JSON special characters.
	hexVal := hex.EncodeToString([]byte(value))
	// security -i reads commands from stdin; -X takes hex-encoded data.
	// The process listing shows only "security -i" — no payload visible.
	stdin := fmt.Sprintf("add-generic-password -U -a %q -s %q -X %s\n", account, service, hexVal)
	_, err := m.run(stdin, "-i")
	return err
}

// Delete removes a keychain item. ANY error (including "not found" and exec
// failures) is treated as a no-op success, matching CC's delete() which
// catches all exceptions and returns false without propagating them. Callers
// should not rely on Delete to signal exec failure.
func (m *macOSKeychainStore) Delete(account, service string) error {
	_, err := m.run("", "delete-generic-password", "-a", account, "-s", service)
	if err != nil {
		// Treat any error as a no-op (mirrors CC's catch-all in delete()).
		return nil
	}
	return nil
}

// keychainCredentialStore implements CredentialStore atop a KeychainStore,
// falling back to a file store when the keychain is unavailable.
type keychainCredentialStore struct {
	kc   KeychainStore
	file *FileCredentialStore
}

// NewKeychainCredentialStore returns the preferred CredentialStore for this
// platform: keychain-backed on macOS, plain file elsewhere.
func NewKeychainCredentialStore(path string) CredentialStore {
	file := NewFileCredentialStore(path)
	if runtime.GOOS != "darwin" {
		return file
	}
	return &keychainCredentialStore{kc: newMacOSKeychainStore(), file: file}
}

// NewDefaultCredentialStore returns the OS-preferred credential store with
// the default credentials path. This is a convenience alias for
// NewKeychainCredentialStore("") that callers outside the auth package can
// use without having to know about the empty-path convention.
func NewDefaultCredentialStore() CredentialStore {
	return NewKeychainCredentialStore("")
}

func (s *keychainCredentialStore) usingKeychain() bool {
	return s.kc != nil && s.kc.Available()
}

// Load retrieves credentials from the keychain (macOS) or the file fallback.
func (s *keychainCredentialStore) Load(ctx context.Context) (Credentials, error) {
	if err := ctx.Err(); err != nil {
		return Credentials{}, err
	}
	if !s.usingKeychain() {
		return s.file.Load(ctx)
	}
	raw, err := s.kc.Get(keychainAccount(), keychainServiceName)
	if err != nil {
		if errors.Is(err, errKeychainNotFound) {
			return Credentials{Source: SourceNone}, nil
		}
		return Credentials{}, fmt.Errorf("auth: load keychain credentials: %w", err)
	}
	if strings.TrimSpace(raw) == "" {
		return Credentials{Source: SourceNone}, nil
	}
	var creds Credentials
	if err := json.Unmarshal([]byte(raw), &creds); err != nil {
		return Credentials{}, fmt.Errorf("auth: decode keychain credentials: %w", err)
	}
	if creds.Source == "" {
		creds.Source = SourceNone
	}
	return creds, nil
}

// Save persists credentials to the keychain (macOS) or the file fallback.
// The JSON shape is identical to FileCredentialStore, making them interchangeable.
func (s *keychainCredentialStore) Save(ctx context.Context, creds Credentials) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if creds.Source == "" {
		creds.Source = SourceNone
	}
	if err := creds.Validate(); err != nil {
		return err
	}
	if !s.usingKeychain() {
		return s.file.Save(ctx, creds)
	}
	data, err := json.Marshal(creds)
	if err != nil {
		return fmt.Errorf("auth: encode keychain credentials: %w", err)
	}
	return s.kc.Set(keychainAccount(), keychainServiceName, string(data))
}

// Delete removes credentials from the keychain (macOS) or the file fallback.
func (s *keychainCredentialStore) Delete(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !s.usingKeychain() {
		return s.file.Delete(ctx)
	}
	return s.kc.Delete(keychainAccount(), keychainServiceName)
}
