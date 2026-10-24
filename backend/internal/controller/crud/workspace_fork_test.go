// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"
	"gorm.io/datatypes"
)

const (
	fkParent = int64(10)
	fkFork   = int64(20)
)

type denyWorkspaceLimiter struct{ stubLimiter }

func (*denyWorkspaceLimiter) AllowWorkspace(int64) bool { return false }

func parentWorkspace() model.Workspace {
	return model.Workspace{
		ID: fkParent, UserID: testUserID, Name: "api", Description: "the API", Icon: "icon",
		WorkingDirectory:      "/src/api",
		NotificationSettings:  datatypes.JSON(`{"TaskCreated":true}`),
		AutoAllowedTools:      datatypes.JSON(`["Bash"]`),
		AllowAllCommands:      true,
		ClearContextDefault:   true,
		SelfLearningLoopNote:  "learn",
		InputSendDelaySeconds: 5,
	}
}

func forkWorkspace() model.Workspace {
	f := parentWorkspace()
	f.ID, f.Name, f.WorkingDirectory, f.ForkOfID = fkFork, "api-fork", "", fkParent
	return f
}

func wantForkErr(t *testing.T, err error, kind error, msg string) {
	t.Helper()
	if !errors.Is(err, kind) {
		t.Fatalf("the error is %v, want one of kind %v", err, kind)
	}
	if err.Error() != msg {
		t.Errorf("the error says %q, want %q", err.Error(), msg)
	}
}

func TestForkWorkspace_CopiesTheParent(t *testing.T) {
	env := newMachineTelemetryEnv(t)
	parent := parentWorkspace()
	env.repo.EXPECT().GetWorkspace(gomock.Any(), fkParent, testUserID).Return(parent, nil)
	env.idgen.EXPECT().NextID().Return(fkFork)
	var created model.Workspace
	env.repo.EXPECT().CreateWorkspace(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, m model.Workspace) (model.Workspace, error) {
			created = m
			return m, nil
		})

	forked, err := env.controller.ForkWorkspace(context.Background(), entity.ForkWorkspaceRequest{UserID: testUserIDStr, WorkspaceID: fkParent})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != fkFork || created.ForkOfID != fkParent || created.Name != "api-fork" || created.UserID != testUserID {
		t.Errorf("the stored fork is %+v, want id %d, forkOfId %d, name \"api-fork\" and the parent's owner", created, fkFork, fkParent)
	}
	if created.Icon != "icon" || created.Description != "the API" {
		t.Errorf("the stored fork is %+v, want the parent's icon and description copied", created)
	}
	if created.WorkingDirectory != "" {
		t.Errorf("the fork's working directory is %q, want none: the daemon makes the fork's folder", created.WorkingDirectory)
	}
	if !reflect.DeepEqual(created.ForkSettings(), parent.ForkSettings()) {
		t.Errorf("the fork's settings are %v, want the parent's %v", created.ForkSettings(), parent.ForkSettings())
	}
	if created.CreatedAt.IsZero() || created.CreatedAt.Equal(parent.CreatedAt) {
		t.Error("the fork kept the parent's creation time")
	}
	if forked.Workspace.ForkOfID != fkParent || forked.Workspace.ForkOf == nil || forked.Workspace.ForkOf.Name != "api" {
		t.Errorf("the returned fork is %+v, want it to name its parent api", forked.Workspace)
	}
	// A fork is a workspace too, so both are counted.
	if got := env.actions(); !reflect.DeepEqual(got, []entity.Action{entity.ActionWorkspaceCreate, entity.ActionWorkspaceForkCreate}) {
		t.Errorf("the counted actions are %v, want a workspace create and a fork create", got)
	}
	for _, e := range *env.events {
		if e.WorkspaceID != fkFork || e.UserID != testUserID {
			t.Errorf("the event is %+v, want it counted against the fork %d and user %d", e, fkFork, testUserID)
		}
	}
}

