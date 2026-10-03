// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package bm25

import (
	"slices"
	"testing"
)

func TestTokens(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"searchIssues", []string{"search", "issues"}},
		{"ListRepositories", []string{"list", "repositories"}},
		{"HTMLParser", []string{"html", "parser"}},
		{"getV2Status", []string{"get", "v2", "status"}},
		{"book_flight, then pay!", []string{"book", "flight", "then", "pay"}},
		{"Ünïcode wörds", []string{"ünïcode", "wörds"}},
		{"  -- ", nil},
	}
	for _, tc := range cases {
		if got := Tokens(tc.in); !slices.Equal(got, tc.want) {
			t.Errorf("Tokens(%q) returned %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestScoresRankTheBestMatchFirst(t *testing.T) {
	ix := New([]string{
		"bookFlight Book a flight",
		"searchFlights Search flights between two airports",
		"searchIssues Find issues in a repository",
		"weather Today's forecast",
	})
	scores := ix.Scores("search flights")

	if scores[3] != 0 {
		t.Errorf("a document sharing no word with the query scored %v, want 0", scores[3])
	}
	if !(scores[1] > scores[0] && scores[1] > scores[2] && scores[0] > 0 && scores[2] > 0) {
		t.Errorf("scores %v: want searchFlights (both words) above bookFlight (the singular) and searchIssues (one word), both above 0", scores)
	}
}

func TestScoresMatchWordsTheQueryBegins(t *testing.T) {
	scores := New([]string{"listRepositories", "createIssue"}).Scores("repo")
	if scores[0] <= 0 || scores[1] != 0 {
		t.Errorf("\"repo\" scored %v, want only listRepositories above 0", scores)
	}
}

// A rarer word counts for more, and a shorter document with the same matches
// ranks above a longer one.
func TestScoresWeighRarityAndLength(t *testing.T) {
	scores := New([]string{
		"create issue",
		"create issue with a long description of what it does",
		"create pull request",
	}).Scores("create issue")
	if !(scores[0] > scores[1] && scores[1] > scores[2] && scores[2] > 0) {
		t.Errorf("scores %v: want the short issue tool, then the long one, then the one with only the common word", scores)
	}
}

// A word of three letters or fewer keeps its s, so "bus" is not "bu".
func TestScoresKeepShortWordsWhole(t *testing.T) {
	scores := New([]string{"bus timetable", "build"}).Scores("bus")
	if scores[0] <= 0 || scores[1] != 0 {
		t.Errorf("\"bus\" scored %v, want only the bus document above 0", scores)
	}
}

// Repeating a word in the query does not count it twice.
func TestScoresIgnoreARepeatedQueryWord(t *testing.T) {
	ix := New([]string{"search issues", "list issues"})
	once, twice := ix.Scores("search"), ix.Scores("search search")
	if !slices.Equal(once, twice) {
		t.Errorf("\"search search\" scored %v, want the same as \"search\": %v", twice, once)
	}
}

func TestScoresOnNoDocuments(t *testing.T) {
	if got := New(nil).Scores("anything"); len(got) != 0 {
		t.Errorf("an empty index returned scores %v, want none", got)
	}
}
