package memory

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

type RecallOptions struct {
	Limit int
}

type RecallMatch struct {
	Summary SessionSummary
	Score   int
	Snippet string
}

func RecallSessionSummaries(root string, query string, options RecallOptions) ([]RecallMatch, error) {
	terms := queryTerms(query)
	var matches []RecallMatch
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() || entry.Name() != SessionSummaryFilename {
			return nil
		}
		summary, err := LoadSessionSummary(path)
		if err != nil {
			return nil
		}
		if summary.UpdatedAt.IsZero() {
			if info, err := entry.Info(); err == nil {
				summary.UpdatedAt = info.ModTime()
			}
		}
		score := recallScore(summary, terms)
		if len(terms) > 0 && score == 0 {
			return nil
		}
		matches = append(matches, RecallMatch{
			Summary: summary,
			Score:   score,
			Snippet: snippet(summary.Summary, 240),
		})
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return matches[i].Summary.UpdatedAt.After(matches[j].Summary.UpdatedAt)
	})
	if options.Limit > 0 && len(matches) > options.Limit {
		matches = matches[:options.Limit]
	}
	return matches, nil
}

func recallScore(summary SessionSummary, terms []string) int {
	if len(terms) == 0 {
		return 0
	}
	haystack := strings.ToLower(string(summary.SessionID) + " " + summary.Summary)
	score := 0
	for _, term := range terms {
		score += strings.Count(haystack, term)
	}
	return score
}

func queryTerms(query string) []string {
	fields := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	seen := map[string]struct{}{}
	var terms []string
	for _, field := range fields {
		if len(field) < 2 {
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		terms = append(terms, field)
	}
	return terms
}

func snippet(text string, max int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if max <= 0 || len(text) <= max {
		return text
	}
	return strings.TrimSpace(text[:max])
}