func TestForkWorkspace_TakesTheGivenName(t *testing.T) {
	env := newMachineTelemetryEnv(t)
	env.repo.EXPECT().GetWorkspace(gomock.Any(), fkParent, testUserID).Return(parentWorkspace(), nil)
	env.idgen.EXPECT().NextID().Return(fkFork)
	env.repo.EXPECT().CreateWorkspace(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, m model.Workspace) (model.Workspace, error) { return m, nil })

	forked, err := env.controller.ForkWorkspace(context.Background(), entity.ForkWorkspaceRequest{UserID: testUserIDStr, WorkspaceID: fkParent, Name: "billing"})
	if err != nil || forked.Workspace.Name != "billing" {
		t.Fatalf("ForkWorkspace returned the name %q and error %v, want \"billing\" and no error", forked.Workspace.Name, err)
	}
}

// Kebab-case, like every workspace name the web app makes.
func TestForkName_IsKebabCase(t *testing.T) {
	for parent, want := range map[string]string{
		"api":            "api-fork",
		"Billing API":    "billing-api-fork",
		"  --Ops__v2!! ": "ops-v2-fork",
		"проект":         "fork", // nothing kebab-case is left of it
		"":               "fork",
	} {
		if got := forkName(parent); got != want {
			t.Errorf("forkName(%q) = %q, want %q", parent, got, want)
		}
	}
}

func TestForkName_FitsTheColumn(t *testing.T) {
	got := forkName(strings.Repeat("x", 200))
	if len(got) != maxWorkspaceNameRunes || !strings.HasSuffix(got, "x-fork") {
		t.Errorf("forkName of 200 characters is %d long and ends %q, want %d long and ending \"x-fork\"", len(got), got[len(got)-10:], maxWorkspaceNameRunes)
	}
	// A cut that lands on a hyphen does not leave two.
	cut := forkName(strings.Repeat("x", maxWorkspaceNameRunes-6) + " yz")
	if strings.Contains(cut, "--") || len(cut) > maxWorkspaceNameRunes {
		t.Errorf("forkName cut on a hyphen is %q, want no double hyphen and at most %d characters", cut, maxWorkspaceNameRunes)
	}
}

func TestForkWorkspace_Refusals(t *testing.T) {
	archived := parentWorkspace()
	now := time.Now()
	archived.ArchivedAt = &now
	supervisor := parentWorkspace()
	supervisor.Name = "supervisor"

	for _, tc := range []struct {
		name   string
		parent model.Workspace
		msg    string
	}{
		{"a fork cannot be forked", forkWorkspace(), "a fork cannot be forked"},
		{"the supervisor cannot be forked", supervisor, "the supervisor workspace cannot be forked"},
		{"an archived workspace cannot be forked", archived, "an archived workspace cannot be forked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newMachineTelemetryEnv(t)
			env.repo.EXPECT().GetWorkspace(gomock.Any(), gomock.Any(), testUserID).Return(tc.parent, nil)
			_, err := env.controller.ForkWorkspace(context.Background(), entity.ForkWorkspaceRequest{UserID: testUserIDStr, WorkspaceID: tc.parent.ID})
			wantForkErr(t, err, entity.ErrForkOfFork, tc.msg)
			if len(*env.events) != 0 {
				t.Errorf("a refused fork was counted: %v", env.actions())
			}
		})
	}
}

func TestForkWorkspace_Errors(t *testing.T) {
	t.Run("a rate-limited account is refused", func(t *testing.T) {
		env := newMachineTelemetryEnv(t)
		env.controller.(*controller).limiter = &denyWorkspaceLimiter{}
		_, err := env.controller.ForkWorkspace(context.Background(), entity.ForkWorkspaceRequest{UserID: testUserIDStr, WorkspaceID: fkParent})
		if err == nil || err.Error() != "rate limit exceeded" {
			t.Fatalf("ForkWorkspace returned %v, want \"rate limit exceeded\"", err)
		}
	})
	t.Run("a parent that does not exist is not found", func(t *testing.T) {
		env := newMachineTelemetryEnv(t)
		env.controller.(*controller).limiter = &stubLimiter{}
		env.repo.EXPECT().GetWorkspace(gomock.Any(), fkParent, testUserID).Return(model.Workspace{}, base.ErrNotFound)
		_, err := env.controller.ForkWorkspace(context.Background(), entity.ForkWorkspaceRequest{UserID: testUserIDStr, WorkspaceID: fkParent})
		if !errors.Is(err, base.ErrNotFound) {
			t.Fatalf("ForkWorkspace returned %v, want %v", err, base.ErrNotFound)
		}
	})
	t.Run("a failed save is returned and not counted", func(t *testing.T) {
		errDB := errors.New("database unavailable")
		env := newMachineTelemetryEnv(t)
		env.repo.EXPECT().GetWorkspace(gomock.Any(), fkParent, testUserID).Return(parentWorkspace(), nil)
		env.idgen.EXPECT().NextID().Return(fkFork)
		env.repo.EXPECT().CreateWorkspace(gomock.Any(), gomock.Any()).Return(model.Workspace{}, errDB)
		if _, err := env.controller.ForkWorkspace(context.Background(), entity.ForkWorkspaceRequest{UserID: testUserIDStr, WorkspaceID: fkParent}); err == nil {
			t.Fatal("ForkWorkspace returned no error, want the database error")
		}
		if len(*env.events) != 0 {
			t.Error("a failed fork was counted")
		}
	})
}

