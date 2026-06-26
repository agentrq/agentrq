package coremcp

import (
	"context"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/mustafaturan/monoflake"
)

type attachmentCrud struct {
	crud.Controller
	got entity.GetAttachmentRequest
}

func (c *attachmentCrud) GetAttachment(ctx context.Context, req entity.GetAttachmentRequest) (*entity.GetAttachmentResponse, error) {
	c.got = req
	return &entity.GetAttachmentResponse{
		Data:     []byte("hello"),
		Filename: "hello.txt",
		MimeType: "text/plain",
	}, nil
}

func TestHandleGetAttachmentPassesTaskID(t *testing.T) {
	crudCtrl := &attachmentCrud{}
	srv := &WorkspaceServer{crud: crudCtrl}
	ctx := context.WithValue(context.Background(), "user_id", "user-1")

	_, _, err := srv.handleGetAttachment(ctx, nil, GetAttachmentParams{
		WorkspaceID:  monoflake.ID(1).String(),
		TaskID:       monoflake.ID(2).String(),
		AttachmentID: "att-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if crudCtrl.got.WorkspaceID != 1 {
		t.Fatalf("expected workspace ID 1, got %d", crudCtrl.got.WorkspaceID)
	}
	if crudCtrl.got.TaskID != 2 {
		t.Fatalf("expected task ID 2, got %d", crudCtrl.got.TaskID)
	}
	if crudCtrl.got.AttachmentID != "att-1" {
		t.Fatalf("expected attachment att-1, got %q", crudCtrl.got.AttachmentID)
	}
}
