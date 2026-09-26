// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package api

import (
	"encoding/json"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

func TestFromEntityAttachmentsToView_KeepsTheLink(t *testing.T) {
	got := fromEntityAttachmentsToView([]entity.Attachment{
		{ID: "a", Filename: "a.png", MimeType: "image/png", URL: "https://cdn/attachments/a"},
		{ID: "b", Filename: "b.txt", MimeType: "text/plain"},
	})
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	// camelCase, and no url key at all for a local attachment.
	want := `[{"id":"a","filename":"a.png","mimeType":"image/png","data":"","url":"https://cdn/attachments/a"},{"id":"b","filename":"b.txt","mimeType":"text/plain","data":""}]`
	if string(b) != want {
		t.Errorf("got %s", b)
	}
}
