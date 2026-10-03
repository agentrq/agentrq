// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package bm25 ranks a small set of short documents, such as tool names and
// descriptions, against a query with Okapi BM25.
package bm25

import (
	"math"
	"strings"
	"unicode"
)

// The usual Okapi BM25 constants: k1 saturates a term's repeats, b scales a
// document's score down by its length relative to the average.
const (
	k1 = 1.2
	b  = 0.75
)

// Index is a fixed set of documents, tokenised and stemmed once.
type Index struct {
	docs   [][]string
	avgLen float64
}

// New tokenises docs for scoring.
func New(docs []string) *Index {
	ix := &Index{docs: make([][]string, len(docs))}
	total := 0
	for i, d := range docs {
		ix.docs[i] = stems(Tokens(d))
		total += len(ix.docs[i])
	}
	if len(docs) > 0 {
		ix.avgLen = float64(total) / float64(len(docs))
	}
	return ix
}

// Scores gives every document's score for query, in the order New was given
// them. A document sharing no term with the query scores 0. A query term
// matches any document word it begins, so "repo" finds "repositories", and a
// plural matches its singular, so "flights" finds "flight".
func (ix *Index) Scores(query string) []float64 {
	scores := make([]float64, len(ix.docs))
	n := float64(len(ix.docs))
	for _, term := range unique(stems(Tokens(query))) {
		counts := make([]int, len(ix.docs))
		docsWithTerm := 0
		for i, doc := range ix.docs {
			for _, word := range doc {
				if strings.HasPrefix(word, term) {
					counts[i]++
				}
			}
			if counts[i] > 0 {
				docsWithTerm++
			}
		}
		if docsWithTerm == 0 {
			continue
		}
		df := float64(docsWithTerm)
		idf := math.Log(1 + (n-df+0.5)/(df+0.5))
		for i, c := range counts {
			if c == 0 {
				continue
			}
			tf := float64(c)
			norm := 1 - b + b*float64(len(ix.docs[i]))/ix.avgLen
			scores[i] += idf * tf * (k1 + 1) / (tf + k1*norm)
		}
	}
	return scores
}

// Tokens splits s into lowercase words of letters and digits. It also splits
// camelCase and PascalCase, so a tool named searchIssues is found by "issues";
// a run of capitals keeps together, so "HTMLParser" is "html" and "parser".
func Tokens(s string) []string {
	var words []string
	var word []rune
	flush := func() {
		if len(word) > 0 {
			words = append(words, strings.ToLower(string(word)))
			word = word[:0]
		}
	}
	runes := []rune(s)
	for i, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			continue
		}
		if unicode.IsUpper(r) && len(word) > 0 {
			prev := runes[i-1]
			nextIsLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextIsLower) {
				flush()
			}
		}
		word = append(word, r)
	}
	flush()
	return words
}

// stems drops a plural's final s from each word longer than three letters,
// the same way for documents and queries, so "issues" and "issue" meet.
func stems(words []string) []string {
	for i, w := range words {
		if len(w) > 3 && strings.HasSuffix(w, "s") {
			words[i] = w[:len(w)-1]
		}
	}
	return words
}

func unique(words []string) []string {
	seen := make(map[string]bool, len(words))
	out := words[:0:0]
	for _, w := range words {
		if !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}
