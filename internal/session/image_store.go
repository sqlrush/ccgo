package session

import (
	"encoding/base64"
	"net/url"
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

func ResolveStoredImagePath(sessionID contracts.ID, content PastedContent) (string, bool) {
	if sessionID == "" || content.Type != PastedContentImage || content.ID <= 0 {
		return "", false
	}
	imagePath := ImagePath(sessionID, content.ID, content.MediaType)
	if _, err := os.Stat(imagePath); err != nil {
		return "", false
	}
	rememberImagePath(content.ID, imagePath)
	return imagePath, true
}

func RestoreCachedImageContent(sessionID contracts.ID, content PastedContent, sourcePath string) (string, string, string, bool) {
	if sessionID == "" || content.Type != PastedContentImage || content.ID <= 0 {
		return "", "", "", false
	}
	localPath, inferredMediaType, ok := resolveCachedImagePath(sessionID, content, sourcePath)
	if !ok {
		return "", "", "", false
	}
	data, err := os.ReadFile(localPath)
	if err != nil {
		return "", "", "", false
	}
	mediaType := strings.TrimSpace(content.MediaType)
	if mediaType == "" {
		mediaType = inferredMediaType
	}
	return base64.StdEncoding.EncodeToString(data), mediaType, localPath, true
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
	mediaType = strings.TrimSpace(strings.ToLower(mediaType))
	if base, _, ok := strings.Cut(mediaType, ";"); ok {
		mediaType = strings.TrimSpace(base)
	}
	_, extension, ok := strings.Cut(mediaType, "/")
	if !ok || extension == "" {
		return "png"
	}
	if plus := strings.IndexByte(extension, '+'); plus >= 0 {
		extension = extension[:plus]
	}
	extension = strings.TrimPrefix(extension, "x-")
	extension = strings.NewReplacer("/", "", "\\", "", ":", "", ";", "").Replace(extension)
	if extension == "" {
		return "png"
	}
	return extension
}

func resolveCachedImagePath(sessionID contracts.ID, content PastedContent, sourcePath string) (string, string, bool) {
	path := strings.TrimSpace(sourcePath)
	if path != "" {
		localPath, ok := localImageCachePath(sessionID, content.ID, path)
		if !ok {
			return "", "", false
		}
		if _, err := os.Stat(localPath); err != nil {
			return "", "", false
		}
		return localPath, imageMediaTypeFromPath(localPath), true
	}
	localPath := ImagePath(sessionID, content.ID, content.MediaType)
	if validatedPath, ok := localImageCachePath(sessionID, content.ID, localPath); ok {
		return validatedPath, imageMediaTypeFromPath(validatedPath), true
	}
	matches, err := filepath.Glob(filepath.Join(ImageStoreDir(sessionID), strconv.Itoa(content.ID)+".*"))
	if err != nil || len(matches) == 0 {
		return "", "", false
	}
	for _, match := range matches {
		localPath, ok := localImageCachePath(sessionID, content.ID, match)
		if ok {
			return localPath, imageMediaTypeFromPath(localPath), true
		}
	}
	return "", "", false
}

func localImageCachePath(sessionID contracts.ID, imageID int, sourcePath string) (string, bool) {
	path := strings.TrimSpace(sourcePath)
	if path == "" || imageID <= 0 {
		return "", false
	}
	if parsed, err := url.Parse(path); err == nil && parsed.Scheme != "" {
		if parsed.Scheme != "file" || parsed.Path == "" {
			return "", false
		}
		path = parsed.Path
	}
	path = filepath.Clean(path)
	root := filepath.Clean(ImageStoreDir(sessionID))
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	if !strings.HasPrefix(filepath.Base(absPath), strconv.Itoa(imageID)+".") {
		return "", false
	}
	info, err := os.Lstat(absPath)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", false
	}
	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", false
	}
	resolvedRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", false
	}
	resolvedPath, err = filepath.Abs(resolvedPath)
	if err != nil {
		return "", false
	}
	resolvedRoot, err = filepath.Abs(resolvedRoot)
	if err != nil {
		return "", false
	}
	rel, err = filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	if !strings.HasPrefix(filepath.Base(resolvedPath), strconv.Itoa(imageID)+".") {
		return "", false
	}
	return resolvedPath, true
}

func imageMediaTypeFromPath(path string) string {
	extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	switch extension {
	case "":
		return ""
	case "jpg", "jpeg":
		return "image/jpeg"
	case "svg":
		return "image/svg+xml"
	default:
		return "image/" + extension
	}
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
