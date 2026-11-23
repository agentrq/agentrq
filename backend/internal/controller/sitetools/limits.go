// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// Package sitetools routes workspace agents' calls to the WebMCP tools of the
// sites a user shares from the AgentRQ Chrome extension.
package sitetools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// Limits on what a site may announce. Over any of them the announcement is
// refused, never truncated: a cut tool list or schema would let an agent call
// a tool it was not shown in full.
const (
	MaxTools       = 128
	MaxDescription = 1 << 10
	MaxSchema      = 32 << 10
	MaxResult      = 256 << 10
	CallDeadline   = 60 * time.Second
)

// Tool is one WebMCP tool a page registered.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
	Annotations *Annotations    `json:"annotations,omitempty"`
}

// Annotations are the page's hints about a tool.
type Annotations struct {
	ReadOnlyHint    *bool `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool `json:"destructiveHint,omitempty"`
}

// ReadOnly is true only for an explicit readOnlyHint: true; anything else
// needs the human's approval.
func (t Tool) ReadOnly() bool {
	return t.Annotations != nil && t.Annotations.ReadOnlyHint != nil && *t.Annotations.ReadOnlyHint
}

// ValidateTools refuses (never truncates) a list over any limit, a
// duplicate or empty name, or a schema that is not a JSON object.
func ValidateTools(tools []Tool) error {
	if len(tools) > MaxTools {
		return fmt.Errorf("sitetools: %d tools, the limit is %d", len(tools), MaxTools)
	}
	seen := make(map[string]bool, len(tools))
	for _, t := range tools {
		if t.Name == "" {
			return fmt.Errorf("sitetools: a tool has no name")
		}
		if seen[t.Name] {
			return fmt.Errorf("sitetools: tool %q is announced twice", t.Name)
		}
		seen[t.Name] = true
		if len(t.Description) > MaxDescription {
			return fmt.Errorf("sitetools: the description of %q is over %d bytes", t.Name, MaxDescription)
		}
		if len(t.InputSchema) == 0 {
			continue
		}
		if len(t.InputSchema) > MaxSchema {
			return fmt.Errorf("sitetools: the inputSchema of %q is over %d bytes", t.Name, MaxSchema)
		}
		var obj map[string]json.RawMessage
		if !bytes.HasPrefix(bytes.TrimSpace(t.InputSchema), []byte("{")) || json.Unmarshal(t.InputSchema, &obj) != nil {
			return fmt.Errorf("sitetools: the inputSchema of %q is not a JSON object", t.Name)
		}
	}
	return nil
}
