// Copyright 2026 Contextual, Inc. https://agentrq.com

package api

import (
	"encoding/json"
	"net/url"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

func FromHTTPRequestToListMemoriesRequestEntity(c *fiber.Ctx) *entity.ListMemoriesRequest {
	workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
	if workspaceID == 0 {
		return nil
	}
	return &entity.ListMemoriesRequest{WorkspaceID: workspaceID}
}

func FromHTTPRequestToGetMemoryRequestEntity(c *fiber.Ctx) *entity.GetMemoryRequest {
	workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
	if workspaceID == 0 {
		return nil
	}
	// The name travels in the path, so it arrives percent-encoded — and Fiber
	// hands back the raw segment, so it has to be decoded here. A memory named
	// "release notes.md" would otherwise be looked up as "release%20notes.md"
	// and never found.
	name, err := url.PathUnescape(c.Params("name"))
	if err != nil {
		return nil
	}
	// An empty name is not checked here: the route cannot match one, so the
	// guard would be unreachable. The controller refuses it, which is where a
	// caller that reaches it another way is caught.
	return &entity.GetMemoryRequest{WorkspaceID: workspaceID, Name: name}
}

func FromListMemoriesResponseEntityToHTTPResponse(rs *entity.ListMemoriesResponse) []byte {
	memories := make([]view.Memory, len(rs.Memories))
	for i, m := range rs.Memories {
		memories[i] = fromEntityMemoryToView(m)
	}
	payload, _ := json.Marshal(view.ListMemoriesResponse{Memories: memories})
	return payload
}

func FromGetMemoryResponseEntityToHTTPResponse(rs *entity.GetMemoryResponse) []byte {
	payload, _ := json.Marshal(view.GetMemoryResponse{Memory: fromEntityMemoryToView(rs.Memory)})
	return payload
}

func fromEntityMemoryToView(m entity.Memory) view.Memory {
	return view.Memory{
		ID:          monoflake.ID(m.ID).String(),
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
		WorkspaceID: monoflake.ID(m.WorkspaceID).String(),
		Name:        m.Name,
		SizeBytes:   m.SizeBytes,
		Content:     m.Content,
	}
}