func TestMergeFork_MovesTheTasksBack(t *testing.T) {
	env := newMachineTelemetryEnv(t)
	env.repo.EXPECT().GetWorkspace(gomock.Any(), fkFork, testUserID).Return(forkWorkspace(), nil)
	env.repo.EXPECT().MergeForkIntoParent(gomock.Any(), fkFork, fkParent).Return([]int64{1, 2, 3}, nil)
	env.repo.EXPECT().GetTask(gomock.Any(), fkParent, int64(1), testUserID).Return(model.Task{ID: 1, WorkspaceID: fkParent}, nil)
	// A task that cannot be read back misses its live update, not the merge.
	env.repo.EXPECT().GetTask(gomock.Any(), fkParent, int64(2), testUserID).Return(model.Task{}, errors.New("task no longer readable"))
	env.repo.EXPECT().GetTask(gomock.Any(), fkParent, int64(3), testUserID).Return(model.Task{ID: 3, WorkspaceID: fkParent}, nil)

	merged, err := env.controller.MergeFork(context.Background(), entity.MergeForkRequest{UserID: testUserIDStr, WorkspaceID: fkFork})
	if err != nil {
		t.Fatal(err)
	}
	if merged.MovedTasks != 3 || len(merged.Tasks) != 2 || merged.Tasks[0].ID != 1 || merged.Tasks[1].ID != 3 {
		t.Errorf("the merge returned %+v, want 3 tasks moved and tasks 1 and 3 read back", merged)
	}
	if want := monoflake.ID(fkParent).String(); merged.ParentID != want {
		t.Errorf("the merge names the parent %q, want %q", merged.ParentID, want)
	}
	e := env.only(t)
	if e.Action != entity.ActionWorkspaceForkMerge || e.WorkspaceID != fkParent || e.ResourceID != fkFork {
		t.Errorf("the event is %+v, want a fork merge counted against the parent, which survives it", e)
	}
}

func TestMergeFork_Refusals(t *testing.T) {
	t.Run("a workspace that is not a fork cannot be merged", func(t *testing.T) {
		env := newMachineTelemetryEnv(t)
		env.repo.EXPECT().GetWorkspace(gomock.Any(), fkParent, testUserID).Return(parentWorkspace(), nil)
		_, err := env.controller.MergeFork(context.Background(), entity.MergeForkRequest{UserID: testUserIDStr, WorkspaceID: fkParent})
		wantForkErr(t, err, entity.ErrNotAFork, "only a fork can be merged")
	})
	t.Run("a fork with unfinished tasks is refused and not counted", func(t *testing.T) {
		env := newMachineTelemetryEnv(t)
		env.repo.EXPECT().GetWorkspace(gomock.Any(), fkFork, testUserID).Return(forkWorkspace(), nil)
		env.repo.EXPECT().MergeForkIntoParent(gomock.Any(), fkFork, fkParent).
			Return(nil, entity.NewForkError(entity.ErrForkUnfinished, "2 tasks in this fork are not finished"))
		_, err := env.controller.MergeFork(context.Background(), entity.MergeForkRequest{UserID: testUserIDStr, WorkspaceID: fkFork})
		wantForkErr(t, err, entity.ErrForkUnfinished, "2 tasks in this fork are not finished")
		if len(*env.events) != 0 {
			t.Error("a refused merge was counted")
		}
	})
	t.Run("a fork that does not exist is not found", func(t *testing.T) {
		env := newMachineTelemetryEnv(t)
		env.repo.EXPECT().GetWorkspace(gomock.Any(), fkFork, testUserID).Return(model.Workspace{}, base.ErrNotFound)
		_, err := env.controller.MergeFork(context.Background(), entity.MergeForkRequest{UserID: testUserIDStr, WorkspaceID: fkFork})
		if !errors.Is(err, base.ErrNotFound) {
			t.Fatalf("MergeFork returned %v, want %v", err, base.ErrNotFound)
		}
	})
}

