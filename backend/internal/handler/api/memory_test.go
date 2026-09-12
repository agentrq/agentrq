// Copyright 2026 Contextual, Inc. https://agentrq.com

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

type memoryCrud struct {
	crud.Controller
	listFunc func(ctx context.Context, req entity.ListMemoriesRequest) (*entity.ListMemoriesResponse, error)
	getFunc  func(ctx context.Context, req entity.GetMemoryRequest) (*entity.GetMemoryResponse, error)
	// What the handler actually asked for, so the test can check the request
	// was assembled from the path and the session rather than from the body.
	sawList entity.ListMemoriesRequest
	sawGet  entity.GetMemoryRequest
}

func (m *memoryCrud) ListMemories(ctx context.Context, req entity.ListMemoriesRequest) (*entity.ListMemoriesResponse, error) {
	m.sawList = req
	return m.listFunc(ctx, req)
}

func (m *memoryCrud) GetMemory(ctx context.Context, req entity.GetMemoryRequest) (*entity.GetMemoryResponse, error) {
	m.sawGet = req
	return m.getFunc(ctx, req)
}

var testMemoryWorkspaceID = monoflake.ID(4242).String()

func memoryApp(c crud.Controller) *fiber.App {
	h := &handler{crud: c}
	app := fiber.New()
	// The session's user, exactly as the auth middleware supplies it.
	app.Use(func(ctx *fiber.Ctx) error {
		ctx.Locals("user_id", "user-1")
		return ctx.Next()
	})
	app.Get("/workspaces/:id/memories", h.listWorkspaceMemories())
	app.Get("/workspaces/:id/memories/:name", h.getWorkspaceMemory())
	return app
}

func TestListWorkspaceMemories(t *testing.T) {
	now := time.Now()
	c := &memoryCrud{
		listFunc: func(context.Context, entity.ListMemoriesRequest) (*entity.ListMemoriesResponse, error) {
			return &entity.ListMemoriesResponse{Memories: []entity.Memory{
				{ID: 900, CreatedAt: now, UpdatedAt: now, WorkspaceID: 4242, Name: "MEMORY.md", SizeBytes: 7},
			}}, nil
		},
	}

	res, _ := memoryApp(c).Test(httptest.NewRequest(http.MethodGet, "/workspaces/"+testMemoryWorkspaceID+"/memories", nil))

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	var body struct {
		Memories []map[string]any `json:"memories"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Memories) != 1 {
		t.Fatalf("expected one memory, got %d", len(body.Memories))
	}
	m := body.Memories[0]
	if m["name"] != "MEMORY.md" || m["sizeBytes"] != float64(7) {
		t.Errorf("got %v", m)
	}
	// camelCase, and no content in the list.
	if _, present := m["content"]; present {
		t.Error("the list must not carry content")
	}
	for _, key := range []string{"createdAt", "updatedAt", "workspaceId", "sizeBytes"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing %q — the API is camelCase", key)
		}
	}
	// The workspace comes from the path and the user from the session, never
	// from anything the caller can put in a body.
	if c.sawList.WorkspaceID != 4242 || c.sawList.UserID != "user-1" {
		t.Errorf("controller saw %+v", c.sawList)
	}
}

func TestGetWorkspaceMemory(t *testing.T) {
	c := &memoryCrud{
		getFunc: func(context.Context, entity.GetMemoryRequest) (*entity.GetMemoryResponse, error) {
			return &entity.GetMemoryResponse{Memory: entity.Memory{
				ID: 900, WorkspaceID: 4242, Name: "deploys.md", Content: "how we ship", SizeBytes: 11,
			}}, nil
		},
	}

	res, _ := memoryApp(c).Test(httptest.NewRequest(http.MethodGet, "/workspaces/"+testMemoryWorkspaceID+"/memories/deploys.md", nil))

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	var body struct {
		Memory map[string]any `json:"memory"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Memory["content"] != "how we ship" {
		t.Errorf("content: got %v", body.Memory["content"])
	}
	if c.sawGet.Name != "deploys.md" {
		t.Errorf("controller asked for %q", c.sawGet.Name)
	}
}

// A name arrives percent-encoded in the path and must reach the controller as
// the name the agent actually saved under.
func TestGetWorkspaceMemory_DecodesTheName(t *testing.T) {
	c := &memoryCrud{
		getFunc: func(context.Context, entity.GetMemoryRequest) (*entity.GetMemoryResponse, error) {
			return &entity.GetMemoryResponse{Memory: entity.Memory{Name: "release notes.md"}}, nil
		},
	}

	res, _ := memoryApp(c).Test(httptest.NewRequest(http.MethodGet, "/workspaces/"+testMemoryWorkspaceID+"/memories/release%20notes.md", nil))

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if c.sawGet.Name != "release notes.md" {
		t.Errorf("controller asked for %q, want the decoded name", c.sawGet.Name)
	}
}

// A percent-escape that decodes to nothing usable stops at the edge rather than
// reaching the controller as a lookup for a name nobody could have saved.
//
// The request is built by hand: httptest.NewRequest parses the URL and refuses
// a malformed escape outright, so the only way to send one is to set the raw
// request line.
func TestGetWorkspaceMemory_RejectsANameItCannotDecode(t *testing.T) {
	c := &memoryCrud{}

	for _, raw := range []string{
		"/workspaces/" + testMemoryWorkspaceID + "/memories/%zz",
		"/workspaces/" + testMemoryWorkspaceID + "/memories/%",
	} {
		req := httptest.NewRequest(http.MethodGet, "/workspaces/"+testMemoryWorkspaceID+"/memories/placeholder", nil)
		req.RequestURI = raw
		req.URL.Path = raw
		req.URL.RawPath = raw

		res, err := memoryApp(c).Test(req)
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if res.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s: status = %d, want 422", raw, res.StatusCode)
		}
	}
}

func TestWorkspaceMemories_RejectAnUnreadableWorkspaceID(t *testing.T) {
	// The controller must not be reached at all, so its funcs stay nil: a call
	// would panic rather than quietly pass.
	c := &memoryCrud{}

	// "0" rather than something that merely looks wrong: base62 happily decodes
	// most junk into a real number, and only an ID that resolves to zero is
	// actually unreadable.
	for _, path := range []string{
		"/workspaces/0/memories",
		"/workspaces/0/memories/MEMORY.md",
	} {
		res, _ := memoryApp(c).Test(httptest.NewRequest(http.MethodGet, path, nil))
		if res.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s: status = %d, want 422", path, res.StatusCode)
		}
	}
}

// A workspace the caller does not own, and a memory that was never written,
// both come back as not-found — the controller answers the first that way so a
// stranger cannot tell whether the workspace exists.
func TestWorkspaceMemories_NotFound(t *testing.T) {
	c := &memoryCrud{
		listFunc: func(context.Context, entity.ListMemoriesRequest) (*entity.ListMemoriesResponse, error) {
			return nil, base.ErrNotFound
		},
		getFunc: func(context.Context, entity.GetMemoryRequest) (*entity.GetMemoryResponse, error) {
			return nil, base.ErrNotFound
		},
	}

	for _, path := range []string{
		"/workspaces/" + testMemoryWorkspaceID + "/memories",
		"/workspaces/" + testMemoryWorkspaceID + "/memories/gone.md",
	} {
		res, _ := memoryApp(c).Test(httptest.NewRequest(http.MethodGet, path, nil))
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, res.StatusCode)
		}
	}
}
