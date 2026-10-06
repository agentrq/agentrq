// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/skill"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
	"github.com/glebarez/sqlite"
	"github.com/golang/mock/gomock"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// expectOwnWorkspace answers the lookup of a workspace that is not a fork.
func expectOwnWorkspace(e *testEnv, id int64) {
	e.repo.EXPECT().SystemGetWorkspace(gomock.Any(), id).Return(model.Workspace{ID: id, UserID: testUserID}, nil)
}

func TestContentWorkspaceID(t *testing.T) {
	for _, tc := range []struct {
		name string
		ws   model.Workspace
		err  error
		want int64
		fail error
	}{
		{name: "a workspace is its own", ws: parentWorkspace(), want: fkParent},
		{name: "a fork is its parent's", ws: forkWorkspace(), want: fkParent},
		{name: "another account's is not found", ws: model.Workspace{ID: fkFork, UserID: testUserID + 1, ForkOfID: fkParent}, fail: base.ErrNotFound},
		{name: "a failed lookup is reported", err: errors.New("database unavailable"), fail: errors.New("database unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestController(t)
			e.repo.EXPECT().SystemGetWorkspace(gomock.Any(), fkFork).Return(tc.ws, tc.err)

			got, err := e.controller.(*controller).ContentWorkspaceID(context.Background(), fkFork, testUserID)

			if tc.fail != nil {
				if err == nil || err.Error() != tc.fail.Error() {
					t.Fatalf("err = %v, want %v", err, tc.fail)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %d, %v; want %d", got, err, tc.want)
			}
		})
	}
}

// The settings screen of a fork shows its parent's memories: a fork has none
// of its own.
func TestAForkListsItsParentsMemories(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), fkFork, testUserID).Return(true, nil).Times(2)
	e.repo.EXPECT().SystemGetWorkspace(gomock.Any(), fkFork).Return(forkWorkspace(), nil).Times(2)
	e.repo.EXPECT().ListMemoriesByWorkspace(gomock.Any(), testUserID, fkParent).Return([]model.Memory{storedMemory("MEMORY.md", "# Index")}, nil)
	e.repo.EXPECT().GetMemory(gomock.Any(), testUserID, fkParent, "MEMORY.md").Return(storedMemory("MEMORY.md", "# Index"), nil)

	list, err := e.controller.ListMemories(context.Background(), entity.ListMemoriesRequest{WorkspaceID: fkFork, UserID: testUserIDStr})
	if err != nil || len(list.Memories) != 1 {
		t.Fatalf("list: %+v, %v", list, err)
	}
	got, err := e.controller.GetMemory(context.Background(), entity.GetMemoryRequest{WorkspaceID: fkFork, UserID: testUserIDStr, Name: "MEMORY.md"})
	if err != nil || got.Memory.Content != "# Index" {
		t.Fatalf("get: %+v, %v", got, err)
	}
}

func TestMemories_ForkLookupFailureIsReported(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), fkFork, testUserID).Return(true, nil)
	e.repo.EXPECT().SystemGetWorkspace(gomock.Any(), fkFork).Return(model.Workspace{}, errors.New("database unavailable"))

	_, err := e.controller.ListMemories(context.Background(), entity.ListMemoriesRequest{WorkspaceID: fkFork, UserID: testUserIDStr})

	if err == nil || !strings.Contains(err.Error(), "database unavailable") {
		t.Fatalf("err = %v, want the lookup failure", err)
	}
}

const skFork = int64(1004) // a fork of skWS

func newForkSkillEnv(t *testing.T) *skillEnv {
	t.Helper()
	e := newSkillEnv(t)
	if err := e.db.Create(&model.Workspace{ID: skFork, UserID: skUser, Name: "alpha fork", ForkOfID: skWS}).Error; err != nil {
		t.Fatal(err)
	}
	return e
}

