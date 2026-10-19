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
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

type mockAttachmentCrud struct {
	crud.Controller
	got entity.GetAttachmentRequest
	res *entity.GetAttachmentResponse
	err error
}

func (m *mockAttachmentCrud) GetAttachment(_ context.Context, req entity.GetAttachmentRequest) (*entity.GetAttachmentResponse, error) {
	m.got = req
	return m.res, m.err
}

func TestGetAttachment_Formats(t *testing.T) {
	public := &entity.GetAttachmentResponse{Filename: "a.png", MimeType: "image/png", URL: "https://cdn/attachments/a"}
	withData := &entity.GetAttachmentResponse{Filename: "a.png", MimeType: "image/png", URL: "https://cdn/attachments/a", Data: []byte("png")}
	local := &entity.GetAttachmentResponse{Filename: "b.txt", MimeType: "text/plain", Data: []byte("txt")}
	for _, tc := range []struct {
		name, format string
		res          *entity.GetAttachmentResponse
		linkOnly     bool
		want         string
	}{
		{"link by default", "", public, true, `{"filename":"a.png","mimeType":"image/png","url":"https://cdn/attachments/a"}`},
		{"link when asked", "url", public, true, `{"filename":"a.png","mimeType":"image/png","url":"https://cdn/attachments/a"}`},
		{"content when asked", "base64", withData, false, `{"filename":"a.png","mimeType":"image/png","data":"cG5n"}`},
		{"content when there is no link", "", local, true, `{"filename":"b.txt","mimeType":"text/plain","data":"dHh0"}`},
		{"text when asked", "textContent", local, false, `{"filename":"b.txt","mimeType":"text/plain","textContent":"txt"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &mockAttachmentCrud{res: tc.res}
			got := textOf(t, toolResult((&WorkspaceServer{crud: m}).handleGetAttachment(authedContext(), nil, GetAttachmentParams{
				WorkspaceID: base62(testWorkspace), TaskID: base62(42), AttachmentID: "a", Format: tc.format,
			})))
			if got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
			if m.got.TaskID != 42 || m.got.WorkspaceID != testWorkspace || m.got.AttachmentID != "a" || m.got.LinkOnly != tc.linkOnly {
				t.Errorf("asked crud for %+v", m.got)
			}
		})
	}

	t.Run("bad format", func(t *testing.T) {
		got := toolResult((&WorkspaceServer{crud: &mockAttachmentCrud{}}).handleGetAttachment(authedContext(), nil, GetAttachmentParams{Format: "zip"}))
		if !got.isError || !strings.Contains(got.text, `"url", "base64" or "textContent"`) {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("text that is not UTF-8", func(t *testing.T) {
		m := &mockAttachmentCrud{res: &entity.GetAttachmentResponse{Filename: "a.png", Data: []byte{0xff, 0xfe}}}
		got := toolResult((&WorkspaceServer{crud: m}).handleGetAttachment(authedContext(), nil, GetAttachmentParams{Format: "textContent"}))
		if !got.isError || got.text != `attachment is not text; use format "base64"` {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("not found", func(t *testing.T) {
		got := toolResult((&WorkspaceServer{crud: &mockAttachmentCrud{err: errors.New("not found")}}).handleGetAttachment(authedContext(), nil, GetAttachmentParams{}))
		if !got.isError || got.text != "not found" {
			t.Errorf("got %+v", got)
		}
	})
}
