package config

import "sync"

type SettingsChangeKind string

const (
	SettingsChangeCreated  SettingsChangeKind = "created"
	SettingsChangeModified SettingsChangeKind = "modified"
	SettingsChangeDeleted  SettingsChangeKind = "deleted"
)

type SettingsFileChange struct {
	Path string
	Kind SettingsChangeKind
}

type SettingsChangeDetector struct {
	mu       sync.Mutex
	snapshot map[string]settingsFileFingerprint
}

func NewSettingsChangeDetector(paths []string) (*SettingsChangeDetector, error) {
	detector := &SettingsChangeDetector{
		snapshot: map[string]settingsFileFingerprint{},
	}
	if err := detector.Reset(paths); err != nil {
		return nil, err
	}
	return detector, nil
}

func (d *SettingsChangeDetector) Reset(paths []string) error {
	next, err := snapshotSettingsFiles(paths)
	if err != nil {
		return err
	}
	d.mu.Lock()
	d.snapshot = next
	d.mu.Unlock()
	return nil
}

func (d *SettingsChangeDetector) DetectChanges(paths []string) ([]SettingsFileChange, error) {
	next, err := snapshotSettingsFiles(paths)
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	changes := diffSettingsFileSnapshots(d.snapshot, next)
	d.snapshot = next
	d.mu.Unlock()

	if len(changes) > 0 {
		ResetSettingsFileCache()
	}
	return changes, nil
}

func snapshotSettingsFiles(paths []string) (map[string]settingsFileFingerprint, error) {
	out := make(map[string]settingsFileFingerprint, len(paths))
	for _, path := range paths {
		key := settingsFileCacheKey(path)
		fingerprint, err := snapshotSettingsFile(path)
		if err != nil {
			return nil, err
		}
		out[key] = fingerprint
	}
	return out, nil
}

func diffSettingsFileSnapshots(before, after map[string]settingsFileFingerprint) []SettingsFileChange {
	seen := map[string]struct{}{}
	var changes []SettingsFileChange
	for path, current := range after {
		previous := before[path]
		seen[path] = struct{}{}
		change, ok := classifySettingsFileChange(previous, current)
		if ok {
			changes = append(changes, SettingsFileChange{Path: path, Kind: change})
		}
	}
	for path, previous := range before {
		if _, ok := seen[path]; ok {
			continue
		}
		if previous.Exists {
			changes = append(changes, SettingsFileChange{Path: path, Kind: SettingsChangeDeleted})
		}
	}
	return changes
}

func classifySettingsFileChange(previous, current settingsFileFingerprint) (SettingsChangeKind, bool) {
	switch {
	case !previous.Exists && current.Exists:
		return SettingsChangeCreated, true
	case previous.Exists && !current.Exists:
		return SettingsChangeDeleted, true
	case previous.Exists && current.Exists && !sameSettingsFileFingerprint(previous, current):
		return SettingsChangeModified, true
	default:
		return "", false
	}
}