func TestDeleteAndArchive_RefuseForksAndTheirParents(t *testing.T) {
	type op func(env *machineTelemetryEnv, id int64) error
	deleteWorkspace := func(env *machineTelemetryEnv, id int64) error {
		return env.controller.DeleteWorkspace(context.Background(), entity.DeleteWorkspaceRequest{ID: id, UserID: testUserIDStr})
	}
	archive := func(env *machineTelemetryEnv, id int64) error {
		return env.controller.ArchiveWorkspace(context.Background(), entity.ArchiveWorkspaceRequest{ID: id, UserID: testUserIDStr})
	}
	for name, do := range map[string]op{"delete": deleteWorkspace, "archive": archive} {
		t.Run(name+" a fork", func(t *testing.T) {
			env := newMachineTelemetryEnv(t)
			env.repo.EXPECT().GetWorkspace(gomock.Any(), fkFork, testUserID).Return(forkWorkspace(), nil)
			wantForkErr(t, do(env, fkFork), entity.ErrForkNoDelete, "merge this fork instead")
		})
		for n, msg := range map[int64]string{
			1: "this workspace has 1 fork; merge it first",
			3: "this workspace has 3 forks; merge them first",
		} {
			t.Run(name+" a parent", func(t *testing.T) {
				env := newMachineTelemetryEnv(t)
				env.repo.EXPECT().GetWorkspace(gomock.Any(), fkParent, testUserID).Return(parentWorkspace(), nil)
				env.repo.EXPECT().CountForks(gomock.Any(), fkParent, testUserID).Return(n, nil)
				wantForkErr(t, do(env, fkParent), entity.ErrHasForks, msg)
			})
		}
		t.Run(name+" returns a failed fork count", func(t *testing.T) {
			errDB := errors.New("database unavailable")
			env := newMachineTelemetryEnv(t)
			env.repo.EXPECT().GetWorkspace(gomock.Any(), fkParent, testUserID).Return(parentWorkspace(), nil)
			env.repo.EXPECT().CountForks(gomock.Any(), fkParent, testUserID).Return(int64(0), errDB)
			if err := do(env, fkParent); err == nil || err.Error() != errDB.Error() {
				t.Fatalf("%s returned %v, want the database error %v", name, err, errDB)
			}
		})
	}
	t.Run("deleting a workspace that does not exist is not found", func(t *testing.T) {
		env := newMachineTelemetryEnv(t)
		env.repo.EXPECT().GetWorkspace(gomock.Any(), fkParent, testUserID).Return(model.Workspace{}, base.ErrNotFound)
		if err := deleteWorkspace(env, fkParent); !errors.Is(err, base.ErrNotFound) {
			t.Fatalf("DeleteWorkspace returned %v, want %v", err, base.ErrNotFound)
		}
	})
}

// updateRequest is what the settings form sends for w, unchanged.
func updateRequest(w model.Workspace) entity.UpdateWorkspaceRequest {
	e := fromModelWorkspaceToEntity(w)
	e.Icon = "" // an icon is sent only when it changes
	return entity.UpdateWorkspaceRequest{UserID: testUserIDStr, Workspace: e}
}

