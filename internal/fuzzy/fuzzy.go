// Package fuzzy provides a pure-Go subsequence-based fuzzy matcher and scorer,
// similar to fzf-lite. It supports case-insensitive matching with three tiers:
//
//	1. Exact substring match (highest score)
//	2. Prefix match on the basename / last path segment
//	3. Subsequence match (lowest score)
//
// The package has no external dependencies and is fully unit-testable.
package fuzzy

import (
	"path"
	"strings"
	"unicode"
)

// MatchTier captures how a query matched a candidate string.
type MatchTier int

const (
	// TierExact means the query is a case-insensitive substring of the candidate.
	TierExact MatchTier = iota
	// TierPrefix means the query is a case-insensitive prefix of the candidate's
	// final path component (basename).
	TierPrefix
	// TierSubsequence means the query characters appear in order in the candidate,
	// but are not a continuous substring.
	TierSubsequence
)

// Match is the result of matching a single candidate against a query.
type Match struct {
	// Value is the original candidate string (not lowercased).
	Value string
	// Tier is the match quality tier.
	Tier MatchTier
	// Score is a higher-is-better numeric ranking within the same tier.
	// A higher score means a tighter match (e.g. shorter candidate, earlier match).
	Score int
}

// matchQuery holds a pre-processed query for efficient repeated matching.
type matchQuery struct {
	raw   string
	lower string
}

// newQuery pre-processes a query string.
func newQuery(q string) matchQuery {
	return matchQuery{raw: q, lower: strings.ToLower(q)}
}

// matchCandidate attempts to match q against candidate. It returns (Match, true)
// when the candidate matches, or (Match, false) when it does not.
func matchCandidate(q matchQuery, candidate string) (Match, bool) {
	if q.lower == "" {
		// Empty query matches everything; score by length (shorter = better).
		return Match{Value: candidate, Tier: TierExact, Score: 1000 - len(candidate)}, true
	}

	lower := strings.ToLower(candidate)

	// Tier 1: exact substring match.
	if idx := strings.Index(lower, q.lower); idx >= 0 {
		// Earlier match in the string = higher score; shorter candidate = higher score.
		score := 1000 - idx - len(candidate)
		return Match{Value: candidate, Tier: TierExact, Score: score}, true
	}

	// Tier 2: prefix match on the basename.
	base := strings.ToLower(path.Base(candidate))
	if strings.HasPrefix(base, q.lower) {
		score := 1000 - len(base) // shorter basename → higher score
		return Match{Value: candidate, Tier: TierPrefix, Score: score}, true
	}

	// Tier 3: subsequence match.
	if score, ok := subsequenceScore(lower, q.lower); ok {
		return Match{Value: candidate, Tier: TierSubsequence, Score: score}, true
	}

	return Match{}, false
}

// subsequenceScore returns (score, true) when all characters of q appear in s
// in order. The score is higher when characters appear earlier and the match is
// tighter (fewer gaps between matched characters).
func subsequenceScore(s, q string) (int, bool) {
	if q == "" {
		return 1000, true
	}
	sr := []rune(s)
	qr := []rune(q)
	j := 0
	firstMatch := -1
	lastMatch := -1
	for i := 0; i < len(sr) && j < len(qr); i++ {
		if unicode.ToLower(sr[i]) == unicode.ToLower(qr[j]) {
			if firstMatch < 0 {
				firstMatch = i
			}
			lastMatch = i
			j++
		}
	}
	if j < len(qr) {
		return 0, false
	}
	// Score: penalise late start and spread.
	spread := lastMatch - firstMatch
	score := 500 - firstMatch - spread
	return score, true
}

// Filter fuzzy-filters candidates against query q and returns matches sorted
// by tier (exact first) then by descending score within each tier.
// Candidates are returned as a new slice; the input slice is not mutated.
func Filter(candidates []string, q string) []Match {
	if len(candidates) == 0 {
		return nil
	}
	query := newQuery(q)
	// Collect matches.
	results := make([]Match, 0, len(candidates))
	for _, c := range candidates {
		if m, ok := matchCandidate(query, c); ok {
			results = append(results, m)
		}
	}
	// Stable sort: tier ascending (exact=0 < prefix=1 < subsequence=2),
	// then score descending within the same tier.
	sortMatches(results)
	return results
}

// sortMatches sorts ms in-place: lower tier first, higher score first within
// the same tier. Uses insertion sort which is stable and fast for small N.
func sortMatches(ms []Match) {
	n := len(ms)
	for i := 1; i < n; i++ {
		cur := ms[i]
		j := i - 1
		for j >= 0 && less(cur, ms[j]) {
			ms[j+1] = ms[j]
			j--
		}
		ms[j+1] = cur
	}
}

// less returns true when a should come before b in the sorted output.
func less(a, b Match) bool {
	if a.Tier != b.Tier {
		return a.Tier < b.Tier
	}
	return a.Score > b.Score
}

// Values is a convenience wrapper that returns only the matched strings
// (in ranked order) from a Filter call.
func Values(candidates []string, q string) []string {
	ms := Filter(candidates, q)
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Value
	}
	return out
}
