// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"testing"

	"github.com/golang/mock/gomock"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mock_idgen "github.com/agentrq/agentrq/backend/internal/service/mocks/idgen"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	mock_repo "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	mock_storage "github.com/agentrq/agentrq/backend/internal/service/mocks/storage"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

const (
	resetOldID  = int64(1)
	resetNewID  = int64(99)
	resetForkID = int64(5)
)

type resetEnv struct {
	controller Controller
	repo       *mock_repo.MockRepository
	storage    *mock_storage.MockService
	events     *[]entity.CRUDEvent
}

func newResetEnv(t *testing.T) *resetEnv {
	t.Helper()
	ctrl := gomock.NewController(t)
	repo := mock_repo.NewMockRepository(ctrl)
	idgen := mock_idgen.NewMockService(ctrl)
	stor := mock_storage.NewMockService(ctrl)
	psSvc := mock_pubsub.NewMockService(ctrl)
	events := &[]entity.CRUDEvent{}
	psSvc.EXPECT().Publish(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, req pubsub.PublishRequest) (*pubsub.PublishResponse, error) {
			if e, ok := req.Event.(entity.CRUDEvent); ok {
				*events = append(*events, e)
			}
			return &pubsub.PublishResponse{}, nil
		}).AnyTimes()
	idgen.EXPECT().NextID().Return(resetNewID).AnyTimes()
	return &resetEnv{
		controller: New(Params{IDGen: idgen, Repository: repo, Storage: stor, PubSub: psSvc}),
		repo:       repo,
		storage:    stor,
		events:     events,
	}
}

func (e *resetEnv) reset() (*entity.ResetWorkspaceIDResponse, error) {
	return e.controller.ResetWorkspaceID(context.Background(), entity.ResetWorkspaceIDRequest{ID: resetOldID, UserID: testUserIDStr})
}

func (e *resetEnv) expectNoEvent(t *testing.T) {
	t.Helper()
	if len(*e.events) != 0 {
		t.Errorf("a failed reset published %+v, want nothing", *e.events)
	}
}

var (
	errStorage = errors.New("S3 unavailable")
	errDB      = errors.New("database unavailable")
	fileData   = []byte("png bytes")
	fileBase64 = base64.StdEncoding.EncodeToString(fileData)
)

func TestResetWorkspaceID_MovesTheWorkspaceAndTheFilesKeptUnderIt(t *testing.T) {
	e := newResetEnv(t)
	e.repo.EXPECT().GetWorkspace(gomock.Any(), resetOldID, testUserID).Return(activeWorkspace(), nil)
	e.repo.EXPECT().ListForks(gomock.Any(), resetOldID, testUserID).Return([]model.Workspace{{ID: resetForkID, ForkOfID: resetOldID}}, nil)
	e.repo.EXPECT().GetWorkspaceAttachments(gomock.Any(), resetOldID).Return([]entity.TaskAttachment{
		{TaskID: 2, ID: "a1", MimeType: "image/png"},
		{TaskID: 3, ID: "legacy", MimeType: "image/png"},
	}, nil)
	// A fork's task keeps its files under the parent.
	e.repo.EXPECT().GetWorkspaceAttachments(gomock.Any(), resetForkID).Return([]entity.TaskAttachment{{TaskID: 6, ID: "a3", MimeType: "image/png"}}, nil)
	gomock.InOrder(
		e.storage.EXPECT().LoadRaw(storage.AttachmentKey(resetOldID, 2, "a1")).Return(fileData, nil),
		e.storage.EXPECT().Save(storage.AttachmentKey(resetNewID, 2, "a1"), fileBase64).Return(nil),
		// Kept under its bare id, which the new id finds just the same.
		e.storage.EXPECT().LoadRaw(storage.AttachmentKey(resetOldID, 3, "legacy")).Return(nil, errors.New("attachment not found")),
		e.storage.EXPECT().LoadRaw(storage.AttachmentKey(resetOldID, 6, "a3")).Return(fileData, nil),
		e.storage.EXPECT().Save(storage.AttachmentKey(resetNewID, 6, "a3"), fileBase64).Return(nil),
		e.repo.EXPECT().ResetWorkspaceID(gomock.Any(), resetOldID, resetNewID, testUserID).Return(nil),
		e.storage.EXPECT().Delete(storage.AttachmentKey(resetOldID, 2, "a1")).Return(nil),
		e.storage.EXPECT().Delete(storage.AttachmentKey(resetOldID, 6, "a3")).Return(nil),
	)

	rs, err := e.reset()
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if rs.ID != resetNewID || !reflect.DeepEqual(rs.ForkIDs, []int64{resetForkID}) {
		t.Errorf("the reset answered id %d and forks %v, want id %d and forks [%d]", rs.ID, rs.ForkIDs, resetNewID, resetForkID)
	}
	if len(*e.events) != 1 {
		t.Fatalf("the reset published %d events, want exactly 1: %+v", len(*e.events), *e.events)
	}
	ev := (*e.events)[0]
	want := entity.CRUDEvent{Action: entity.ActionWorkspaceResetID, WorkspaceID: resetNewID, UserID: testUserID,
		ResourceType: entity.ResourceWorkspace, ResourceID: resetNewID, Actor: entity.ActorHuman, Origin: entity.OriginAPI}
	if ev != want {
		t.Errorf("the reset published %+v, want %+v", ev, want)
	}
}

