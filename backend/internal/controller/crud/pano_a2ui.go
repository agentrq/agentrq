// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync"

	"github.com/agentrq/agentrq/backend/internal/service/a2ui"
)

// PanoA2UIComponents are what an a2ui block in a Pano doc may draw: display
// only, since nothing answers a doc. Progress is the checklist's, and List a
// markdown table's.
var PanoA2UIComponents = []string{"Text", "Row", "Column", "Card", "Tabs", "Divider", "Icon", "Image", "Video", "AudioPlayer", "Chart"}

// panoA2UIValidator loads the A2UI schemas once, on the first doc that has a
// block.
var panoA2UIValidator = sync.OnceValues(a2ui.New)

// panoA2UIBlock is a fenced code block tagged a2ui: its first line's number,
// from 1, and its content.
type panoA2UIBlock struct {
	line int
	body string
}

// panoFenceOpen is a code fence's opening line: its run of backticks or
// tildes, and its info string's first word.
var panoFenceOpen = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})[ \t]*([^ \t]*)")

// panoA2UIBlocks is the markdown's a2ui blocks, fenced as CommonMark fences
// them, as the web app draws each one: a fence closes on a line of at least
// as many of its marks, and nothing else.
func panoA2UIBlocks(markdown string) []panoA2UIBlock {
	var blocks []panoA2UIBlock
	lines := strings.Split(markdown, "\n")
	fence := ""
	var body []string
	for i, line := range lines {
		if fence != "" {
			if trimmed := strings.TrimLeft(line, " "); len(line)-len(trimmed) < 4 && strings.HasPrefix(trimmed, fence) &&
				strings.TrimSpace(strings.TrimLeft(trimmed, fence[:1])) == "" {
				if body != nil {
					blocks[len(blocks)-1].body = strings.Join(body, "\n")
				}
				fence, body = "", nil
			} else if body != nil {
				body = append(body, line)
			}
			continue
		}
		m := panoFenceOpen.FindStringSubmatch(line)
		// A backtick fence's info string has no backtick, or it is no fence.
		if m == nil || m[1][0] == '`' && strings.Contains(strings.TrimLeft(line, " ")[len(m[1]):], "`") {
			continue
		}
		fence = m[1]
		// The info string's first word is the block's language.
		if m[2] == "a2ui" {
			body = []string{}
			blocks = append(blocks, panoA2UIBlock{line: i + 1})
		}
	}
	if body != nil {
		// Unclosed, it runs to the end, as markdown draws it.
		blocks[len(blocks)-1].body = strings.Join(body, "\n")
	}
	return blocks
}

// panoMedia is a media reference in an a2ui block.
var panoMedia = regexp.MustCompile(`"attachment:([^"\\]+)"`)

// panoA2UILinks adds the files the markdown's a2ui blocks show to links: a
// block's media is a link to the doc's file, which keeps it.
func panoA2UILinks(markdown string, links map[string]bool) {
	for _, b := range panoA2UIBlocks(markdown) {
		for _, m := range panoMedia.FindAllStringSubmatch(b.body, -1) {
			links[m[1]] = true
		}
	}
}

// checkPanoA2UI refuses a doc whose a2ui block is not one the web app can
// draw, saying where and why. files are the doc's files once saved.
func checkPanoA2UI(ctx context.Context, markdown string, files map[string]bool) error {
	blocks := panoA2UIBlocks(markdown)
	if len(blocks) == 0 {
		return nil
	}
	validator, err := panoA2UIValidator()
	if err != nil {
		return err
	}
	// A reference stays as written: the markdown is the doc, and the web app
	// resolves it to the file's link.
	attachments := make(map[string]string, len(files))
	for name := range files {
		attachments[name] = "attachment:" + name
	}
	opts := a2ui.Options{Components: PanoA2UIComponents, Attachments: attachments, AttachedTo: "this doc"}
	for _, b := range blocks {
		if _, err := validator.Validate(ctx, json.RawMessage(b.body), opts); err != nil {
			return panoRefuse("the a2ui block at line %d is not valid A2UI v1.0: %s. A block may use %s; see the pano tool's help", b.line, strings.TrimPrefix(err.Error(), "ui is not valid A2UI v1.0: "), strings.Join(PanoA2UIComponents, ", "))
		}
	}
	return nil
}
