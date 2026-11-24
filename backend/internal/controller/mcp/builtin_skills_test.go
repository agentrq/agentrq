// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"context"
	"reflect"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/agentrq/agentrq/backend/internal/service/mcp"
	"github.com/agentrq/agentrq/backend/internal/service/mcpskill"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/golang/mock/gomock"
)

const workspaceSkillURI = "skill://agentrq/SKILL.md"

// An agent connected to a workspace gets the workspace skill over the Skills
// extension, with no plugin installed, and each read is counted against the
// workspace.
func TestWorkspaceServer_ServesTheWorkspaceSkill(t *testing.T) {
	ctrl := gomock.NewController(t)
	pub := mock_pubsub.NewMockService(ctrl)
	var counted []MCPEvent
	pub.EXPECT().Publish(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, req pubsub.PublishRequest) (*pubsub.PublishResponse, error) {
		if ev, ok := req.Event.(MCPEvent); ok && ev.Action == ActionMCPMethodCall {
			counted = append(counted, ev)
		}
		return nil, nil
	}).AnyTimes()
	ps := NewWorkspaceServer(
		100, 0, "user", "http://localhost",
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		eventbus.New(), nil, nil, "icon", "name", "desc", nil, nil, nil, nil,
	)
	ps.pubsub = pub
	cs := connectTestServer(t, ps.MCPServer())
	ctx := context.Background()

	caps := cs.InitializeResult().Capabilities
	if want := (map[string]any{mcpskill.Extension: map[string]any{"directoryRead": true}}); !reflect.DeepEqual(caps.Extensions, want) {
		t.Errorf("extensions = %v, want %v", caps.Extensions, want)
	}
	if caps.Experimental["claude/channel"] == nil || caps.Experimental["claude/channel/permission"] == nil {
		t.Errorf("the channel capabilities are no longer advertised: %v", caps.Experimental)
	}

	list, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var listed *mcp.Resource
	for _, r := range list.Resources {
		if r.URI == workspaceSkillURI {
			listed = r
		}
	}
	if listed == nil || listed.MIMEType != "text/markdown" || listed.Description == "" {
		t.Fatalf("resources = %v, want %s with its description", list.Resources, workspaceSkillURI)
	}

	want, err := builtinSkillFiles.ReadFile("skills/agentrq/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	rs, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: workspaceSkillURI})
	if err != nil {
		t.Fatal(err)
	}
	if len(rs.Contents) != 1 || rs.Contents[0].Text != string(want) {
		t.Errorf("read %s = %+v, want the skill's SKILL.md", workspaceSkillURI, rs.Contents)
	}

	if len(counted) != 1 || counted[0].ToolName != "resource:agentrq" || counted[0].WorkspaceID != 100 {
		t.Errorf("counted %+v, want one read of the agentrq skill in workspace 100", counted)
	}
}
