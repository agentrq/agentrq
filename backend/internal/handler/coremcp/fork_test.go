// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/mustafaturan/monoflake"
)

const (
	forkParentID = int64(10)
	forkID       = int64(20)
)

type mockForkCrud struct {
	crud.Controller

	forkReq entity.ForkWorkspaceRequest
	forkErr error
}

func (m *mockForkCrud) ForkWorkspace(_ context.Context, req entity.ForkWorkspaceRequest) (*entity.ForkWorkspaceResponse, error) {
	m.forkReq = req
	if m.forkErr != nil {
		return nil, m.forkErr
	}
	return &entity.ForkWorkspaceResponse{Workspace: entity.Workspace{
		ID: forkID, Name: "api fork", Icon: "big-icon-bytes", ForkOfID: req.WorkspaceID,
	}}, nil
}

func (m *mockForkCrud) ListWorkspaces(context.Context, entity.ListWorkspacesRequest) (*entity.ListWorkspacesResponse, error) {
	return &entity.ListWorkspacesResponse{Workspaces: []entity.Workspace{
		{ID: forkParentID, Name: "api", ForkCount: 1},
		{ID: forkID, Name: "api fork", ForkOfID: forkParentID},
	}}, nil
}

func (m *mockForkCrud) ListAgentActivity(context.Context, entity.ListAgentActivityRequest) (*entity.ListAgentActivityResponse, error) {
	return &entity.ListAgentActivityResponse{}, nil
}

func (m *mockForkCrud) GetWorkspace(_ context.Context, req entity.GetWorkspaceRequest) (*entity.GetWorkspaceResponse, error) {
	return &entity.GetWorkspaceResponse{Workspace: entity.Workspace{ID: req.ID, Name: "api fork", ForkOfID: forkParentID}}, nil
}

type fakeMerger struct {
	req entity.MergeForkRequest
	err error
}

func (f *fakeMerger) Merge(_ context.Context, rq entity.MergeForkRequest) (*entity.MergeForkResponse, error) {
	f.req = rq
	if f.err != nil {
		return nil, f.err
	}
	return &entity.MergeForkResponse{ParentID: monoflake.ID(forkParentID).String(), MovedTasks: 3}, nil
}

func b62(id int64) string { return monoflake.ID(id).String() }

func TestForkWorkspace_ForksForTheCaller(t *testing.T) {
	c := &mockForkCrud{}
	s := &WorkspaceServer{crud: c, baseURL: "https://agentrq.example"}

	body := textOf(t, toolResult(s.handleForkWorkspace(authedContext(), nil, ForkWorkspaceParams{WorkspaceID: b62(forkParentID), Name: "  second pair of hands  "})))

	if c.forkReq.UserID != testUserID || c.forkReq.WorkspaceID != forkParentID || c.forkReq.Name != "second pair of hands" {
		t.Errorf("request = %+v", c.forkReq)
	}
	var got struct {
		Workspace struct {
			ID       string `json:"id"`
			ForkOfID string `json:"forkOfId"`
			Icon     string `json:"icon"`
			MCPURL   string `json:"mcpUrl"`
		} `json:"workspace"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if got.Workspace.ID != b62(forkID) || got.Workspace.ForkOfID != b62(forkParentID) || got.Workspace.Icon != "" ||
		got.Workspace.MCPURL != "https://agentrq.example/mcp/"+b62(forkID) {
		t.Errorf("workspace = %+v, from %s", got.Workspace, body)
	}
}

// The name is checked as REST checks it, and a refusal says why.
func TestForkWorkspace_Refusals(t *testing.T) {
	c := &mockForkCrud{}
	s := &WorkspaceServer{crud: c}
	r := toolResult(s.handleForkWorkspace(authedContext(), nil, ForkWorkspaceParams{WorkspaceID: b62(forkParentID), Name: strings.Repeat("n", 129)}))
	if !r.isError || !strings.Contains(r.text, "longer than 128") || c.forkReq.WorkspaceID != 0 {
		t.Errorf("long name: %+v, forked %+v", r, c.forkReq)
	}

	c.forkErr = entity.NewForkError(entity.ErrForkOfFork, "a fork cannot be forked")
	r = toolResult(s.handleForkWorkspace(authedContext(), nil, ForkWorkspaceParams{WorkspaceID: b62(forkID)}))
	if !r.isError || r.text != "a fork cannot be forked" {
		t.Errorf("fork of a fork: %+v", r)
	}
}

// mergeFork runs the merge REST runs, the one that stops the agent first.
func TestMergeFork_UsesTheSharedMerge(t *testing.T) {
	m := &fakeMerger{}
	s := &WorkspaceServer{forks: m}

	body := textOf(t, toolResult(s.handleMergeFork(authedContext(), nil, MergeForkParams{WorkspaceID: b62(forkID)})))

	if m.req.UserID != testUserID || m.req.WorkspaceID != forkID {
		t.Errorf("request = %+v", m.req)
	}
	if body != `{"parentId":"`+b62(forkParentID)+`","movedTasks":3}` {
		t.Errorf("body = %s", body)
	}
}

func TestMergeFork_DeleteFolder(t *testing.T) {
	m := &fakeMerger{}
	s := &WorkspaceServer{forks: m}
	toolResult(s.handleMergeFork(authedContext(), nil, MergeForkParams{WorkspaceID: b62(forkID), DeleteFolder: true}))
	if !m.req.DeleteFolder {
		t.Errorf("request = %+v, want DeleteFolder", m.req)
	}
}

func TestMergeFork_Refused(t *testing.T) {
	m := &fakeMerger{err: entity.NewForkError(entity.ErrForkUnfinished, "2 tasks in this fork are not finished")}
	r := toolResult((&WorkspaceServer{forks: m}).handleMergeFork(authedContext(), nil, MergeForkParams{WorkspaceID: b62(forkID)}))
	if !r.isError || !strings.Contains(r.text, "2 tasks in this fork are not finished") {
		t.Errorf("result = %+v", r)
	}
}

// An agent reading the workspaces can tell a fork from its parent.
func TestWorkspaceOutputCarriesForkOfID(t *testing.T) {
	s := &WorkspaceServer{crud: &mockForkCrud{}}
	list := textOf(t, toolResult(s.handleListWorkspaces(authedContext(), nil, ListWorkspacesParams{IncludeDetails: true})))
	if !strings.Contains(list, `"forkOfId":"`+b62(forkParentID)+`"`) || !strings.Contains(list, `"forkCount":1`) {
		t.Errorf("listWorkspaces = %s", list)
	}
	one := textOf(t, toolResult(s.handleGetWorkspace(authedContext(), nil, GetWorkspaceParams{ID: b62(forkID)})))
	if !strings.Contains(one, `"forkOfId":"`+b62(forkParentID)+`"`) {
		t.Errorf("getWorkspace = %s", one)
	}
}

// New hands the server the merge app.go shares with REST.
func TestNew_WiresTheForkMerger(t *testing.T) {
	m := &fakeMerger{}
	h, err := New(Params{Mux: http.NewServeMux(), ForkMerger: m})
	if err != nil {
		t.Fatal(err)
	}
	if got := h.(*handler).coremcpServer.forks; got != m {
		t.Errorf("forks = %v", got)
	}
}
