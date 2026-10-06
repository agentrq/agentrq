// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/mustafaturan/monoflake"
	"reflect"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
	"github.com/golang/mock/gomock"
	"gorm.io/datatypes"
)

const filesURL = "https://agentrq.example/storage/artifacts"

// newPublicStore is a local store linked through the public file routes.
func newPublicStore(t *testing.T) storage.Service {
	t.Helper()
	s, err := storage.NewNestedPublic(t.TempDir(), filesURL)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSaveAttachments_LinksAndDropsCallerURLs(t *testing.T) {
	e := newTestController(t)
	e.idgen.EXPECT().NextID().Return(int64(62))
	atts := []entity.Attachment{
		{ID: "mine", Filename: "a.png", MimeType: "image/png", Data: "cG5n", URL: "https://evil.example/a"},
		// No data, so nothing is stored: the link the caller made up still goes.
		{ID: "ref", Filename: "b.png", URL: "https://evil.example/b"},
	}
	store := newPublicStore(t)
	SaveAttachments(store, e.idgen, 1, 2, atts)
	// A fresh server-made id, never the caller's.
	id := atts[0].ID
	if id != "00000000010" {
		t.Fatalf("id %q", id)
	}
	want := []entity.Attachment{
		{ID: id, Filename: "a.png", MimeType: "image/png", URL: filesURL + "/w-00000000001/00000000002/" + id},
		{ID: "ref", Filename: "b.png"},
	}
	if !reflect.DeepEqual(atts, want) {
		t.Fatalf("got %#v, want %#v", atts, want)
	}
	if raw, err := storage.LoadAttachment(store, 1, 2, id); err != nil || string(raw) != "png" {
		t.Errorf("stored under the task: %q, %v", raw, err)
	}
}

func TestCopyAttachments_KeepsThePublicLink(t *testing.T) {
	e := newTestController(t)
	c := e.controller.(*controller)
	store := newPublicStore(t)
	c.storage = store
	if err := store.Save(storage.AttachmentKey(1, 2, "src"), "cG5n"); err != nil {
		t.Fatal(err)
	}
	e.idgen.EXPECT().NextID().Return(int64(63))
	// Task 2's attachment is copied into the fork, task 3, in the same workspace.
	out, keys := c.copyAttachments(1, 2, 3, attJSON(t, entity.Attachment{ID: "src", Filename: "a.png", MimeType: "image/png", URL: "https://cdn.example.com/attachments/w-00000000001/00000000002/src"}))
	var got []entity.Attachment
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "00000000011" {
		t.Fatalf("got %#v", got)
	}
	key := "w-00000000001/00000000003/" + got[0].ID
	want := []entity.Attachment{{ID: got[0].ID, Filename: "a.png", MimeType: "image/png", URL: filesURL + "/" + key}}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(keys, []string{key}) {
		t.Fatalf("got %#v %v", got, keys)
	}
}

// GetPublicFile looks up nothing that is not a key the server makes, even
// when the handler in front of it has been bypassed.
func TestGetPublicFile(t *testing.T) {
	e := newTestController(t)
	c := e.controller.(*controller)
	atts, skills := newPublicStore(t), newPublicStore(t)
	c.storage, c.skillStorage = atts, skills
	attKey := storage.AttachmentKey(1, 2, monoflake.ID(3).String())
	skillKey := "u-00000000001/skill-00000000002/" + monoflake.ID(4).String()
	if err := atts.Save(attKey, "iVBORw0KGgoK"); err != nil { // a PNG signature
		t.Fatal(err)
	}
	if err := skills.Save(skillKey, "PGI+"); err != nil { // "<b>"
		t.Fatal(err)
	}
	ctx := context.Background()

	got, err := c.GetPublicFile(ctx, entity.GetPublicFileRequest{Kind: entity.PublicFileArtifacts, Key: attKey})
	if err != nil || got.ContentType != "image/png" {
		t.Fatalf("attachment: %+v, %v", got, err)
	}
	got, err = c.GetPublicFile(ctx, entity.GetPublicFileRequest{Kind: entity.PublicFileSkills, Key: skillKey})
	if err != nil || string(got.Data) != "<b>" || got.ContentType != "text/plain; charset=utf-8" {
		t.Fatalf("skill: %+v, %v", got, err)
	}

	for _, req := range []entity.GetPublicFileRequest{
		{Kind: "other", Key: attKey},
		{Kind: entity.PublicFileArtifacts, Key: skillKey},
		{Kind: entity.PublicFileSkills, Key: attKey},
		{Kind: entity.PublicFileArtifacts, Key: "../" + attKey},
		{Kind: entity.PublicFileArtifacts, Key: attKey + "/"},
		{Kind: entity.PublicFileArtifacts, Key: storage.AttachmentKey(1, 2, "0000000003")},
		{Kind: entity.PublicFileArtifacts, Key: storage.AttachmentKey(1, 2, "00000000003-abcdef")},
		// The right shape, but nothing there.
		{Kind: entity.PublicFileArtifacts, Key: storage.AttachmentKey(1, 2, monoflake.ID(5).String())},
	} {
		if _, err := c.GetPublicFile(ctx, req); !errors.Is(err, base.ErrNotFound) {
			t.Errorf("%+v: %v", req, err)
		}
	}
}

func TestGetAttachment_LinkOnlySkipsTheFile(t *testing.T) {
	e := newTestController(t)
	task := model.Task{ID: 10, WorkspaceID: 1, Messages: []model.Message{
		{ID: 1},
		{ID: 2, Attachments: datatypes.JSON(`not json`)},
		{ID: 3, Attachments: attJSON(t, entity.Attachment{ID: "other"}, entity.Attachment{ID: "a", Filename: "a.png", MimeType: "image/png", URL: "https://l/a"})},
	}}
	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil).Times(2)
	req := entity.GetAttachmentRequest{WorkspaceID: 1, TaskID: 10, AttachmentID: "a", UserID: testUserIDStr, LinkOnly: true}

	// No storage call is expected: gomock fails the test if one is made.
	got, err := e.controller.GetAttachment(context.Background(), req)
	if err != nil || got.URL != "https://l/a" || got.Filename != "a.png" || got.Data != nil {
		t.Fatalf("got %+v, %v", got, err)
	}

	// Asked for the content, it reads the file too.
	expectOwnWorkspace(e, 1)
	e.storage.EXPECT().LoadRaw(storage.AttachmentKey(1, 10, "a")).Return([]byte("png"), nil)
	req.LinkOnly = false
	if got, err := e.controller.GetAttachment(context.Background(), req); err != nil || string(got.Data) != "png" || got.URL != "https://l/a" {
		t.Fatalf("got %+v, %v", got, err)
	}
}