func TestUpdateWorkspace_ForkKeepsItsInheritedSettings(t *testing.T) {
	change := map[string]func(*entity.Workspace){
		"notification_settings":    func(w *entity.Workspace) { w.NotificationSettings = &entity.NotificationSettings{TaskCreated: false} },
		"auto_allowed_tools":       func(w *entity.Workspace) { w.AutoAllowedTools = []string{"Bash", "Edit"} },
		"allow_all_commands":       func(w *entity.Workspace) { w.AllowAllCommands = false },
		"clear_context_default":    func(w *entity.Workspace) { w.ClearContextDefault = false },
		"self_learning_loop_note":  func(w *entity.Workspace) { w.SelfLearningLoopNote = "other" },
		"input_send_delay_seconds": func(w *entity.Workspace) { w.InputSendDelaySeconds = 10 },
	}
	// Every inherited setting is refused here, and nothing else is inherited.
	for col := range forkWorkspace().ForkSettings() {
		if change[col] == nil {
			t.Errorf("no refusal case for %s", col)
		}
	}
	for col, mutate := range change {
		t.Run(col, func(t *testing.T) {
			env := newMachineTelemetryEnv(t)
			env.repo.EXPECT().GetWorkspace(gomock.Any(), fkFork, testUserID).Return(forkWorkspace(), nil)
			env.repo.EXPECT().GetWorkspace(gomock.Any(), fkParent, testUserID).Return(parentWorkspace(), nil)
			rq := updateRequest(forkWorkspace())
			mutate(&rq.Workspace)
			_, err := env.controller.UpdateWorkspace(context.Background(), rq)
			wantForkErr(t, err, entity.ErrForkInherited, "this setting is inherited from api; change it there")
		})
	}
	t.Run("an unreadable parent is not named in the refusal", func(t *testing.T) {
		env := newMachineTelemetryEnv(t)
		env.repo.EXPECT().GetWorkspace(gomock.Any(), fkFork, testUserID).Return(forkWorkspace(), nil)
		env.repo.EXPECT().GetWorkspace(gomock.Any(), fkParent, testUserID).Return(model.Workspace{}, errors.New("database unavailable"))
		rq := updateRequest(forkWorkspace())
		rq.Workspace.AllowAllCommands = false
		_, err := env.controller.UpdateWorkspace(context.Background(), rq)
		wantForkErr(t, err, entity.ErrForkInherited, "this setting is inherited from its parent; change it there")
	})
}

func TestUpdateWorkspace_ForkEditsItsOwnFields(t *testing.T) {
	env := newMachineTelemetryEnv(t)
	env.repo.EXPECT().GetWorkspace(gomock.Any(), fkFork, testUserID).Return(forkWorkspace(), nil)
	env.repo.EXPECT().UpdateWorkspace(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, m model.Workspace) (model.Workspace, error) { return m, nil })
	rq := updateRequest(forkWorkspace())
	rq.Workspace.Name = "renamed"
	rq.Workspace.Description = "mine"
	rq.Workspace.WorkingDirectory = "/home/me/.agentrq/forks/x"
	updated, err := env.controller.UpdateWorkspace(context.Background(), rq)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Workspace.Name != "renamed" || updated.Workspace.WorkingDirectory != "/home/me/.agentrq/forks/x" || updated.ForkIDs != nil {
		t.Errorf("the update returned %+v, want the new name and folder and no fork ids", updated)
	}
}

func TestUpdateWorkspace_ParentNamesItsForks(t *testing.T) {
	env := newMachineTelemetryEnv(t)
	env.repo.EXPECT().GetWorkspace(gomock.Any(), fkParent, testUserID).Return(parentWorkspace(), nil)
	env.repo.EXPECT().UpdateWorkspace(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, m model.Workspace) (model.Workspace, error) { return m, nil })
	env.repo.EXPECT().ListForks(gomock.Any(), fkParent, testUserID).Return([]model.Workspace{{ID: 21}, {ID: 22}}, nil)
	rq := updateRequest(parentWorkspace())
	rq.Workspace.AllowAllCommands = false
	updated, err := env.controller.UpdateWorkspace(context.Background(), rq)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(updated.ForkIDs, []int64{21, 22}) {
		t.Errorf("the update names the forks %v, want [21 22]", updated.ForkIDs)
	}
}

