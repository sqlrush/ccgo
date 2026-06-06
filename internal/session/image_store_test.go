package session

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"ccgo/internal/contracts"
)

func TestStoreImageWritesBase64AndCachesPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	ClearStoredImagePaths()
	defer ClearStoredImagePaths()

	sessionID := contracts.ID("session-1")
	content := PastedContent{
		ID:        7,
		Type:      PastedContentImage,
		Content:   base64.StdEncoding.EncodeToString([]byte("image bytes")),
		MediaType: "image/png",
		Filename:  "chart.png",
	}
	cachedPath, ok := CacheImagePath(sessionID, content)
	if !ok || cachedPath != filepath.Join(dir, "image-cache", "session-1", "7.png") {
		t.Fatalf("cached path = %q ok=%v", cachedPath, ok)
	}
	if got, ok := GetStoredImagePath(7); !ok || got != cachedPath {
		t.Fatalf("stored path after cache = %q ok=%v", got, ok)
	}

	storedPath, ok := StoreImage(sessionID, content)
	if !ok || storedPath != cachedPath {
		t.Fatalf("stored path = %q ok=%v", storedPath, ok)
	}
	data, err := os.ReadFile(storedPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "image bytes" {
		t.Fatalf("stored data = %q", data)
	}
	info, err := os.Stat(storedPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}

func TestStoreImagesOnlyStoresImages(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	ClearStoredImagePaths()
	defer ClearStoredImagePaths()

	paths := StoreImages("session-1", map[int]PastedContent{
		1: {ID: 1, Type: PastedContentText, Content: "hello"},
		2: {ID: 2, Type: PastedContentImage, Content: base64.StdEncoding.EncodeToString([]byte("webp")), MediaType: "image/webp"},
		3: {ID: 3, Type: PastedContentImage, Content: "not base64", MediaType: "image/jpeg"},
	})
	if len(paths) != 1 {
		t.Fatalf("paths = %#v", paths)
	}
	want := filepath.Join(dir, "image-cache", "session-1", "2.webp")
	if paths[2] != want {
		t.Fatalf("image path = %q, want %q", paths[2], want)
	}
	if _, ok := paths[1]; ok {
		t.Fatal("text paste was stored as image")
	}
	if _, ok := paths[3]; ok {
		t.Fatal("invalid base64 image was stored")
	}
}

func TestImagePathCacheEvictsOldestAtCap(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	ClearStoredImagePaths()
	defer ClearStoredImagePaths()

	for id := 1; id <= MaxStoredImagePaths+1; id++ {
		if _, ok := CacheImagePath("session-1", PastedContent{ID: id, Type: PastedContentImage, MediaType: "image/png"}); !ok {
			t.Fatalf("cache image %d failed", id)
		}
	}
	if _, ok := GetStoredImagePath(1); ok {
		t.Fatal("oldest image path was not evicted")
	}
	want := filepath.Join(dir, "image-cache", "session-1", strconv.Itoa(MaxStoredImagePaths+1)+".png")
	if got, ok := GetStoredImagePath(MaxStoredImagePaths + 1); !ok || got != want {
		t.Fatalf("newest path = %q ok=%v, want %q", got, ok, want)
	}
}

func TestCleanupOldImageCachesRemovesOtherSessions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)

	for _, sessionID := range []string{"current", "old-a", "old-b"} {
		path := filepath.Join(dir, "image-cache", sessionID, "1.png")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(sessionID), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if removed := CleanupOldImageCaches("current"); removed != 2 {
		t.Fatalf("removed = %d", removed)
	}
	for _, sessionID := range []string{"old-a", "old-b"} {
		if _, err := os.Stat(filepath.Join(dir, "image-cache", sessionID)); !os.IsNotExist(err) {
			t.Fatalf("old cache %s remains: %v", sessionID, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "image-cache", "current", "1.png")); err != nil {
		t.Fatalf("current cache removed: %v", err)
	}

	if err := os.RemoveAll(filepath.Join(dir, "image-cache", "current")); err != nil {
		t.Fatal(err)
	}
	if removed := CleanupOldImageCaches("current"); removed != 0 {
		t.Fatalf("removed empty base = %d", removed)
	}
	if _, err := os.Stat(filepath.Join(dir, "image-cache")); !os.IsNotExist(err) {
		t.Fatalf("empty image-cache should be removed: %v", err)
	}
}

func TestImagePathDefaultsToPngExtension(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	got := ImagePath("session-1", 4, "")
	want := filepath.Join(dir, "image-cache", "session-1", strconv.Itoa(4)+".png")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestImagePathNormalizesMediaTypeParameters(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	if got, want := ImagePath("session-1", 5, "image/png; charset=binary"), filepath.Join(dir, "image-cache", "session-1", "5.png"); got != want {
		t.Fatalf("png path = %q, want %q", got, want)
	}
	if got, want := ImagePath("session-1", 6, "image/svg+xml"), filepath.Join(dir, "image-cache", "session-1", "6.svg"); got != want {
		t.Fatalf("svg path = %q, want %q", got, want)
	}
	if got, want := ImagePath("session-1", 7, "image/x-png"), filepath.Join(dir, "image-cache", "session-1", "7.png"); got != want {
		t.Fatalf("x-png path = %q, want %q", got, want)
	}
}
