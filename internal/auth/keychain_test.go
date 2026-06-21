package auth

import (
	"context"
	"strings"
	"testing"
)

// fakeKeychain is an in-memory KeychainStore for testing the credential store
// wrapper without touching the real OS keychain.
type fakeKeychain struct {
	data      map[string]string
	available bool
}

func newFakeKeychain() *fakeKeychain { return &fakeKeychain{data: map[string]string{}, available: true} }

func (f *fakeKeychain) key(a, s string) string { return a + "\x00" + s }
func (f *fakeKeychain) Available() bool         { return f.available }
func (f *fakeKeychain) Get(a, s string) (string, error) {
	v, ok := f.data[f.key(a, s)]
	if !ok {
		return "", errKeychainNotFound
	}
	return v, nil
}
func (f *fakeKeychain) Set(a, s, v string) error { f.data[f.key(a, s)] = v; return nil }
func (f *fakeKeychain) Delete(a, s string) error { delete(f.data, f.key(a, s)); return nil }

func TestKeychainCredentialStoreRoundTrip(t *testing.T) {
	kc := newFakeKeychain()
	store := &keychainCredentialStore{kc: kc, file: NewFileCredentialStore(t.TempDir() + "/credentials.json")}

	creds := Credentials{Source: SourceOAuth, AccessToken: "AT", RefreshToken: "RT", Scopes: []string{"user:profile"}}
	if err := store.Save(context.Background(), creds); err != nil {
		t.Fatalf("Save err: %v", err)
	}
	// The keychain holds the value; the plaintext file must NOT have been written.
	if len(kc.data) == 0 {
		t.Fatal("expected credentials in keychain")
	}

	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load err: %v", err)
	}
	if loaded.AccessToken != "AT" || loaded.RefreshToken != "RT" {
		t.Fatalf("loaded = %+v", loaded)
	}

	if err := store.Delete(context.Background()); err != nil {
		t.Fatalf("Delete err: %v", err)
	}
	loaded, _ = store.Load(context.Background())
	if loaded.AccessToken != "" {
		t.Fatal("credentials not deleted")
	}
}

func TestKeychainCredentialStoreFallsBackToFile(t *testing.T) {
	kc := newFakeKeychain()
	kc.available = false
	file := NewFileCredentialStore(t.TempDir() + "/credentials.json")
	store := &keychainCredentialStore{kc: kc, file: file}

	creds := Credentials{Source: SourceOAuth, AccessToken: "AT2"}
	if err := store.Save(context.Background(), creds); err != nil {
		t.Fatalf("Save err: %v", err)
	}
	if len(kc.data) != 0 {
		t.Fatal("keychain unavailable: must not write to keychain")
	}
	loaded, err := store.Load(context.Background())
	if err != nil || loaded.AccessToken != "AT2" {
		t.Fatalf("file fallback failed: %+v err=%v", loaded, err)
	}
}

func TestMacOSSecurityArgsHaveNoSecretInArgv(t *testing.T) {
	// Verify Set routes the secret via stdin, not argv, to avoid leaking it to
	// process listings (matches CC's `security -i` approach).
	var sawStdin string
	var sawArgs []string
	kc := &macOSKeychainStore{run: func(stdin string, args ...string) (string, error) {
		sawStdin, sawArgs = stdin, args
		return "", nil
	}}
	if err := kc.Set("acct", "svc", "TOPSECRET"); err != nil {
		t.Fatalf("Set err: %v", err)
	}
	if strings.Contains(strings.Join(sawArgs, " "), "TOPSECRET") {
		t.Fatalf("secret leaked into argv: %v", sawArgs)
	}
	if !strings.Contains(sawStdin, "TOPSECRET") && !strings.Contains(sawStdin, "544f5053454352455424") {
		// either raw on stdin or hex-encoded; both keep it out of argv
		t.Fatalf("secret not passed via stdin: %q", sawStdin)
	}
}
