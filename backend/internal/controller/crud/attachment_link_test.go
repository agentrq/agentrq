// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package crud

import (
	"encoding/json"
	"reflect"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

// publicStore is a local store that hands out links, as the public S3 store does.
type publicStore struct{ storage.Service }

func (s publicStore) SavePublic(id, dataBase64, contentType string) (string, error) {
	return "https://cdn.example.com/attachments/" + id + "?" + contentType, s.Save(id, dataBase64)
}

func newPublicStore(t *testing.T) publicStore {
	t.Helper()
	local, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return publicStore{local}
}

func TestSaveAttachments_LinksAndDropsCallerURLs(t *testing.T) {
	e := newTestController(t)
	e.idgen.EXPECT().NextID().Return(int64(62))
	atts := []entity.Attachment{
		{ID: "mine", Filename: "a.png", MimeType: "image/png", Data: "cG5n", URL: "https://evil.example/a"},
		// No data, so nothing is stored: the link the caller made up still goes.
		{ID: "ref", Filename: "b.png", URL: "https://evil.example/b"},
	}
	SaveAttachments(newPublicStore(t), e.idgen, atts)
	want := []entity.Attachment{
		{ID: "00000000010", Filename: "a.png", MimeType: "image/png", URL: "https://cdn.example.com/attachments/00000000010?image/png"},
		{ID: "ref", Filename: "b.png"},
	}
	if !reflect.DeepEqual(atts, want) {
		t.Fatalf("got %#v, want %#v", atts, want)
	}
}

func TestCopyAttachments_KeepsThePublicLink(t *testing.T) {
	e := newTestController(t)
	c := e.controller.(*controller)
	store := newPublicStore(t)
	c.storage = store
	if err := store.Save("src", "cG5n"); err != nil {
		t.Fatal(err)
	}
	e.idgen.EXPECT().NextID().Return(int64(63))
	out, ids := c.copyAttachments(attJSON(t, entity.Attachment{ID: "src", Filename: "a.png", MimeType: "image/png", URL: "https://cdn.example.com/attachments/src"}))
	var got []entity.Attachment
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	want := []entity.Attachment{{ID: "00000000011", Filename: "a.png", MimeType: "image/png", URL: "https://cdn.example.com/attachments/00000000011?image/png"}}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(ids, []string{"00000000011"}) {
		t.Fatalf("got %#v %v", got, ids)
	}
}
