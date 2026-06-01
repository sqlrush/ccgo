package memory

import (
	"sort"
	"strings"
	"unicode"

	"ccgo/internal/contracts"
)

type RecallOptions struct {
	Limit            int
	ExcludeSessionID contracts.ID
}

type RecallMatch struct {
	Summary SessionSummary
	Score   int
	Snippet string
}

func RecallSessionSummaries(root string, query string, options RecallOptions) ([]RecallMatch, error) {
	terms := queryTerms(query)
	var matches []RecallMatch
	summaries, err := LoadSessionSummaries(root)
	if err != nil {
		return nil, err
	}
	for _, summary := range summaries {
		if options.ExcludeSessionID != "" && summary.SessionID == options.ExcludeSessionID {
			continue
		}
		score := recallScore(summary, terms)
		if len(terms) > 0 && score == 0 {
			continue
		}
		matches = append(matches, RecallMatch{
			Summary: summary,
			Score:   score,
			Snippet: snippet(summary.Summary, 240),
		})
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
