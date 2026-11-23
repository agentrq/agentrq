// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"encoding/json"
	"fmt"
)

// Content is a block of a tool result or a prompt message. The servers here
// only ever send text.
type Content interface {
	MarshalJSON() ([]byte, error)
	isContent()
}

// TextContent is a text block.
type TextContent struct {
	Text        string
	Meta        Meta
	Annotations *Annotations
}

func (*TextContent) isContent() {}

// MarshalJSON always writes "text", which the protocol requires even when
// empty.
func (c *TextContent) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type        string       `json:"type"`
		Text        string       `json:"text"`
		Meta        Meta         `json:"_meta,omitempty"`
		Annotations *Annotations `json:"annotations,omitempty"`
	}{"text", c.Text, c.Meta, c.Annotations})
}

// wireContent is a content block as read off the wire.
type wireContent struct {
	Type        string       `json:"type"`
	Text        string       `json:"text"`
	Meta        Meta         `json:"_meta,omitempty"`
	Annotations *Annotations `json:"annotations,omitempty"`
}

func contentFromWire(w *wireContent) (Content, error) {
	if w == nil {
		return nil, fmt.Errorf("nil content")
	}
	if w.Type != "text" {
		return nil, fmt.Errorf("unrecognized content type %q", w.Type)
	}
	return &TextContent{Text: w.Text, Meta: w.Meta, Annotations: w.Annotations}, nil
}

func contentsFromWire(ws []*wireContent) ([]Content, error) {
	var out []Content
	for _, w := range ws {
		c, err := contentFromWire(w)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}
