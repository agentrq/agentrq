// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package coremcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ── mock crud controller ──────────────────────────────────────────────────────
//
// Embedding the interface keeps the mock to the methods these tools call, the
// same convention mockEventCrud uses in events_test.go.

type mockMemoryCrud struct {
	crud.Controller

	listMemories func(ctx context.Context, req entity.ListMemoriesRequest) (*entity.ListMemoriesResponse, error)
	getMemory    func(ctx context.Context, req entity.GetMemoryRequest) (*entity.GetMemoryResponse, error)
}

func (m *mockMemoryCrud) ListMemories(ctx context.Context, req entity.ListMemoriesRequest) (*entity.ListMemoriesResponse, error) {
	return m.listMemories(ctx, req)
}

func (m *mockMemoryCrud) GetMemory(ctx context.Context, req entity.GetMemoryRequest) (*entity.GetMemoryResponse, error) {
	return m.getMemory(ctx, req)
}

func memoryServer(ctrl *mockMemoryCrud) *WorkspaceServer {
	return &WorkspaceServer{crud: ctrl}
}

func TestListMemories_ScopesToTheAuthenticatedUserAndWorkspace(t *testing.T) {
	var got entity.ListMemoriesRequest
	ctrl := &mockMemoryCrud{listMemories: func(_ context.Context, req entity.ListMemoriesRequest) (*entity.ListMemoriesResponse, error) {
		got = req
		return &entity.ListMemoriesResponse{Memories: []entity.Memory{
			{ID: 1, WorkspaceID: testWorkspace, Name: "MEMORY.md", SizeBytes: 42},
		}}, nil
	}}

	body := textOf(t, toolResult(memoryServer(ctrl).handleListMemories(authedContext(), nil, ListMemoriesParams{
		WorkspaceID: base62(testWorkspace),
	})))

	if got.UserID != testUserID {
		t.Errorf("UserID = %q, want %q", got.UserID, testUserID)
	}
	if got.WorkspaceID != testWorkspace {
		t.Errorf("WorkspaceID = %d, want %d", got.WorkspaceID, testWorkspace)
	}

	var payload struct {
		Memories []struct {
			Name      string `json:"name"`
			SizeBytes int    `json:"sizeBytes"`
			Content   string `json:"content"`
		} `json:"memories"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Memories) != 1 || payload.Memories[0].Name != "MEMORY.md" || payload.Memories[0].SizeBytes != 42 {
		t.Fatalf("memories = %+v", payload.Memories)
	}
}

func TestListMemories_ReportsAFailure(t *testing.T) {
	ctrl := &mockMemoryCrud{listMemories: func(context.Context, entity.ListMemoriesRequest) (*entity.ListMemoriesResponse, error) {
		return nil, errors.New("database unavailable")
	}}

	result := toolResult(memoryServer(ctrl).handleListMemories(authedContext(), nil, ListMemoriesParams{WorkspaceID: base62(testWorkspace)}))

	if !result.isError || result.text != "database unavailable" {
		t.Fatalf("result = %+v", result)
	}
}

func TestGetMemory_ReadsOneByNameAndReturnsItsContent(t *testing.T) {
	var got entity.GetMemoryRequest
	ctrl := &mockMemoryCrud{getMemory: func(_ context.Context, req entity.GetMemoryRequest) (*entity.GetMemoryResponse, error) {
		got = req
		return &entity.GetMemoryResponse{Memory: entity.Memory{
			WorkspaceID: req.WorkspaceID, Name: req.Name, Content: "# Index\n- deploys.md", SizeBytes: 21,
		}}, nil
	}}

	body := textOf(t, toolResult(memoryServer(ctrl).handleGetMemory(authedContext(), nil, GetMemoryParams{
		WorkspaceID: base62(testWorkspace), Name: "MEMORY.md",
	})))

	if got.UserID != testUserID || got.WorkspaceID != testWorkspace || got.Name != "MEMORY.md" {
		t.Fatalf("request = %+v", got)
	}

	var payload struct {
		Memory struct {
			Name    string `json:"name"`
			Content string `json:"content"`
		} `json:"memory"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Memory.Content != "# Index\n- deploys.md" {
		t.Errorf("content = %q", payload.Memory.Content)
	}
}

// A memory that does not exist, or a workspace the user cannot access, is the
// controller's call — the tool reports what it said rather than inventing its
// own answer (mirrors TestGetEvent_ReportsAMissingEvent).
func TestGetMemory_ReportsAMissingMemory(t *testing.T) {
	ctrl := &mockMemoryCrud{getMemory: func(context.Context, entity.GetMemoryRequest) (*entity.GetMemoryResponse, error) {
		return nil, errors.New("record not found")
	}}

	result := toolResult(memoryServer(ctrl).handleGetMemory(authedContext(), nil, GetMemoryParams{
		WorkspaceID: base62(testWorkspace), Name: "never-written.md",
	}))

	if !result.isError || result.text != "record not found" {
		t.Fatalf("result = %+v", result)
	}
}

func TestMemoryToolsAreRegistered(t *testing.T) {
	srv := NewServer(&mockMemoryCrud{}, "https://agentrq.example")

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	defer serverSession.Close()

	clientSession, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).
		Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	defer clientSession.Close()

	listed, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	registered := make(map[string]string, len(listed.Tools))
	for _, tool := range listed.Tools {
		registered[tool.Name] = tool.Description
	}

	for _, name := range []string{"listMemories", "getMemory"} {
		description, ok := registered[name]
		if !ok {
			t.Errorf("tool %q is not registered", name)
			continue
		}
		if description == "" {
			t.Errorf("tool %q has no description", name)
		}
	}
}