// A fork's skills are its parent's: one saved from the fork is the parent's,
// and the parent's are the fork's.
func TestAForkSharesItsParentsSkills(t *testing.T) {
	e := newForkSkillEnv(t)
	e.save(t, skWS, "deploy", skill.FileName, md("deploy", "how we ship"))
	saved := e.save(t, skFork, "review", skill.FileName, md("review", "how we review"))
	if !slices.Equal(saved.Skill.WorkspaceIDs, []int64{skWS}) {
		t.Fatalf("a skill saved in a fork is on in %v, want the parent %d", saved.Skill.WorkspaceIDs, skWS)
	}

	for _, ws := range []int64{skWS, skFork} {
		rs, err := e.c.SearchSkills(e.ctx, entity.SearchSkillsRequest{WorkspaceID: ws, UserID: skUserStr})
		if err != nil {
			t.Fatal(err)
		}
		if rs.Total != 2 {
			t.Fatalf("workspace %d sees %d skills, want both", ws, rs.Total)
		}
		for _, s := range rs.Skills {
			if !s.WorkspaceEnabled {
				t.Errorf("workspace %d sees %s as off there, want it on", ws, s.Name)
			}
		}
	}
	got, err := e.c.GetSkillFile(e.ctx, entity.GetSkillFileRequest{WorkspaceID: skFork, UserID: skUserStr, Name: "deploy", Path: skill.FileName})
	if err != nil || !strings.Contains(got.File.Content, "how we ship") {
		t.Fatalf("the fork reads its parent's skill: %+v, %v", got, err)
	}
	if err := e.c.DeleteSkill(e.ctx, entity.DeleteSkillRequest{WorkspaceID: skFork, UserID: skUserStr, Name: "deploy"}); err != nil {
		t.Fatalf("the fork deletes its parent's skill: %v", err)
	}
	if _, err := e.c.GetSkill(e.ctx, entity.GetSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "deploy"}); !errors.Is(err, base.ErrNotFound) {
		t.Fatalf("deleted from the fork, the parent still has it: %v", err)
	}
}

// A skill turned on in a fork is turned on in its parent, where the fork
// reads it; importing into a fork does the same.
func TestASkillTurnedOnInAForkIsOnInItsParent(t *testing.T) {
	e := newForkSkillEnv(t)
	e.save(t, skWS2, "lint", skill.FileName, md("lint", "lint rules"))

	rs, err := e.c.SetSkillEnabled(e.ctx, entity.SetSkillEnabledRequest{WorkspaceID: skFork, UserID: skUserStr, Name: "lint", Enabled: true})
	if err != nil || !rs.Skill.WorkspaceEnabled || !slices.Equal(rs.Skill.WorkspaceIDs, []int64{skWS2, skWS}) {
		t.Fatalf("turning lint on in the fork returned %+v, %v; want it on in beta and the parent", rs, err)
	}
	got, err := e.c.GetSkill(e.ctx, entity.GetSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "lint"})
	if err != nil || !got.Skill.WorkspaceEnabled {
		t.Fatalf("the parent reads %+v, %v; want lint on there", got, err)
	}

	e.importer.res = githubResult(importedSkill("deploy", "How we ship."))
	imp, err := e.c.ImportSkills(e.ctx, entity.ImportSkillsRequest{UserID: skUserStr, URL: "u", WorkspaceIDs: []int64{skFork, skWS}})
	if err != nil || len(imp.Imported) != 1 || !slices.Equal(imp.Imported[0].WorkspaceIDs, []int64{skWS}) {
		t.Fatalf("importing into the fork and its parent returned %+v, %v; want deploy on in the parent once", imp, err)
	}
}

// forkTaskEnv is a controller on a real database and a real attachment store,
// with a parent workspace and one fork of it.
type forkTaskEnv struct {
	c     *controller
	db    *gorm.DB
	store storage.Service
	ctx   context.Context
}