func TestUpdateWorkspaceAutoAllowedTools_ForkWritesItsParent(t *testing.T) {
	env := newMachineTelemetryEnv(t)
	env.repo.EXPECT().GetWorkspace(gomock.Any(), fkFork, testUserID).Return(forkWorkspace(), nil)
	env.repo.EXPECT().GetWorkspace(gomock.Any(), fkParent, testUserID).Return(parentWorkspace(), nil)
	env.repo.EXPECT().UpdateWorkspace(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, m model.Workspace) (model.Workspace, error) {
			if m.ID != fkParent || string(m.AutoAllowedTools) != `["Bash","Edit"]` {
				t.Errorf("saved workspace %d with tools %s, want the parent %d with [\"Bash\",\"Edit\"]", m.ID, m.AutoAllowedTools, fkParent)
			}
			return m, nil
		})
	err := env.controller.UpdateWorkspaceAutoAllowedTools(context.Background(), entity.UpdateWorkspaceAutoAllowedToolsRequest{
		WorkspaceID: fkFork, UserID: testUserIDStr, Tools: []string{"Bash", "Edit"},
	})
	if err != nil {
		t.Fatal(err)
	}

	env = newMachineTelemetryEnv(t)
	env.repo.EXPECT().GetWorkspace(gomock.Any(), fkFork, testUserID).Return(forkWorkspace(), nil)
	env.repo.EXPECT().GetWorkspace(gomock.Any(), fkParent, testUserID).Return(model.Workspace{}, base.ErrNotFound)
	err = env.controller.UpdateWorkspaceAutoAllowedTools(context.Background(), entity.UpdateWorkspaceAutoAllowedToolsRequest{
		WorkspaceID: fkFork, UserID: testUserIDStr, Tools: []string{"Bash"},
	})
	if !errors.Is(err, base.ErrNotFound) {
		t.Fatalf("with the parent gone, UpdateWorkspaceAutoAllowedTools returned %v, want %v", err, base.ErrNotFound)
	}
}

func TestListWorkspaces_FillsInForks(t *testing.T) {
	env := newMachineTelemetryEnv(t)
	orphan := model.Workspace{ID: 30, UserID: testUserID, Name: "orphan", ForkOfID: 99}
	env.repo.EXPECT().ListWorkspaces(gomock.Any(), testUserID, false).
		Return([]model.Workspace{forkWorkspace(), parentWorkspace(), orphan, {ID: 40, Name: "plain"}}, nil)
	env.repo.EXPECT().CountUnfinishedTasks(gomock.Any(), []int64{fkFork, 30}).Return(map[int64]int64{fkFork: 2}, nil)
	env.repo.EXPECT().SummarizeTasks(gomock.Any(), gomock.Any()).Return(nil, nil)

	listed, err := env.controller.ListWorkspaces(context.Background(), entity.ListWorkspacesRequest{UserID: testUserIDStr})
	if err != nil {
		t.Fatal(err)
	}
	fork, parent, orphanFork, plain := listed.Workspaces[0], listed.Workspaces[1], listed.Workspaces[2], listed.Workspaces[3]
	if fork.ForkOfID != fkParent || fork.ForkOf == nil || fork.ForkOf.Name != "api" || fork.UnfinishedTasks != 2 {
		t.Errorf("the fork is listed as %+v, want it to name its parent api and 2 unfinished tasks", fork)
	}
	if parent.ForkCount != 1 || parent.ForkOf != nil {
		t.Errorf("the parent is listed as %+v, want 1 fork and no parent of its own", parent)
	}
	if orphanFork.ForkOfID != 99 || orphanFork.ForkOf != nil || orphanFork.UnfinishedTasks != 0 {
		t.Errorf("the fork of a parent not listed is %+v, want forkOfId 99, no parent details and 0 unfinished tasks", orphanFork)
	}
	if plain.ForkCount != 0 || plain.ForkOfID != 0 {
		t.Errorf("the plain workspace is listed as %+v, want no forks and no parent", plain)
	}

	env = newMachineTelemetryEnv(t)
	env.repo.EXPECT().ListWorkspaces(gomock.Any(), testUserID, false).Return([]model.Workspace{forkWorkspace()}, nil)
	env.repo.EXPECT().CountUnfinishedTasks(gomock.Any(), gomock.Any()).Return(nil, errors.New("database unavailable"))
	if _, err := env.controller.ListWorkspaces(context.Background(), entity.ListWorkspacesRequest{UserID: testUserIDStr}); err == nil {
		t.Fatal("ListWorkspaces returned no error, want the failed count's error")
	}
}

