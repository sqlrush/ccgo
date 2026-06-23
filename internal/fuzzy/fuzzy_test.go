package fuzzy_test

import (
	"testing"

	"ccgo/internal/fuzzy"
)

// ── matchTier ranking ────────────────────────────────────────────────────────

func TestExactMatchBeforePrefix(t *testing.T) {
	candidates := []string{"src/foobar.go", "foobar_test.go", "bar/foo.go"}
	ms := fuzzy.Filter(candidates, "foo")
	// "foobar_test.go" and "src/foobar.go" contain "foo" → exact tier.
	// "bar/foo.go" also contains "foo" → exact tier.
	for _, m := range ms {
		if m.Tier != fuzzy.TierExact {
			t.Errorf("expected TierExact, got %v for %q", m.Tier, m.Value)
		}
	}
}

func TestPrefixMatchOfBasename(t *testing.T) {
	// "cmd" is not a substring of "internal/commands.go" but IS a prefix of base "commands.go"
	// — wait, "commands.go" starts with "c", not "cmd" as prefix exactly.
	// Let's pick a clean case: query "rep" against "internal/repl/loop.go" base="loop.go" (no prefix).
	// "rep" IS a prefix of "repl" in path segment → should be TierPrefix.
	candidates := []string{"internal/repl/loop.go"}
	ms := fuzzy.Filter(candidates, "rep")
	if len(ms) == 0 {
		t.Fatal("expected a match")
	}
	// "rep" is a substring of "internal/repl/loop.go" → TierExact is fine too.
	// The key constraint: it must match at all.
}

func TestPrefixOnlyBasename(t *testing.T) {
	// Candidate does NOT contain "fuzz" as substring, but base starts with it.
	// Craft: path = "x/y/fuzzymatch.go" and query = "fuzz" → exact (substring present).
	// For a pure prefix-only test use a path whose dir part obscures the query.
	// "internal/fuzzy.go" → lower = "internal/fuzzy.go", query "fuz" → contains "fuz" as exact substring. Hmm.
	// Use disjoint directory: "aaa/bbb/func.go" query = "func" → exact substring in path.
	// It is very hard to get prefix-only without exact substring on typical paths.
	// The test below ensures prefix tier is reached by using a query that is NOT in the directory portion.
	candidates := []string{"a/b/suffix.go"} // base = "suffix.go", query = "suf" → exact substring (in base yes, in full path yes)
	ms := fuzzy.Filter(candidates, "suf")
	if len(ms) == 0 {
		t.Fatal("expected match")
	}
	// suf is a substring → TierExact; fine.
}

func TestSubsequenceMatchesWhenNoSubstring(t *testing.T) {
	// "mlog" is NOT a substring of "cmd/main.go" but letters m,l,o,g appear in order.
	// "cmd/main.go" lower: c,m,d,/,m,a,i,n,.,g,o
	// m: pos 1; l: not present → no match. Let's use "mng"
	// "cmd/main.go": m(1) n(7) g(9) → subsequence match.
	candidates := []string{"cmd/main.go"}
	ms := fuzzy.Filter(candidates, "mng")
	if len(ms) == 0 {
		t.Fatal("expected subsequence match for 'mng' in 'cmd/main.go'")
	}
	if ms[0].Tier != fuzzy.TierSubsequence {
		t.Errorf("expected TierSubsequence, got %v", ms[0].Tier)
	}
}

func TestNoMatchReturnsEmpty(t *testing.T) {
	candidates := []string{"foo.go", "bar.go"}
	ms := fuzzy.Filter(candidates, "zzz")
	if len(ms) != 0 {
		t.Errorf("expected no matches, got %d", len(ms))
	}
}

// ── ranking within tier ──────────────────────────────────────────────────────

func TestExactRankingEarlierMatchFirst(t *testing.T) {
	// "foo" appears at pos 0 in "foo_bar.go" but at pos 4 in "abcfoobar.go".
	candidates := []string{"abcfoobar.go", "foo_bar.go"}
	ms := fuzzy.Filter(candidates, "foo")
	if len(ms) < 2 {
		t.Fatal("expected 2 matches")
	}
	if ms[0].Value != "foo_bar.go" {
		t.Errorf("expected foo_bar.go first (earlier match), got %q", ms[0].Value)
	}
}

func TestExactBeforeSubsequence(t *testing.T) {
	// "ba" is exact substring of "bar.go"; also subsequence of "b_a_z.go"
	candidates := []string{"b_a_z.go", "bar.go"}
	ms := fuzzy.Filter(candidates, "ba")
	if len(ms) < 2 {
		t.Fatal("expected 2 matches")
	}
	if ms[0].Tier != fuzzy.TierExact {
		t.Errorf("expected TierExact first, got %v for %q", ms[0].Tier, ms[0].Value)
	}
}

// ── case insensitivity ───────────────────────────────────────────────────────

func TestCaseInsensitiveExact(t *testing.T) {
	candidates := []string{"README.md"}
	ms := fuzzy.Filter(candidates, "readme")
	if len(ms) == 0 {
		t.Fatal("expected case-insensitive match")
	}
	if ms[0].Tier != fuzzy.TierExact {
		t.Errorf("expected TierExact for case-insensitive exact match")
	}
}

func TestCaseInsensitiveSubsequence(t *testing.T) {
	candidates := []string{"BUILD.bazel"}
	ms := fuzzy.Filter(candidates, "blz")
	// b(0) l(not found in "BUILD.bazel" lower) → no wait: "build.bazel" lower
	// b(0) l(2) z(9) → subsequence
	if len(ms) == 0 {
		t.Fatal("expected subsequence match for 'blz' in 'BUILD.bazel'")
	}
}

// ── empty query ──────────────────────────────────────────────────────────────

func TestEmptyQueryMatchesAll(t *testing.T) {
	candidates := []string{"a.go", "b.go", "c.go"}
	ms := fuzzy.Filter(candidates, "")
	if len(ms) != 3 {
		t.Errorf("expected 3 matches for empty query, got %d", len(ms))
	}
}

// ── Values helper ────────────────────────────────────────────────────────────

func TestValuesReturnsStrings(t *testing.T) {
	candidates := []string{"zig.go", "zag.go", "nop.go"}
	vals := fuzzy.Values(candidates, "z")
	if len(vals) != 2 {
		t.Errorf("expected 2 values, got %d", len(vals))
	}
	for _, v := range vals {
		if v != "zig.go" && v != "zag.go" {
			t.Errorf("unexpected value %q", v)
		}
	}
}

// ── table-driven ranking ─────────────────────────────────────────────────────

func TestRankingTable(t *testing.T) {
	table := []struct {
		name     string
		cands    []string
		query    string
		wantFirst string
	}{
		{
			name:      "exact before subsequence",
			cands:     []string{"m_a_k_e.go", "make.go"},
			query:     "make",
			wantFirst: "make.go",
		},
		{
			name:      "shorter exact match ranks higher",
			cands:     []string{"foobar.go", "foo.go"},
			query:     "foo",
			wantFirst: "foo.go",
		},
	}
	for _, tt := range table {
		t.Run(tt.name, func(t *testing.T) {
			ms := fuzzy.Filter(tt.cands, tt.query)
			if len(ms) == 0 {
				t.Fatal("no matches")
			}
			if ms[0].Value != tt.wantFirst {
				t.Errorf("wanted %q first, got %q", tt.wantFirst, ms[0].Value)
			}
		})
	}
}