func newForkTaskEnv(t *testing.T) *forkTaskEnv {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Workspace{}, &model.Task{}, &model.Message{}, &model.ToolCall{},
		&model.SlackTaskThread{}, &model.EventTrigger{}, &model.WorkflowStep{}, &model.TaskStateTransition{},
		&model.TaskLatency{}, &model.Skill{}, &model.SkillFile{}, &model.WorkspaceSkill{}, &model.SiteShare{}, &model.ForkFolder{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, w := range []model.Workspace{
		{ID: fkParent, CreatedAt: now, UserID: testUserID, Name: "api"},
		{ID: fkFork, CreatedAt: now, UserID: testUserID, Name: "api fork", ForkOfID: fkParent},
	} {
		if err := db.Create(&w).Error; err != nil {
			t.Fatal(err)
		}
	}
	store, err := storage.NewNested(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := &controller{repository: base.New(&realDB{db: db}), idgen: &seqIDGen{n: 9000}, storage: store}
	return &forkTaskEnv{c: c, db: db, store: store, ctx: context.Background()}
}

func (e *forkTaskEnv) attachment(t *testing.T, ws, taskID int64, id string) []byte {
	t.Helper()
	rs, err := e.c.GetAttachment(e.ctx, entity.GetAttachmentRequest{WorkspaceID: ws, TaskID: taskID, AttachmentID: id, UserID: testUserIDStr})
	if err != nil {
		t.Fatalf("attachment %s from workspace %d: %v", id, ws, err)
	}
	return rs.Data
}

func file(name, content string) entity.Attachment {
	return entity.Attachment{Filename: name, MimeType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte(content))}
}

// An attachment has one home, under the parent, so it opens from the fork a
// task is moved into, and from the parent again once the fork is merged —
// including one added while the task was in the fork. Its public link is the
// same the whole way. Review Focus 3.
func TestAnAttachmentOpensInAForkAndAfterTheMerge(t *testing.T) {
	e := newForkTaskEnv(t)
	created, err := e.c.CreateTask(e.ctx, entity.CreateTaskRequest{UserID: testUserIDStr, Task: entity.Task{
		WorkspaceID: fkParent, Title: "fix it", Body: "the bug", Assignee: "agent", CreatedBy: "human",
		Attachments: []entity.Attachment{file("spec.txt", "the spec")},
	}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	task := created.Task
	spec := task.Attachments[0].ID
	if _, err := e.store.LoadRaw(storage.AttachmentKey(fkParent, task.ID, spec)); err != nil {
		t.Fatalf("filed under the parent: %v", err)
	}

	if _, err := e.c.MoveTask(e.ctx, entity.MoveTaskRequest{WorkspaceID: fkParent, TaskID: task.ID, DestinationWorkspaceID: fkFork, UserID: testUserIDStr}); err != nil {
		t.Fatalf("move into the fork: %v", err)
	}
	if got := e.attachment(t, fkFork, task.ID, spec); string(got) != "the spec" {
		t.Fatalf("from the fork: %q", got)
	}

	replied, err := e.c.ReplyToTask(e.ctx, entity.ReplyToTaskRequest{WorkspaceID: fkFork, TaskID: task.ID, UserID: testUserIDStr, Text: "a log", Attachments: []entity.Attachment{file("log.txt", "the log")}})
	if err != nil {
		t.Fatalf("reply in the fork: %v", err)
	}
	var log string
	for _, m := range replied.Task.Messages {
		for _, a := range m.Attachments {
			log = a.ID
		}
	}
	if _, err := e.store.LoadRaw(storage.AttachmentKey(fkParent, task.ID, log)); err != nil {
		t.Fatalf("one added in the fork is filed under the parent: %v", err)
	}

	if err := e.db.Model(&model.Task{}).Where("id = ?", task.ID).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.MergeFork(e.ctx, entity.MergeForkRequest{WorkspaceID: fkFork, UserID: testUserIDStr}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	for id, want := range map[string]string{spec: "the spec", log: "the log"} {
		if got := e.attachment(t, fkParent, task.ID, id); string(got) != want {
			t.Errorf("%s from the parent after the merge: %q, want %q", id, got, want)
		}
		pub, err := e.c.GetPublicFile(e.ctx, entity.GetPublicFileRequest{Kind: entity.PublicFileArtifacts, Key: storage.AttachmentKey(fkParent, task.ID, id)})
		if err != nil || string(pub.Data) != want {
			t.Errorf("%s by its public link: %+v, %v", id, pub, err)
		}
	}
}

// A task deleted in a fork takes its files with it, from under the parent.
func TestDeletingATaskInAForkDeletesItsFiles(t *testing.T) {
	e := newForkTaskEnv(t)
	created, err := e.c.CreateTask(e.ctx, entity.CreateTaskRequest{UserID: testUserIDStr, Task: entity.Task{
		WorkspaceID: fkFork, Title: "fix it", Body: "the bug", Assignee: "agent", CreatedBy: "human",
		Attachments: []entity.Attachment{file("spec.txt", "the spec")},
	}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	key := storage.AttachmentKey(fkParent, created.Task.ID, created.Task.Attachments[0].ID)
	if _, err := e.store.LoadRaw(key); err != nil {
		t.Fatalf("a task created in a fork files under the parent: %v", err)
	}

	if _, err := e.c.DeleteTask(e.ctx, entity.DeleteTaskRequest{WorkspaceID: fkFork, TaskID: created.Task.ID, UserID: testUserIDStr}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := e.store.LoadRaw(key); err == nil {
		t.Fatal("the file outlived its task")
	}
}

// A task copied from a message in a fork copies its files under the parent.
func TestForkTaskInAForkCopiesFilesUnderTheParent(t *testing.T) {
	e := newForkTaskEnv(t)
	created, err := e.c.CreateTask(e.ctx, entity.CreateTaskRequest{UserID: testUserIDStr, Task: entity.Task{
		WorkspaceID: fkFork, Title: "fix it", Body: "the bug", Assignee: "agent", CreatedBy: "human",
	}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	replied, err := e.c.RespondToTask(e.ctx, entity.RespondToTaskRequest{WorkspaceID: fkFork, TaskID: created.Task.ID, UserID: testUserIDStr, Action: "text", Text: "a log", Attachments: []entity.Attachment{file("log.txt", "the log")}})
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	msg := replied.Task.Messages[len(replied.Task.Messages)-1]
	if _, err := e.store.LoadRaw(storage.AttachmentKey(fkParent, created.Task.ID, msg.Attachments[0].ID)); err != nil {
		t.Fatalf("a response in a fork files under the parent: %v", err)
	}

	copied, err := e.c.ForkTask(e.ctx, entity.ForkTaskRequest{WorkspaceID: fkFork, TaskID: created.Task.ID, MessageID: msg.ID, UserID: testUserIDStr, Status: "notstarted"})
	if err != nil {
		t.Fatalf("copy the task: %v", err)
	}
	last := copied.Task.Messages[len(copied.Task.Messages)-1]
	if got := e.attachment(t, fkFork, copied.Task.ID, last.Attachments[0].ID); string(got) != "the log" {
		t.Fatalf("the copy's file: %q", got)
	}
}

func TestGetAttachment_WorkspaceLookupFails(t *testing.T) {
	e := newTestController(t)
	attsJSON, _ := json.Marshal([]entity.Attachment{{ID: "att-1", Filename: "f.txt"}})
	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(model.Task{ID: 10, WorkspaceID: 1, Attachments: datatypes.JSON(attsJSON)}, nil)
	e.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(1)).Return(model.Workspace{}, errors.New("database unavailable"))

	_, err := e.controller.GetAttachment(context.Background(), entity.GetAttachmentRequest{WorkspaceID: 1, TaskID: 10, AttachmentID: "att-1", UserID: testUserIDStr})

	if !errors.Is(err, base.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// A fork of a parent with clear-context turned off has it off too, in the
// database and not only in the answer: every task made in the fork reads it.
func TestForkKeepsTheParentsClearContextOff(t *testing.T) {
	e := newForkTaskEnv(t)
	if err := e.db.Model(&model.Workspace{}).Where("id = ?", fkParent).Update("clear_context_default", false).Error; err != nil {
		t.Fatal(err)
	}
	rs, err := e.c.ForkWorkspace(e.ctx, entity.ForkWorkspaceRequest{UserID: testUserIDStr, WorkspaceID: fkParent, Name: "billing"})
	if err != nil {
		t.Fatal(err)
	}
	var stored model.Workspace
	if err := e.db.First(&stored, rs.Workspace.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ClearContextDefault || rs.Workspace.ClearContextDefault {
		t.Errorf("fork clearContextDefault: stored %v, answered %v, want false like its parent",
			stored.ClearContextDefault, rs.Workspace.ClearContextDefault)
	}
}
