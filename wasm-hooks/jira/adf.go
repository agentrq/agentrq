// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// node is a node of the Atlassian Document Format, Jira's rich text.
type node struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Content []node `json:"content"`
	Attrs   struct {
		Text      string          `json:"text"`
		ShortName string          `json:"shortName"`
		URL       string          `json:"url"`
		Level     int             `json:"level"`
		Timestamp json.RawMessage `json:"timestamp"`
	} `json:"attrs"`
}

// maxDepth bounds how deep a document is walked; real ones are a few deep.
const maxDepth = 32

var blankLines = regexp.MustCompile(`\n{3,}`)

// text is a description or comment as plain text: a v2 API string as is,
// a v3 API document flattened.
func text(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var doc node
	if json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	var b strings.Builder
	write(&b, doc, "", 0)
	return strings.TrimSpace(blankLines.ReplaceAllString(b.String(), "\n\n"))
}

// write appends a node; indent prefixes the lines of a nested list.
func write(b *strings.Builder, n node, indent string, depth int) {
	if depth > maxDepth {
		return
	}
	children := func(indent string) {
		for _, c := range n.Content {
			write(b, c, indent, depth+1)
		}
	}
	switch n.Type {
	case "text":
		b.WriteString(n.Text)
	case "hardBreak":
		b.WriteString("\n" + indent)
	case "mention", "status":
		b.WriteString(n.Attrs.Text)
	case "emoji":
		if n.Attrs.Text != "" {
			b.WriteString(n.Attrs.Text)
		} else {
			b.WriteString(n.Attrs.ShortName)
		}
	case "inlineCard":
		b.WriteString(n.Attrs.URL)
	case "blockCard", "embedCard":
		b.WriteString(n.Attrs.URL + "\n\n")
	case "date":
		b.WriteString(date(n.Attrs.Timestamp))
	case "rule":
		b.WriteString("\n---\n")
	case "heading":
		b.WriteString("\n" + strings.Repeat("#", min(max(n.Attrs.Level, 1), 6)) + " ")
		children(indent)
		b.WriteString("\n\n")
	case "codeBlock":
		b.WriteString("\n```\n")
		children(indent)
		b.WriteString("\n```\n")
	case "bulletList", "orderedList":
		b.WriteString("\n")
		for i, item := range n.Content {
			marker := "- "
			if n.Type == "orderedList" {
				marker = strconv.Itoa(i+1) + ". "
			}
			b.WriteString(indent + marker)
			// A list item's paragraphs run on one line; a nested list
			// starts its own, further in.
			for _, c := range item.Content {
				if c.Type == "paragraph" {
					write(b, node{Type: "inline", Content: c.Content}, indent+"  ", depth+2)
				} else {
					write(b, c, indent+"  ", depth+2)
				}
			}
			b.WriteString("\n")
		}
	case "tableRow":
		for i, c := range n.Content {
			if i > 0 {
				b.WriteString(" | ")
			}
			var cell strings.Builder
			for _, cc := range c.Content {
				write(&cell, cc, "", depth+2)
			}
			b.WriteString(strings.Join(strings.Fields(cell.String()), " "))
		}
		b.WriteString("\n")
	case "inline":
		children(indent)
	default:
		// doc, paragraph, blockquote, panel, table, media and nodes added
		// later: their text, as a block of its own.
		children(indent)
		b.WriteString("\n\n")
	}
}

// date is a date node's timestamp, milliseconds as a string, as a day.
func date(raw json.RawMessage) string {
	var ms json.Number
	if json.Unmarshal(raw, &ms) != nil {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return ""
		}
		ms = json.Number(s)
	}
	n, err := ms.Int64()
	if err != nil {
		return ""
	}
	return time.UnixMilli(n).UTC().Format("2006-01-02")
}
