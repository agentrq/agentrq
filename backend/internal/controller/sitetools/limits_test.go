// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package sitetools

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func validTool(name string) Tool {
	return Tool{Name: name, Description: "does a thing", InputSchema: json.RawMessage(`{"type":"object"}`)}
}

func TestValidateTools(t *testing.T) {
	many := make([]Tool, MaxTools+1)
	for i := range many {
		many[i] = validTool(fmt.Sprintf("t%d", i))
	}
	longDesc := validTool("a")
	longDesc.Description = strings.Repeat("x", MaxDescription+1)
	bigSchema := validTool("a")
	bigSchema.InputSchema = json.RawMessage(`{"d":"` + strings.Repeat("x", MaxSchema) + `"}`)
	arraySchema := validTool("a")
	arraySchema.InputSchema = json.RawMessage(`[]`)
	badJSON := validTool("a")
	badJSON.InputSchema = json.RawMessage(`{`)

	refused := map[string][]Tool{
		"too many tools":      many,
		"long description":    {longDesc},
		"big schema":          {bigSchema},
		"duplicate name":      {validTool("a"), validTool("a")},
		"empty name":          {validTool("")},
		"array schema":        {arraySchema},
		"schema not JSON":     {badJSON},
		"null schema":         {{Name: "a", InputSchema: json.RawMessage(`null`)}},
		"one bad in the list": {validTool("a"), validTool("")},
	}
	for name, tools := range refused {
		t.Run(name, func(t *testing.T) {
			if err := ValidateTools(tools); err == nil {
				t.Fatal("want a refusal, got nil")
			}
		})
	}

	exact := validTool("exact")
	exact.Description = strings.Repeat("x", MaxDescription)
	noSchema := Tool{Name: "noSchema"}
	ok := append(append([]Tool{}, many[:MaxTools-2]...), exact, noSchema)
	if err := ValidateTools(ok); err != nil {
		t.Fatalf("valid list refused: %v", err)
	}
	if err := ValidateTools(nil); err != nil {
		t.Fatalf("empty list refused: %v", err)
	}
}

func TestToolReadOnly(t *testing.T) {
	f, tr := false, true
	cases := []struct {
		name string
		ann  *Annotations
		want bool
	}{
		{"nil annotations", nil, false},
		{"no hint", &Annotations{}, false},
		{"false", &Annotations{ReadOnlyHint: &f}, false},
		{"true", &Annotations{ReadOnlyHint: &tr}, true},
	}
	for _, c := range cases {
		if got := (Tool{Annotations: c.ann}).ReadOnly(); got != c.want {
			t.Errorf("%s: ReadOnly() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestFrameJSONIsCamelCase(t *testing.T) {
	b, err := json.Marshal(Frame{Type: FrameShares, WorkspaceID: "w", LastURL: "u", CallID: "c",
		Shares: []ShareState{{Origin: "https://a.b", WorkspaceID: "w", AlwaysAllow: []string{"x"}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{`"workspaceId"`, `"lastUrl"`, `"callId"`, `"alwaysAllow"`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("%s missing from %s", k, b)
		}
	}
}
