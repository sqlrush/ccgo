package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type SnapshotCorpus struct {
	Dir string
}

type SnapshotComparison struct {
	Name         string
	Match        bool
	Missing      bool
	ExpectedText string
	ActualText   string
}

func (c SnapshotCorpus) Write(snapshot ANSISnapshot) error {
	if c.Dir == "" {
		return fmt.Errorf("snapshot corpus dir is required")
	}
	if snapshot.Name == "" {
		return fmt.Errorf("snapshot name is required")
	}
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return err
	}
	base := c.pathBase(snapshot.Name)
	if err := os.WriteFile(base+".ansi", []byte(snapshot.Output), 0o644); err != nil {
		return err
	}
	return os.WriteFile(base+".txt", []byte(snapshot.Text), 0o644)
}

func (c SnapshotCorpus) LoadText(name string) (string, error) {
	data, err := os.ReadFile(c.pathBase(name) + ".txt")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (c SnapshotCorpus) Compare(snapshot ANSISnapshot) (SnapshotComparison, error) {
	expected, err := c.LoadText(snapshot.Name)
	if err != nil {
		if os.IsNotExist(err) {
			return SnapshotComparison{
				Name:       snapshot.Name,
				Missing:    true,
				ActualText: snapshot.Text,
			}, nil
		}
		return SnapshotComparison{}, err
	}
	actual := snapshot.Text
	return SnapshotComparison{
		Name:         snapshot.Name,
		Match:        expected == actual,
		ExpectedText: expected,
		ActualText:   actual,
	}, nil
}

func (c SnapshotCorpus) pathBase(name string) string {
	return filepath.Join(c.Dir, sanitizeSnapshotName(name))
}

func sanitizeSnapshotName(name string) string {
	name = strings.TrimSpace(name)
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "\x00", "")
	name = replacer.Replace(name)
	if name == "" {
		return "snapshot"
	}
	return name
}
