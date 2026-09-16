// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	mock_idgen "github.com/agentrq/agentrq/backend/internal/service/mocks/idgen"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/golang/mock/gomock"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCreateTaskAttachmentSchema(t *testing.T) {
	ctx := context.Background()
	ps := &WorkspaceServer{}
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "createTask"}, ps.handleCreateTask)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	listed, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 1 {
		t.Fatalf("expected one tool, got %d", len(listed.Tools))
	}
	raw, err := json.Marshal(listed.Tools[0].InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]struct {
			Items struct {
				Type       string `json:"type"`
				Properties map[string]struct {
					Type string `json:"type"`
				} `json:"properties"`
			} `json:"items"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("attachment items must have an object schema: %v", err)
	}
	items := schema.Properties["attachments"].Items
	if items.Type != "object" {
		t.Fatalf("attachment items type = %q, want object", items.Type)
	}
	for _, field := range []string{"id", "filename", "mimeType", "data"} {
		if items.Properties[field].Type != "string" {
			t.Errorf("attachment field %s must have a string schema", field)
		}
	}
}

func TestCreateTaskPreservesAttachments(t *testing.T) {
	for _, input := range []string{
		`{"title":"test","body":"test"}`,
		`{"title":"test","body":"test","attachments":[{"id":"att-1","filename":"hello.txt","mimeType":"text/plain","data":"aGVsbG8K"}]}`,
	} {
		t.Run(input, func(t *testing.T) {
			var params CreateTaskParams
			if err := json.Unmarshal([]byte(input), &params); err != nil {
				t.Fatal(err)
			}
			ctrl := gomock.NewController(t)
			ids := mock_idgen.NewMockService(ctrl)
			ids.EXPECT().NextID().Return(int64(123))
			psub := mock_pubsub.NewMockService(ctrl)
			psub.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil)
			var stored model.Task
			ps := &WorkspaceServer{
				idgen: ids, bus: eventbus.New(), pubsub: psub,
				createTask: func(_ context.Context, task model.Task) (model.Task, error) {
					stored = task
					return task, nil
				},
			}
			result, _, err := ps.handleCreateTask(context.Background(), nil, params)
			if err != nil || result.IsError {
				t.Fatalf("createTask failed: %v, %v", result, err)
			}
			var got []entity.Attachment
			if len(stored.Attachments) > 0 {
				if err := json.Unmarshal(stored.Attachments, &got); err != nil {
					t.Fatal(err)
				}
			}
			if !reflect.DeepEqual(got, params.Attachments) {
				t.Fatalf("stored attachments = %#v, want %#v", got, params.Attachments)
			}
		})
	}
}
