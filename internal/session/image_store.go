package session

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

const MaxStoredImagePaths = 200

var storedImagePathCache = struct {
	mu    sync.Mutex
	paths map[int]string
	order []int
}{
	paths: map[int]string{},
}

func ImageStoreDir(sessionID contracts.ID) string {
	return filepath.Join(platform.ClaudeHomeDir(), "image-cache", string(sessionID))
}

func ImagePath(sessionID contracts.ID, imageID int, mediaType string) string {
	extension := imageExtension(mediaType)
	return filepath.Join(ImageStoreDir(sessionID), strconv.Itoa(imageID)+"."+extension)
}

func CacheImagePath(sessionID contracts.ID, content PastedContent) (string, bool) {
	if content.Type != PastedContentImage {
		return "", false
	}
	imagePath := ImagePath(sessionID, content.ID, content.MediaType)
	rememberImagePath(content.ID, imagePath)
	return imagePath, true
}

func StoreImage(sessionID contracts.ID, content PastedContent) (string, bool) {
	if content.Type != PastedContentImage {
		return "", false
	}
	data, err := base64.StdEncoding.DecodeString(content.Content)
	if err != nil {
		return "", false
	}
	if err := platform.EnsureDir(ImageStoreDir(sessionID)); err != nil {
		return "", false
	}
	imagePath := ImagePath(sessionID, content.ID, content.MediaType)
	f, err := os.OpenFile(imagePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", false
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(imagePath)
		}
	}()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return "", false
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return "", false
	}
	if err := f.Close(); err != nil {
		return "", false
	}
	ok = true
	rememberImagePath(content.ID, imagePath)
	return imagePath, true
}

func StoreImages(sessionID contracts.ID, pastedContents map[int]PastedContent) map[int]string {
	paths := map[int]string{}
	for id, content := range pastedContents {
		if content.Type != PastedContentImage {
			continue
		}
		path, ok := StoreImage(sessionID, content)
		if ok {
			paths[id] = path
		}
	}
	return paths
}

func GetStoredImagePath(imageID int) (string, bool) {
	storedImagePathCache.mu.Lock()
	defer storedImagePathCache.mu.Unlock()
	path, ok := storedImagePathCache.paths[imageID]
	return path, ok
}

func ClearStoredImagePaths() {
	storedImagePathCache.mu.Lock()
	defer storedImagePathCache.mu.Unlock()
	storedImagePathCache.paths = map[int]string{}
	storedImagePathCache.order = nil
}

func CleanupOldImageCaches(currentSessionID contracts.ID) int {
	baseDir := filepath.Join(platform.ClaudeHomeDir(), "image-cache")
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return 0
	}
	removed := 0
	for _, entry := range entries {
		if entry.Name() == string(currentSessionID) {
			continue
		}
		path := filepath.Join(baseDir, entry.Name())
		if err := os.RemoveAll(path); err == nil {
			removed++
		}
	}
	if remaining, err := os.ReadDir(baseDir); err == nil && len(remaining) == 0 {
		_ = os.Remove(baseDir)
	}
	return removed
}

func imageExtension(mediaType string) string {
	if mediaType == "" {
		mediaType = "image/png"
	}
	_, extension, ok := strings.Cut(mediaType, "/")
	if !ok || extension == "" {
		return "png"
	}
	return extension
}

func rememberImagePath(imageID int, imagePath string) {
	storedImagePathCache.mu.Lock()
	defer storedImagePathCache.mu.Unlock()
	if _, exists := storedImagePathCache.paths[imageID]; !exists {
		for len(storedImagePathCache.paths) >= MaxStoredImagePaths {
			if len(storedImagePathCache.order) == 0 {
				break
			}
			oldest := storedImagePathCache.order[0]
			storedImagePathCache.order = storedImagePathCache.order[1:]
			delete(storedImagePathCache.paths, oldest)
		}
		storedImagePathCache.order = append(storedImagePathCache.order, imageID)
	}
	storedImagePathCache.paths[imageID] = imagePath
}