func TestSameForkSettings_ComparesMeaning(t *testing.T) {
	a := parentWorkspace()
	b := parentWorkspace()
	b.NotificationSettings = datatypes.JSON(`{ "TaskCreated": true }`)
	if !sameForkSettings(a, b) {
		t.Error("the same JSON with other spacing counted as a change")
	}
	b.NotificationSettings = datatypes.JSON(`{"TaskCreated":true,"TaskStatusUpdated":false,"Channels":[],"c":"","d":0,"x":null}`)
	if !sameForkSettings(a, b) {
		t.Error("a key set to its zero value counted as a change")
	}
	a.AutoAllowedTools, b.AutoAllowedTools = nil, datatypes.JSON(`[]`)
	a.NotificationSettings, b.NotificationSettings = datatypes.JSON(`{}`), datatypes.JSON(`null`)
	if !sameForkSettings(a, b) {
		t.Error("empty, [] , {} and null counted as different")
	}
	b.NotificationSettings = datatypes.JSON(`not json`)
	if !sameForkSettings(a, b) {
		t.Error("unreadable JSON is no setting at all")
	}
	b.AutoAllowedTools = datatypes.JSON(`["Bash"]`)
	if sameForkSettings(a, b) {
		t.Error("a new tool went unnoticed")
	}
}

