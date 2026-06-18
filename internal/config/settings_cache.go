package config

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

type settingsFileFingerprint struct {
	Exists  bool
	Size    int64
	Mode    os.FileMode
	ModTime time.Time
}

type cachedSettingsFile struct {
	fingerprint settingsFileFingerprint
	data        []byte
}

var settingsFileCache = struct {
	sync.Mutex
	files map[string]cachedSettingsFile
}{
	files: map[string]cachedSettingsFile{},
}

func readSettingsFile(path string) ([]byte, error) {
	key := settingsFileCacheKey(path)
	fingerprint, err := statExistingSettingsFile(path)
	if err != nil {
		return nil, err
	}

	settingsFileCache.Lock()
	if cached, ok := settingsFileCache.files[key]; ok && sameSettingsFileFingerprint(cached.fingerprint, fingerprint) {
		data := cloneBytes(cached.data)
		settingsFileCache.Unlock()
		return data, nil
	}
	settingsFileCache.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if latest, statErr := statExistingSettingsFile(path); statErr == nil {
		fingerprint = latest
	} else {
		return cloneBytes(data), nil
	}

	settingsFileCache.Lock()
	settingsFileCache.files[key] = cachedSettingsFile{
		fingerprint: fingerprint,
		data:        cloneBytes(data),
	}
	settingsFileCache.Unlock()

	return cloneBytes(data), nil
}

func ResetSettingsCache() {
	ResetSettingsFileCache()
}

func ResetSettingsFileCache() {
	settingsFileCache.Lock()
	defer settingsFileCache.Unlock()
	settingsFileCache.files = map[string]cachedSettingsFile{}
}

func settingsFileCacheLen() int {
	settingsFileCache.Lock()
	defer settingsFileCache.Unlock()
	return len(settingsFileCache.files)
}

func statExistingSettingsFile(path string) (settingsFileFingerprint, error) {
	info, err := os.Stat(path)
	if err != nil {
		return settingsFileFingerprint{}, err
	}
	return settingsFileFingerprint{
		Exists:  true,
		Size:    info.Size(),
		Mode:    info.Mode(),
		ModTime: info.ModTime(),
	}, nil
}

func snapshotSettingsFile(path string) (settingsFileFingerprint, error) {
	fingerprint, err := statExistingSettingsFile(path)
	if err == nil {
		return fingerprint, nil
	}
	if os.IsNotExist(err) {
		return settingsFileFingerprint{}, nil
	}
	return settingsFileFingerprint{}, err
}

func settingsFileCacheKey(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

func sameSettingsFileFingerprint(a, b settingsFileFingerprint) bool {
	return a.Exists == b.Exists &&
		a.Size == b.Size &&
		a.Mode == b.Mode &&
		a.ModTime.Equal(b.ModTime)
}

func cloneBytes(data []byte) []byte {
	if data == nil {
		return nil
	}
	out := make([]byte, len(data))
	copy(out, data)
	return out
}