func TestResetWorkspaceID_AForkMovesNoFiles(t *testing.T) {
	e := newResetEnv(t)
	e.repo.EXPECT().GetWorkspace(gomock.Any(), resetOldID, testUserID).Return(model.Workspace{ID: resetOldID, UserID: testUserID, ForkOfID: 7}, nil)
	e.repo.EXPECT().ResetWorkspaceID(gomock.Any(), resetOldID, resetNewID, testUserID).Return(nil)

	rs, err := e.reset()
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if rs.ID != resetNewID || len(rs.ForkIDs) != 0 {
		t.Errorf("the reset answered id %d and forks %v, want id %d and no forks", rs.ID, rs.ForkIDs, resetNewID)
	}
}

func TestResetWorkspaceID_AWorkspaceNotFoundIsReturned(t *testing.T) {
	e := newResetEnv(t)
	notFound := errors.New("workspace not found")
	e.repo.EXPECT().GetWorkspace(gomock.Any(), resetOldID, testUserID).Return(model.Workspace{}, notFound)

	if _, err := e.reset(); !errors.Is(err, notFound) {
		t.Fatalf("the reset returned %v, want %v", err, notFound)
	}
	e.expectNoEvent(t)
}

func TestResetWorkspaceID_ForksThatCannotBeListedStopTheReset(t *testing.T) {
	e := newResetEnv(t)
	e.repo.EXPECT().GetWorkspace(gomock.Any(), resetOldID, testUserID).Return(activeWorkspace(), nil)
	e.repo.EXPECT().ListForks(gomock.Any(), resetOldID, testUserID).Return(nil, errDB)

	if _, err := e.reset(); !errors.Is(err, errDB) {
		t.Fatalf("the reset returned %v, want %v", err, errDB)
	}
	e.expectNoEvent(t)
}

func TestResetWorkspaceID_AttachmentsThatCannotBeListedRemoveTheCopies(t *testing.T) {
	e := newResetEnv(t)
	e.repo.EXPECT().GetWorkspace(gomock.Any(), resetOldID, testUserID).Return(activeWorkspace(), nil)
	e.repo.EXPECT().ListForks(gomock.Any(), resetOldID, testUserID).Return([]model.Workspace{{ID: resetForkID}}, nil)
	e.repo.EXPECT().GetWorkspaceAttachments(gomock.Any(), resetOldID).Return([]entity.TaskAttachment{{TaskID: 2, ID: "a1"}}, nil)
	e.repo.EXPECT().GetWorkspaceAttachments(gomock.Any(), resetForkID).Return(nil, errDB)
	e.storage.EXPECT().LoadRaw(storage.AttachmentKey(resetOldID, 2, "a1")).Return(fileData, nil)
	e.storage.EXPECT().Save(storage.AttachmentKey(resetNewID, 2, "a1"), fileBase64).Return(nil)
	e.storage.EXPECT().Delete(storage.AttachmentKey(resetNewID, 2, "a1")).Return(nil)

	if _, err := e.reset(); !errors.Is(err, errDB) {
		t.Fatalf("the reset returned %v, want %v", err, errDB)
	}
	e.expectNoEvent(t)
}

func TestResetWorkspaceID_AFileThatCannotBeCopiedRemovesTheCopies(t *testing.T) {
	e := newResetEnv(t)
	e.repo.EXPECT().GetWorkspace(gomock.Any(), resetOldID, testUserID).Return(activeWorkspace(), nil)
	e.repo.EXPECT().ListForks(gomock.Any(), resetOldID, testUserID).Return(nil, nil)
	e.repo.EXPECT().GetWorkspaceAttachments(gomock.Any(), resetOldID).Return([]entity.TaskAttachment{{TaskID: 2, ID: "a1"}, {TaskID: 3, ID: "a2"}}, nil)
	e.storage.EXPECT().LoadRaw(storage.AttachmentKey(resetOldID, 2, "a1")).Return(fileData, nil)
	e.storage.EXPECT().Save(storage.AttachmentKey(resetNewID, 2, "a1"), fileBase64).Return(nil)
	e.storage.EXPECT().LoadRaw(storage.AttachmentKey(resetOldID, 3, "a2")).Return(fileData, nil)
	e.storage.EXPECT().Save(storage.AttachmentKey(resetNewID, 3, "a2"), fileBase64).Return(errStorage)
	e.storage.EXPECT().Delete(storage.AttachmentKey(resetNewID, 2, "a1")).Return(nil)

	if _, err := e.reset(); !errors.Is(err, errStorage) {
		t.Fatalf("the reset returned %v, want %v", err, errStorage)
	}
	e.expectNoEvent(t)
}

func TestResetWorkspaceID_AFailedMoveRemovesTheCopiesAndKeepsTheOriginals(t *testing.T) {
	e := newResetEnv(t)
	e.repo.EXPECT().GetWorkspace(gomock.Any(), resetOldID, testUserID).Return(activeWorkspace(), nil)
	e.repo.EXPECT().ListForks(gomock.Any(), resetOldID, testUserID).Return(nil, nil)
	e.repo.EXPECT().GetWorkspaceAttachments(gomock.Any(), resetOldID).Return([]entity.TaskAttachment{{TaskID: 2, ID: "a1"}}, nil)
	e.storage.EXPECT().LoadRaw(storage.AttachmentKey(resetOldID, 2, "a1")).Return(fileData, nil)
	e.storage.EXPECT().Save(storage.AttachmentKey(resetNewID, 2, "a1"), fileBase64).Return(nil)
	e.repo.EXPECT().ResetWorkspaceID(gomock.Any(), resetOldID, resetNewID, testUserID).Return(errDB)
	e.storage.EXPECT().Delete(storage.AttachmentKey(resetNewID, 2, "a1")).Return(nil)

	if _, err := e.reset(); !errors.Is(err, errDB) {
		t.Fatalf("the reset returned %v, want %v", err, errDB)
	}
	e.expectNoEvent(t)
}
