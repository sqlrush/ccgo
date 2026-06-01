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
	Name          string
	Match         bool
	Missing       bool
	FirstDiffLine int
	ExpectedText  string
	ActualText    string
	ExpectedDiff  string
	ActualDiff    string
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
	firstDiffLine, expectedDiff, actualDiff := snapshotTextDiff(expected, actual)
	return SnapshotComparison{
		Name:          snapshot.Name,
		Match:         expected == actual,
		FirstDiffLine: firstDiffLine,
		ExpectedText:  expected,
		ActualText:    actual,
		ExpectedDiff:  expectedDiff,
		ActualDiff:    actualDiff,
	}, nil
}

func (c SnapshotCorpus) CompareAll(snapshots []ANSISnapshot) ([]SnapshotComparison, error) {
	comparisons := make([]SnapshotComparison, 0, len(snapshots))
	for _, snapshot := range snapshots {
		comparison, err := c.Compare(snapshot)
		if err != nil {
			return nil, err
		}
		comparisons = append(comparisons, comparison)
	}
	return comparisons, nil
}

func (c SnapshotCorpus) WriteAll(snapshots []ANSISnapshot) error {
	for _, snapshot := range snapshots {
		if err := c.Write(snapshot); err != nil {
			return err
		}
	}
	return nil
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

func snapshotTextDiff(expected string, actual string) (int, string, string) {
	if expected == actual {
		return 0, "", ""
	}
	expectedLines := strings.Split(expected, "\n")
	actualLines := strings.Split(actual, "\n")
	max := len(expectedLines)
	if len(actualLines) > max {
		max = len(actualLines)
	}
	for i := 0; i < max; i++ {
		expectedLine := ""
		if i < len(expectedLines) {
			expectedLine = expectedLines[i]
		}
		actualLine := ""
		if i < len(actualLines) {
			actualLine = actualLines[i]
		}
		if expectedLine != actualLine {
			return i + 1, expectedLine, actualLine
		}
	}
	return 0, "", ""
}
