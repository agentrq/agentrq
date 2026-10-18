// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"slices"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
	"github.com/mustafaturan/monoflake"
)

const (
	forkParentID = int64(100)
	forkID       = int64(200)
)

func TestContentIDIsTheWorkspaceUnlessGiven(t *testing.T) {
	if got := (&WorkspaceServer{workspaceID: forkParentID}).contentID(); got != forkParentID {
		t.Errorf("a workspace: %d, want its own %d", got, forkParentID)
	}
	if got := (&WorkspaceServer{workspaceID: forkID, contentWorkspaceID: forkParentID}).contentID(); got != forkParentID {
		t.Errorf("a fork: %d, want its parent %d", got, forkParentID)
	}
	ps := NewWorkspaceServer(forkID, forkParentID, "1", "http://localhost",
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, "", "", "", nil, nil, nil, nil)
	if got := ps.contentID(); got != forkParentID {
		t.Errorf("built: %d, want %d", got, forkParentID)
	}
}

// A fork's agent files what it attaches under the parent, and opens a file
// the task brought with it from the parent. Review Focus 3.
func TestAForksAttachmentsAreFiledUnderItsParent(t *testing.T) {
	store, err := storage.NewNested(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var created model.Task
	ps := &WorkspaceServer{
		workspaceID:        forkID,
		contentWorkspaceID: forkParentID,
		userID:             monoflake.ID(7).String(),
		storage:            store,
		idgen:              &fakeIDs{},
		bus:                eventbus.New(),
		createTask: func(_ context.Context, task model.Task) (model.Task, error) {
			created = task
			return task, nil
		},
		getTask: func(context.Context, int64) (model.Task, error) { return created, nil },
	}

	res, _, err := ps.handleCreateTask(context.Background(), nil, CreateTaskParams{
		Title: "T", Body: "B",
		Attachments: []AttachmentParam{{Filename: "log.txt", MimeType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte("the log"))}},
	})
	if err != nil || res.IsError {
		t.Fatalf("createTask: %+v, %v", res, err)
	}
	var atts []struct{ ID string }
	if err := json.Unmarshal(created.Attachments, &atts); err != nil || len(atts) != 1 {
		t.Fatalf("attachments %s: %v", created.Attachments, err)
	}
	if _, err := store.LoadRaw(storage.AttachmentKey(forkParentID, created.ID, atts[0].ID)); err != nil {
		t.Fatalf("not filed under the parent: %v", err)
	}

	res, _, _ = ps.handleGetAttachment(context.Background(), nil, GetAttachmentParams{
		TaskID: monoflake.ID(created.ID).String(), AttachmentID: atts[0].ID, Format: AttachmentFormatBase64,
	})
	var got AttachmentView
	if res.IsError || json.Unmarshal([]byte(resultText(t, res)), &got) != nil || string(got.Data) != "the log" {
		t.Fatalf("getAttachment: %s", resultText(t, res))
	}
}

// A fork's agent sees the sites shared with its parent, and an "always
// allow" it gives is remembered there.
func TestAForkUsesItsParentsSiteShares(t *testing.T) {
	backend := &fakeSiteTools{shares: []SiteShareView{githubShare()}, text: "starred"}
	s := newSiteServer(t, backend, "accept", map[string]any{"decision": "always"})
	s.workspaceID, s.contentWorkspaceID = forkID, forkParentID

	res, _, _ := s.handleListSiteTools(context.Background(), nil, ListSiteToolsParams{})
	if res.IsError {
		t.Fatalf("listSiteTools: %s", resultText(t, res))
	}
	if _, isErr := getSiteTool(t, s, GetSiteToolDefinitionParams{Site: siteGitHub, Tool: "search"}); isErr {
		t.Fatal("getSiteToolDefinition failed")
	}
	if text, isErr := callSite(t, s, CallSiteToolParams{TaskID: siteTask, Site: siteGitHub, Tool: "star"}); isErr {
		t.Fatalf("callSiteTool: %s", text)
	}

	if len(backend.workspaces) == 0 || slices.ContainsFunc(backend.workspaces, func(id int64) bool { return id != forkParentID }) {
		t.Fatalf("asked for workspaces %v, want only the parent %d", backend.workspaces, forkParentID)
	}
}
