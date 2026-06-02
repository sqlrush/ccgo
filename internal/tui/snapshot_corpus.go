package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

type SnapshotCorpusReport struct {
	Comparisons []SnapshotComparison
	Unexpected  []string
}

func (r SnapshotCorpusReport) Passed() bool {
	if len(r.Unexpected) > 0 {
		return false
	}
	for _, comparison := range r.Comparisons {
		if !comparison.Match || comparison.Missing {
			return false
		}
	}
	return true
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
		if !os.IsNotExist(err) {
			return "", err
		}
		ansiData, ansiErr := os.ReadFile(c.pathBase(name) + ".ansi")
		if ansiErr != nil {
			return "", err
		}
		return StripANSI(string(ansiData)), nil
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

func (c SnapshotCorpus) CompareAllStrict(snapshots []ANSISnapshot) (SnapshotCorpusReport, error) {
	comparisons, err := c.CompareAll(snapshots)
	if err != nil {
		return SnapshotCorpusReport{}, err
	}
	unexpected, err := c.UnexpectedBaselines(snapshots)
	if err != nil {
		return SnapshotCorpusReport{}, err
	}
	return SnapshotCorpusReport{Comparisons: comparisons, Unexpected: unexpected}, nil
}

func (c SnapshotCorpus) UnexpectedBaselines(snapshots []ANSISnapshot) ([]string, error) {
	if c.Dir == "" {
		return nil, fmt.Errorf("snapshot corpus dir is required")
	}
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	expected := map[string]struct{}{}
	for _, snapshot := range snapshots {
		if snapshot.Name == "" {
			continue
		}
		expected[sanitizeSnapshotName(snapshot.Name)] = struct{}{}
	}
	unexpectedSet := map[string]struct{}{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := ""
		switch {
		case strings.HasSuffix(entry.Name(), ".txt"):
			name = strings.TrimSuffix(entry.Name(), ".txt")
		case strings.HasSuffix(entry.Name(), ".ansi"):
			name = strings.TrimSuffix(entry.Name(), ".ansi")
		default:
			continue
		}
		if _, ok := expected[name]; !ok {
			unexpectedSet[name] = struct{}{}
		}
	}
	var unexpected []string
	for name := range unexpectedSet {
		unexpected = append(unexpected, name)
	}
	sort.Strings(unexpected)
	return unexpected, nil
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
