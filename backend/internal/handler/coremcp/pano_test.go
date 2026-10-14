// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	mcpevent "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

// A hand-written fake rather than a generated mock: one CI job runs this
// package's tests without generating mocks.
type mockPanoCrud struct {
	crud.Controller

	workspaces []entity.Workspace
	listErr    error
	listed     entity.ListWorkspacesRequest
	deleted    *entity.DeletePanoDocRequest
	agent      int64
}

func (m *mockPanoCrud) ListWorkspaces(_ context.Context, req entity.ListWorkspacesRequest) (*entity.ListWorkspacesResponse, error) {
	m.listed = req
	if m.listErr != nil {
		return nil, m.listErr
	}
	return &entity.ListWorkspacesResponse{Workspaces: m.workspaces}, nil
}

func (m *mockPanoCrud) DeletePanoDoc(ctx context.Context, req entity.DeletePanoDocRequest) error {
	m.deleted, m.agent = &req, entity.GetAgentWorkspace(ctx)
	return nil
}

func TestPano_WritesAsTheSupervisorWorkspace(t *testing.T) {
	ctrl := &mockPanoCrud{workspaces: []entity.Workspace{
		{ID: 301, Name: "alpha"},
		// A fork of the supervisor carries its name, and is not it.
		{ID: 302, Name: "supervisor", ForkOfID: testWorkspace},
		{ID: testWorkspace, Name: "supervisor"},
	}}
	s := &WorkspaceServer{crud: ctrl}
	body := textOf(t, toolResult(s.handlePano(authedContext(), nil, mcpevent.PanoParams{Action: "delete", Payload: map[string]any{"id": base62(7)}})))

	if ctrl.listed != (entity.ListWorkspacesRequest{UserID: testUserID, IncludeArchived: true}) {
		t.Errorf("the workspaces were listed with %+v, want the user's, archived included", ctrl.listed)
	}
	if ctrl.deleted == nil || *ctrl.deleted != (entity.DeletePanoDocRequest{UserID: testUserID, WorkspaceID: testWorkspace, ID: 7}) {
		t.Errorf("the controller was asked to delete %+v, want doc 7 as the supervisor workspace %d", ctrl.deleted, testWorkspace)
	}
	if ctrl.agent != testWorkspace {
		t.Errorf("the agent's workspace was %d, want the supervisor's %d, whose task a doc is linked to", ctrl.agent, testWorkspace)
	}
	if !strings.Contains(body, "Deleted Pano doc "+base62(7)) {
		t.Errorf("the tool answered %q, want it to say the doc was deleted", body)
	}
}

func TestPano_Refusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  context.Context
		ctrl *mockPanoCrud
		want string
	}{
		{"a call with no user is refused", context.Background(), &mockPanoCrud{}, "context canceled"},
		{"a failure to list the workspaces is reported", authedContext(), &mockPanoCrud{listErr: errors.New("database unavailable")}, "database unavailable"},
		{"an account with no supervisor workspace is told so", authedContext(), &mockPanoCrud{workspaces: []entity.Workspace{{ID: 301, Name: "alpha"}}}, "no supervisor workspace"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &WorkspaceServer{crud: tc.ctrl}
			res := toolResult(s.handlePano(tc.ctx, nil, mcpevent.PanoParams{Action: "delete", Payload: map[string]any{"id": base62(7)}}))
			if !res.isError || !strings.Contains(res.text, tc.want) {
				t.Errorf("the tool answered %+v, want an error containing %q", res, tc.want)
			}
			if tc.ctrl.deleted != nil {
				t.Errorf("the controller was asked to delete %+v, want no call", tc.ctrl.deleted)
			}
		})
	}
}
