package auth

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestFileCredentialStoreSavesLoadsAndDeletesCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "credentials.json")
	store := NewFileCredentialStore(path)
	expiresAt := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	credentials := Credentials{
		Source:       SourceOAuth,
		AccessToken:  "access",
		RefreshToken: "refresh",
		Scopes:       []string{"user:profile", "user:mcp_servers"},
		ExpiresAt:    expiresAt,
	}
	if err := store.Save(context.Background(), credentials); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("credential file mode = %o", info.Mode().Perm())
		}
		dirInfo, err := os.Stat(filepath.Dir(path))
		if err != nil {
			t.Fatal(err)
		}
		if dirInfo.Mode().Perm() != 0o700 {
			t.Fatalf("credential dir mode = %o", dirInfo.Mode().Perm())
		}
	}

	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Source != SourceOAuth || loaded.AccessToken != "access" || loaded.RefreshToken != "refresh" || !loaded.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("loaded = %#v", loaded)
	}
	if len(loaded.Scopes) != 2 || loaded.Scopes[1] != "user:mcp_servers" {
		t.Fatalf("scopes = %#v", loaded.Scopes)
	}

	if err := store.Delete(context.Background()); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Source != SourceNone {
		t.Fatalf("loaded after delete = %#v", loaded)
	}
}

func TestFileCredentialStoreRejectsInvalidCredentials(t *testing.T) {
	store := NewFileCredentialStore(filepath.Join(t.TempDir(), "credentials.json"))
	err := store.Save(context.Background(), Credentials{Source: SourceOAuth})
	if err == nil {
		t.Fatal("expected invalid credentials error")
	}
}

func TestFileCredentialStoreHonorsContext(t *testing.T) {
	store := NewFileCredentialStore(filepath.Join(t.TempDir(), "credentials.json"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Save(ctx, Credentials{Source: SourceNone}); err == nil {
		t.Fatal("expected context error")
	}
	if _, err := store.Load(ctx); err == nil {
		t.Fatal("expected context error")
	}
	if err := store.Delete(ctx); err == nil {
		t.Fatal("expected context error")
	}
}
