// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPlainDescriptions(t *testing.T) {
	for raw, want := range map[string]string{
		`"  plain\ntext  "`: "plain\ntext",
		`null`:              "",
		``:                  "",
		`42`:                "",
		`{"type":"doc"}`:    "",
	} {
		if got := text(json.RawMessage(raw)); got != want {
			t.Errorf("text(%s) = %q, want %q", raw, got, want)
		}
	}
}

func TestDocumentsAreFlattened(t *testing.T) {
	doc := `{"type":"doc","version":1,"content":[
		{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Steps"}]},
		{"type":"paragraph","content":[
			{"type":"text","text":"Ask ","marks":[{"type":"strong"}]},
			{"type":"mention","attrs":{"id":"1","text":"@Ada"}},
			{"type":"text","text":" "},
			{"type":"emoji","attrs":{"shortName":":smile:","text":"😄"}},
			{"type":"emoji","attrs":{"shortName":":custom:"}},
			{"type":"hardBreak"},
			{"type":"text","text":"see "},
			{"type":"inlineCard","attrs":{"url":"https://x.test/a"}},
			{"type":"text","text":" by "},
			{"type":"date","attrs":{"timestamp":"1735689600000"}},
			{"type":"text","text":" "},
			{"type":"status","attrs":{"text":"BLOCKED"}}
		]},
		{"type":"bulletList","content":[
			{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"one"}]},
				{"type":"orderedList","content":[
					{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"first"}]}]},
					{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"second"}]}]}
				]}]},
			{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"two"}]}]}
		]},
		{"type":"codeBlock","attrs":{"language":"go"},"content":[{"type":"text","text":"x := 1"}]},
		{"type":"rule"},
		{"type":"table","content":[
			{"type":"tableRow","content":[
				{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"Key"}]}]},
				{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"Value"}]}]}]},
			{"type":"tableRow","content":[
				{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"a"}]}]},
				{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"b"}]}]}]}
		]},
		{"type":"blockCard","attrs":{"url":"https://x.test/b"}},
		{"type":"panel","content":[{"type":"paragraph","content":[{"type":"text","text":"Note"}]}]}
	]}`
	want := "## Steps\n\n" +
		"Ask @Ada 😄:custom:\nsee https://x.test/a by 2025-01-01 BLOCKED\n\n" +
		"- one\n  1. first\n  2. second\n\n- two\n\n" +
		"```\nx := 1\n```\n\n---\n" +
		"Key | Value\na | b\n\n" +
		"https://x.test/b\n\nNote"
	if got := text(json.RawMessage(doc)); got != want {
		t.Errorf("text =\n%s\n---want---\n%s", got, want)
	}
}

func TestDates(t *testing.T) {
	for raw, want := range map[string]string{
		`1735689600000`:   "2025-01-01",
		`"1735689600000"`: "2025-01-01",
		`"soon"`:          "",
		`{}`:              "",
		`1.5`:             "",
	} {
		if got := date(json.RawMessage(raw)); got != want {
			t.Errorf("date(%s) = %q, want %q", raw, got, want)
		}
	}
}

func TestHeadingLevelsAreBounded(t *testing.T) {
	for level, want := range map[int]string{0: "# H", 9: "###### H"} {
		doc := `{"type":"doc","content":[{"type":"heading","attrs":{"level":` + itoa(level) + `},"content":[{"type":"text","text":"H"}]}]}`
		if got := text(json.RawMessage(doc)); got != want {
			t.Errorf("level %d = %q, want %q", level, got, want)
		}
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// A document nested deeper than any real one is cut off, not walked.
func TestDeepDocumentsStop(t *testing.T) {
	doc := strings.Repeat(`{"type":"blockquote","content":[`, 200) + `{"type":"text","text":"deep"}` + strings.Repeat(`]}`, 200)
	if got := text(json.RawMessage(doc)); got != "" {
		t.Errorf("text = %q", got)
	}
	shallow := strings.Repeat(`{"type":"blockquote","content":[`, 5) + `{"type":"text","text":"near"}` + strings.Repeat(`]}`, 5)
	if got := text(json.RawMessage(shallow)); got != "near" {
		t.Errorf("text = %q", got)
	}
}
