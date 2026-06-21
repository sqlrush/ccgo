package auth

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"regexp"
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
	// hex.EncodeToString([]byte("TOPSECRET")) == "544f50534543524554"
	if !strings.Contains(sawStdin, "TOPSECRET") && !strings.Contains(sawStdin, "544f50534543524554") {
		// either raw on stdin or hex-encoded; both keep it out of argv
		t.Fatalf("secret not passed via stdin: %q", sawStdin)
	}
}

// hexOnly matches a lowercase hex string (no 0x prefix).
var hexOnly = regexp.MustCompile(`^[0-9a-f]+$`)

// TestMacOSSecuritySetUsesHexEncoding verifies that Set:
//  1. Encodes the secret as lowercase hex (no quoting, no raw JSON in the command).
//  2. Uses -X (not -w) in the stdin command, matching CC's macOsKeychainStorage.ts.
//  3. Does not place the raw secret (or any %q-style \"…\" encoding of it) in argv.
func TestMacOSSecuritySetUsesHexEncoding(t *testing.T) {
	// Use a value containing JSON special chars (`"` and `\`) to prove the
	// quoting-corruption class of bugs is eliminated.
	secretValue := `a"b\c{"key":"val"}`

	var sawStdin string
	var sawArgs []string
	kc := &macOSKeychainStore{run: func(stdin string, args ...string) (string, error) {
		sawStdin, sawArgs = stdin, args
		return "", nil
	}}
	if err := kc.Set("myacct", "mysvc", secretValue); err != nil {
		t.Fatalf("Set err: %v", err)
	}

	// 1. Secret must not appear raw in the stdin command.
	if strings.Contains(sawStdin, secretValue) {
		t.Errorf("raw secret found in stdin command: %q", sawStdin)
	}

	// 2. The stdin command must use -X (hex flag), not -w (password flag).
	if !strings.Contains(sawStdin, " -X ") {
		t.Errorf("stdin does not contain -X flag (hex encoding): %q", sawStdin)
	}
	if strings.Contains(sawStdin, " -w ") {
		t.Errorf("stdin contains -w flag (raw password); should use -X instead: %q", sawStdin)
	}

	// 3. Extract the hex value from the stdin command and verify it is valid hex
	//    that decodes back to the original secret.
	// Expected stdin format: add-generic-password -U -a "myacct" -s "mysvc" -X <hex>\n
	const marker = "-X "
	idx := strings.Index(sawStdin, marker)
	if idx < 0 {
		t.Fatalf("cannot find -X in stdin: %q", sawStdin)
	}
	hexPart := strings.TrimRight(sawStdin[idx+len(marker):], "\n")
	if !hexOnly.MatchString(hexPart) {
		t.Errorf("value after -X is not pure hex [0-9a-f]+: %q", hexPart)
	}
	decoded, err := hex.DecodeString(hexPart)
	if err != nil {
		t.Fatalf("hex.DecodeString failed: %v", err)
	}
	if string(decoded) != secretValue {
		t.Errorf("hex round-trip mismatch: got %q, want %q", string(decoded), secretValue)
	}

	// 4. Secret must not appear in argv.
	joinedArgs := strings.Join(sawArgs, " ")
	if strings.Contains(joinedArgs, secretValue) {
		t.Errorf("raw secret leaked into argv: %v", sawArgs)
	}

	// 5. No %q-style Go quoting artifacts in the secret portion (no \" sequences
	//    that would indicate the old -w %q approach).
	if strings.Contains(sawStdin, `\"`) {
		t.Errorf("stdin contains %q-style escaped quotes; should be hex only: %q", `\"`, sawStdin)
	}
}

// TestMacOSKeychainHexRoundTripViaFakeRun tests the full Save→Load round-trip
// using a fake run that simulates what a real macOS security(1) tool would do:
// Set stores hex, Get returns the decoded JSON (security(1) decodes -X hex
// transparently on -w read, matching CC's macOsKeychainStorage.ts read path).
func TestMacOSKeychainHexRoundTripViaFakeRun(t *testing.T) {
	// store simulates the keychain: maps (account,service) → raw value (as if
	// security(1) stored and returned the decoded bytes).
	stored := map[string]string{}

	fakeRun := func(stdin string, args ...string) (string, error) {
		if len(args) == 1 && args[0] == "-i" {
			// Parse the SET command from stdin.
			// Format: add-generic-password -U -a "acct" -s "svc" -X <hex>
			line := strings.TrimRight(stdin, "\n")
			const marker = "-X "
			idx := strings.Index(line, marker)
			if idx < 0 {
				t.Errorf("fake: stdin missing -X: %q", stdin)
				return "", nil
			}
			hexPart := line[idx+len(marker):]
			decoded, err := hex.DecodeString(hexPart)
			if err != nil {
				t.Errorf("fake: hex.DecodeString(%q): %v", hexPart, err)
				return "", nil
			}
			// Derive a storage key from the stdin (simplified: use hex of "acct+svc").
			stored["default"] = string(decoded)
			return "", nil
		}
		// GET command: security find-generic-password -a acct -s svc -w
		// Returns the decoded value (as security(1) would).
		v, ok := stored["default"]
		if !ok {
			return "", errKeychainNotFound
		}
		return v + "\n", nil
	}

	kc := &macOSKeychainStore{run: fakeRun}

	// Use a credential with a token that contains JSON special chars to prove
	// that the quoting-corruption class of bugs is eliminated.
	originalCreds := Credentials{
		Source:       SourceOAuth,
		AccessToken:  `tok"en\with"quotes`,
		RefreshToken: `ref\resh`,
		Scopes:       []string{`scope"1`, `scope\2`},
	}
	jsonBytes, err := json.Marshal(originalCreds)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	rawJSON := string(jsonBytes)

	// Set via the macOSKeychainStore (hex-encodes into the fake run).
	if err := kc.Set("acct", "svc", rawJSON); err != nil {
		t.Fatalf("Set err: %v", err)
	}

	// Get back via the macOSKeychainStore (fake run returns decoded bytes).
	got, err := kc.Get("acct", "svc")
	if err != nil {
		t.Fatalf("Get err: %v", err)
	}

	// Unmarshal and verify round-trip fidelity.
	var roundTripped Credentials
	if err := json.Unmarshal([]byte(got), &roundTripped); err != nil {
		t.Fatalf("json.Unmarshal(%q): %v", got, err)
	}
	if roundTripped.AccessToken != originalCreds.AccessToken {
		t.Errorf("AccessToken mismatch: got %q, want %q", roundTripped.AccessToken, originalCreds.AccessToken)
	}
	if roundTripped.RefreshToken != originalCreds.RefreshToken {
		t.Errorf("RefreshToken mismatch: got %q, want %q", roundTripped.RefreshToken, originalCreds.RefreshToken)
	}
}