// The check a merge makes before stopping the fork's agent.
func TestCheckForkMerge(t *testing.T) {
	req := entity.MergeForkRequest{UserID: testUserIDStr, WorkspaceID: fkFork}
	t.Run("a fork with every task finished is ready", func(t *testing.T) {
		env := newMachineTelemetryEnv(t)
		env.repo.EXPECT().GetWorkspace(gomock.Any(), fkFork, testUserID).Return(forkWorkspace(), nil)
		env.repo.EXPECT().CountUnfinishedTasks(gomock.Any(), []int64{fkFork}).Return(map[int64]int64{}, nil)
		if err := env.controller.CheckForkMerge(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("a fork with an unfinished task is refused", func(t *testing.T) {
		env := newMachineTelemetryEnv(t)
		env.repo.EXPECT().GetWorkspace(gomock.Any(), fkFork, testUserID).Return(forkWorkspace(), nil)
		env.repo.EXPECT().CountUnfinishedTasks(gomock.Any(), []int64{fkFork}).Return(map[int64]int64{fkFork: 1}, nil)
		wantForkErr(t, env.controller.CheckForkMerge(context.Background(), req), entity.ErrForkUnfinished, "1 task in this fork is not finished")
	})
	t.Run("a workspace that is not a fork is refused", func(t *testing.T) {
		env := newMachineTelemetryEnv(t)
		env.repo.EXPECT().GetWorkspace(gomock.Any(), fkParent, testUserID).Return(parentWorkspace(), nil)
		err := env.controller.CheckForkMerge(context.Background(), entity.MergeForkRequest{UserID: testUserIDStr, WorkspaceID: fkParent})
		wantForkErr(t, err, entity.ErrNotAFork, "only a fork can be merged")
	})
	t.Run("a failed count of unfinished tasks is returned", func(t *testing.T) {
		env := newMachineTelemetryEnv(t)
		env.repo.EXPECT().GetWorkspace(gomock.Any(), fkFork, testUserID).Return(forkWorkspace(), nil)
		env.repo.EXPECT().CountUnfinishedTasks(gomock.Any(), []int64{fkFork}).Return(nil, errors.New("database unavailable"))
		if err := env.controller.CheckForkMerge(context.Background(), req); err == nil {
			t.Fatal("CheckForkMerge returned no error, want the failed count's error")
		}
	})
}

func TestRecordForkDirectory(t *testing.T) {
	const sessionID = int64(77)
	req := func(dir string) entity.RecordForkDirectoryRequest {
		return entity.RecordForkDirectoryRequest{UserID: testUserIDStr, SessionID: monoflake.ID(sessionID).String(), Dir: dir}
	}
	const made = "/home/u/.agentrq/forks/abc"
	const machineID = int64(5)
	session := model.Session{ID: sessionID, UserID: testUserID, WorkspaceID: fkFork, MachineID: machineID}

	t.Run("a fork's new folder is stored on the fork", func(t *testing.T) {
		env := newMachineTelemetryEnv(t)
		env.repo.EXPECT().GetSession(gomock.Any(), sessionID, testUserID).Return(session, nil)
		env.repo.EXPECT().GetWorkspace(gomock.Any(), fkFork, testUserID).Return(forkWorkspace(), nil)
		env.repo.EXPECT().RecordForkFolder(gomock.Any(), fkFork, machineID, testUserID).Return(nil)
		env.repo.EXPECT().SetWorkingDirectory(gomock.Any(), fkFork, testUserID, made).Return(nil)
		if err := env.controller.RecordForkDirectory(context.Background(), req(made)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("a workspace that is not a fork keeps its own folder", func(t *testing.T) {
		env := newMachineTelemetryEnv(t)
		env.repo.EXPECT().GetSession(gomock.Any(), sessionID, testUserID).Return(model.Session{WorkspaceID: fkParent}, nil)
		env.repo.EXPECT().GetWorkspace(gomock.Any(), fkParent, testUserID).Return(parentWorkspace(), nil)
		if err := env.controller.RecordForkDirectory(context.Background(), req("/src/api")); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("the same folder is not saved again, but its machine is recorded", func(t *testing.T) {
		env := newMachineTelemetryEnv(t)
		fork := forkWorkspace()
		fork.WorkingDirectory = made
		env.repo.EXPECT().GetSession(gomock.Any(), sessionID, testUserID).Return(session, nil)
		env.repo.EXPECT().GetWorkspace(gomock.Any(), fkFork, testUserID).Return(fork, nil)
		env.repo.EXPECT().RecordForkFolder(gomock.Any(), fkFork, machineID, testUserID).Return(nil)
		if err := env.controller.RecordForkDirectory(context.Background(), req(made)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("an empty folder is ignored, and a relative folder or missing ids are refused", func(t *testing.T) {
		env := newMachineTelemetryEnv(t)
		if err := env.controller.RecordForkDirectory(context.Background(), req("")); err != nil {
			t.Fatal(err)
		}
		if err := env.controller.RecordForkDirectory(context.Background(), req("relative/dir")); err == nil {
			t.Error("a relative folder was accepted")
		}
		if err := env.controller.RecordForkDirectory(context.Background(), entity.RecordForkDirectoryRequest{Dir: made}); err == nil {
			t.Error("a report with no ids was accepted")
		}
	})
	t.Run("a failed read or write is returned to the caller", func(t *testing.T) {
		errDB := errors.New("database unavailable")
		env := newMachineTelemetryEnv(t)
		env.repo.EXPECT().GetSession(gomock.Any(), sessionID, testUserID).Return(model.Session{}, errDB)
		if err := env.controller.RecordForkDirectory(context.Background(), req(made)); !errors.Is(err, errDB) {
			t.Errorf("reading the session returned %v, want the database error", err)
		}
		env = newMachineTelemetryEnv(t)
		env.repo.EXPECT().GetSession(gomock.Any(), sessionID, testUserID).Return(session, nil)
		env.repo.EXPECT().GetWorkspace(gomock.Any(), fkFork, testUserID).Return(model.Workspace{}, errDB)
		if err := env.controller.RecordForkDirectory(context.Background(), req(made)); !errors.Is(err, errDB) {
			t.Errorf("reading the workspace returned %v, want the database error", err)
		}
		env = newMachineTelemetryEnv(t)
		env.repo.EXPECT().GetSession(gomock.Any(), sessionID, testUserID).Return(session, nil)
		env.repo.EXPECT().GetWorkspace(gomock.Any(), fkFork, testUserID).Return(forkWorkspace(), nil)
		env.repo.EXPECT().RecordForkFolder(gomock.Any(), fkFork, machineID, testUserID).Return(errDB)
		if err := env.controller.RecordForkDirectory(context.Background(), req(made)); !errors.Is(err, errDB) {
			t.Errorf("recording the fork's machine returned %v, want the database error", err)
		}
		env = newMachineTelemetryEnv(t)
		env.repo.EXPECT().GetSession(gomock.Any(), sessionID, testUserID).Return(session, nil)
		env.repo.EXPECT().GetWorkspace(gomock.Any(), fkFork, testUserID).Return(forkWorkspace(), nil)
		env.repo.EXPECT().RecordForkFolder(gomock.Any(), fkFork, machineID, testUserID).Return(nil)
		env.repo.EXPECT().SetWorkingDirectory(gomock.Any(), fkFork, testUserID, made).Return(errDB)
		if err := env.controller.RecordForkDirectory(context.Background(), req(made)); !errors.Is(err, errDB) {
			t.Errorf("saving the working directory returned %v, want the database error", err)
		}
	})
}
