// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package crud

import (
	"context"
	"errors"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/golang/mock/gomock"
)

func storedMemory(name, content string) model.Memory {
	now := time.Now()
	return model.Memory{
		ID: 900, CreatedAt: now, UpdatedAt: now,
		UserID: testUserID, WorkspaceID: 1, Name: name, Content: content,
	}
}

func TestListMemories_ReportsSizeWithoutTheContent(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), int64(1), testUserID).Return(true, nil)
	e.repo.EXPECT().ListMemoriesByWorkspace(gomock.Any(), testUserID, int64(1)).Return([]model.Memory{
		storedMemory("MEMORY.md", "# Index"),
		storedMemory("deploys.md", "how we ship"),
	}, nil)

	rs, err := e.controller.ListMemories(context.Background(), entity.ListMemoriesRequest{
		WorkspaceID: 1, UserID: testUserIDStr,
	})

	if err != nil {
		t.Fatalf("ListMemories: %v", err)
	}
	if len(rs.Memories) != 2 {
		t.Fatalf("expected 2 memories, got %d", len(rs.Memories))
	}
	for _, m := range rs.Memories {
		// A workspace's memories run to 16 KiB each; shipping them all to draw
		// a list of names would send the whole store to render an index.
		if m.Content != "" {
			t.Errorf("%s: the list must not carry content", m.Name)
		}
	}
	if rs.Memories[0].SizeBytes != len("# Index") {
		t.Errorf("size: got %d, want %d", rs.Memories[0].SizeBytes, len("# Index"))
	}
}

func TestGetMemory_ReturnsTheContent(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), int64(1), testUserID).Return(true, nil)
	e.repo.EXPECT().GetMemory(gomock.Any(), testUserID, int64(1), "MEMORY.md").Return(storedMemory("MEMORY.md", "# Index"), nil)

	rs, err := e.controller.GetMemory(context.Background(), entity.GetMemoryRequest{
		WorkspaceID: 1, UserID: testUserIDStr, Name: "MEMORY.md",
	})

	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	if rs.Memory.Content != "# Index" {
		t.Errorf("content: got %q", rs.Memory.Content)
	}
	if rs.Memory.SizeBytes != len("# Index") {
		t.Errorf("size: got %d", rs.Memory.SizeBytes)
	}
}

// A memory is an agent's working notes about somebody's private workspace, so a
// caller who does not own it must not be able to read one — and must not learn
// that it exists either, which is why this is not-found rather than forbidden.
func TestMemories_NotReadableByAnotherAccount(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(*testEnv) error
	}{
		{"list", func(e *testEnv) error {
			_, err := e.controller.ListMemories(context.Background(), entity.ListMemoriesRequest{WorkspaceID: 1, UserID: testUserIDStr})
			return err
		}},
		{"get", func(e *testEnv) error {
			_, err := e.controller.GetMemory(context.Background(), entity.GetMemoryRequest{WorkspaceID: 1, UserID: testUserIDStr, Name: "MEMORY.md"})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestController(t)
			e.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), int64(1), testUserID).Return(false, nil)
			// The repository must never be reached for a workspace the caller
			// does not own.

			err := tc.call(e)

			if !errors.Is(err, base.ErrNotFound) {
				t.Errorf("got %v, want ErrNotFound", err)
			}
		})
	}
}

func TestMemories_AccessCheckFailureIsReported(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), int64(1), testUserID).Return(false, errors.New("db down"))

	_, err := e.controller.ListMemories(context.Background(), entity.ListMemoriesRequest{WorkspaceID: 1, UserID: testUserIDStr})

	// A database that cannot answer must not read as "you may not see this".
	if err == nil || errors.Is(err, base.ErrNotFound) {
		t.Errorf("got %v, want the underlying failure", err)
	}
}

func TestMemories_RejectIncompleteRequests(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(*testEnv) error
	}{
		{"list with no workspace", func(e *testEnv) error {
			_, err := e.controller.ListMemories(context.Background(), entity.ListMemoriesRequest{UserID: testUserIDStr})
			return err
		}},
		{"list with no user", func(e *testEnv) error {
			_, err := e.controller.ListMemories(context.Background(), entity.ListMemoriesRequest{WorkspaceID: 1})
			return err
		}},
		{"get with no name", func(e *testEnv) error {
			_, err := e.controller.GetMemory(context.Background(), entity.GetMemoryRequest{WorkspaceID: 1, UserID: testUserIDStr})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestController(t)
			if tc.name == "get with no name" {
				e.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), int64(1), testUserID).Return(true, nil)
			}

			if err := tc.call(e); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestMemories_ReportStorageFailures(t *testing.T) {
	t.Run("list", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), int64(1), testUserID).Return(true, nil)
		e.repo.EXPECT().ListMemoriesByWorkspace(gomock.Any(), testUserID, int64(1)).Return(nil, errors.New("db down"))

		if _, err := e.controller.ListMemories(context.Background(), entity.ListMemoriesRequest{WorkspaceID: 1, UserID: testUserIDStr}); err == nil {
			t.Error("expected an error")
		}
	})

	t.Run("get", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), int64(1), testUserID).Return(true, nil)
		e.repo.EXPECT().GetMemory(gomock.Any(), testUserID, int64(1), "gone.md").Return(model.Memory{}, base.ErrNotFound)

		_, err := e.controller.GetMemory(context.Background(), entity.GetMemoryRequest{WorkspaceID: 1, UserID: testUserIDStr, Name: "gone.md"})

		// A name that was never written is a clean not-found, not an empty
		// success that would render as a memory with nothing in it.
		if !errors.Is(err, base.ErrNotFound) {
			t.Errorf("got %v, want ErrNotFound", err)
		}
	})
}
