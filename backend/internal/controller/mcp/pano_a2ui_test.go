// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	"github.com/agentrq/agentrq/backend/internal/service/a2ui"
)

// The card the skill shows an agent in a doc is one a doc's save accepts.
func TestPanoA2UIExample_IsADocsCard(t *testing.T) {
	body, err := builtinSkillFiles.ReadFile("skills/agentrq/references/pano-a2ui.md")
	if err != nil {
		t.Fatal(err)
	}
	validator, err := a2ui.New()
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile("(?s)~~~a2ui\n(.*?)\n~~~")
	cards := 0
	for _, c := range exampleCalls(t, "pano-a2ui.md", body) {
		md, _ := c.Payload["markdown"].(string)
		files := map[string]string{}
		list, _ := c.Payload["files"].([]any)
		for _, f := range list {
			name := f.(map[string]any)["filename"].(string)
			files[name] = "attachment:" + name
		}
		for _, m := range block.FindAllStringSubmatch(md, -1) {
			cards++
			if _, err := validator.Validate(context.Background(), json.RawMessage(m[1]), a2ui.Options{Components: crud.PanoA2UIComponents, Attachments: files}); err != nil {
				t.Errorf("the example's card is refused: %v", err)
			}
		}
	}
	if cards == 0 {
		t.Error("pano-a2ui.md shows no card")
	}
}
