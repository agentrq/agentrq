// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package teamview

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
)

var errBoom = errors.New("database unavailable")

// fake is every dependency at once. The method named by fail fails; a task
// read is in workspace 100, which the team reads when canRead.
type fake struct {
	fail      string
	canRead   bool
	visible   []entity.TeamWorkspace
	task      model.Task
	memRows   []base.WorkspaceMemoryRow
	skillRows []base.WorkspaceSkillRow
	published []entity.CRUDEvent
	missing   string                   // the attachment whose file is gone
	created   entity.CreateTaskRequest // the last task created
}

func (f *fake) err(name string) error {
	if f.fail == name {
		return errBoom
	}
	return nil
}

func (f *fake) GetTeam(_ context.Context, _, teamID int64) (entity.Team, error) {
	return entity.Team{ID: teamID, Name: "core"}, f.err("GetTeam")
}
func (f *fake) CanReadWorkspaceInTeam(context.Context, int64, int64, int64) (bool, error) {
	return f.canRead, f.err("CanReadWorkspaceInTeam")
}
func (f *fake) ListTeamWorkspaces(context.Context, int64, int64) ([]entity.TeamWorkspace, error) {
	return f.visible, f.err("ListTeamWorkspaces")
}

func (f *fake) GetTask(context.Context, entity.GetTaskRequest) (*entity.GetTaskResponse, error) {
	return &entity.GetTaskResponse{}, f.err("GetTask")
}
func (f *fake) GetTaskPlan(context.Context, entity.GetTaskPlanRequest) (*entity.GetTaskPlanResponse, error) {
	return &entity.GetTaskPlanResponse{}, f.err("GetTaskPlan")
}
func (f *fake) GetAttachment(_ context.Context, req entity.GetAttachmentRequest) (*entity.GetAttachmentResponse, error) {
	if req.AttachmentID == f.missing {
		return nil, base.ErrNotFound
	}
	return &entity.GetAttachmentResponse{Data: []byte("x")}, f.err("GetAttachment")
}
func (f *fake) GetMemory(context.Context, entity.GetMemoryRequest) (*entity.GetMemoryResponse, error) {
	return &entity.GetMemoryResponse{}, f.err("GetMemory")
}
func (f *fake) CreateTask(_ context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
	f.created = req
	return &entity.CreateTaskResponse{Task: entity.Task{ID: 9}}, f.err("CreateTask")
}
func (f *fake) SystemListTasksInWorkspaces(context.Context, entity.ListTasksRequest, []int64) (*entity.ListTasksResponse, error) {
	return &entity.ListTasksResponse{}, f.err("SystemListTasksInWorkspaces")
}
func (f *fake) SystemGetDetailedStatsOfWorkspaces(context.Context, entity.GetStatsOfWorkspacesRequest) (*entity.GetStatsOfWorkspacesResponse, error) {
	return &entity.GetStatsOfWorkspacesResponse{}, f.err("SystemGetDetailedStatsOfWorkspaces")
}
func (f *fake) SystemGetSkillFile(context.Context, entity.SystemGetSkillFileRequest) (*entity.GetSkillFileResponse, error) {
	return &entity.GetSkillFileResponse{}, f.err("SystemGetSkillFile")
}
func (f *fake) CopySkill(context.Context, entity.CopySkillRequest) (*entity.CopySkillResponse, error) {
	return &entity.CopySkillResponse{Skill: entity.Skill{ID: 8}}, f.err("CopySkill")
}

func (f *fake) SystemGetTask(context.Context, int64) (model.Task, error) {
	return f.task, f.err("SystemGetTask")
}
func (f *fake) SystemGetUser(context.Context, int64) (model.User, error) {
	return model.User{Name: "Alice"}, f.err("SystemGetUser")
}
func (f *fake) TeamWorkspaceInfo(_ context.Context, _, workspaceID int64) (base.TeamWorkspaceRow, error) {
	return base.TeamWorkspaceRow{ID: workspaceID, UserID: 1}, f.err("TeamWorkspaceInfo")
}
func (f *fake) GetTeamMemory(context.Context, int64, string) (model.TeamMemory, error) {
	return model.TeamMemory{}, f.err("GetTeamMemory")
}
func (f *fake) SaveTeamMemory(context.Context, model.TeamMemory) (model.TeamMemory, bool, error) {
	return model.TeamMemory{}, false, f.err("SaveTeamMemory")
}
func (f *fake) ListWorkspaceMemoryNames(context.Context, []int64) ([]base.WorkspaceMemoryRow, error) {
	return f.memRows, f.err("ListWorkspaceMemoryNames")
}
func (f *fake) ListSkillsOnInWorkspaces(context.Context, []int64) ([]base.WorkspaceSkillRow, error) {
	return f.skillRows, f.err("ListSkillsOnInWorkspaces")
}

func (f *fake) Publish(_ context.Context, req pubsub.PublishRequest) (*pubsub.PublishResponse, error) {
	f.published = append(f.published, req.Event.(entity.CRUDEvent))
	return &pubsub.PublishResponse{}, nil
}

type ids struct{ n int64 }

func (s *ids) NextID() int64 { s.n++; return s.n }

func newFake() (*fake, Controller) {
	f := &fake{
		canRead: true,
		visible: []entity.TeamWorkspace{{ID: 100, OwnerID: 1, Group: "g"}},
		task:    model.Task{ID: 1000, WorkspaceID: 100, Attachments: []byte(`[{"id":"a","filename":"f.txt"}]`)},
		skillRows: []base.WorkspaceSkillRow{
			{WorkspaceID: 100, UserID: 1, Name: "deploy"},
		},
	}
	return f, New(Params{Teams: f, Owner: f, Repository: f, IDGen: &ids{}, PubSub: f, MaxMemoryBytes: 10})
}

// Every read passes a failure on, as it is.
func TestFailures(t *testing.T) {
	ctx := context.Background()
	clone := entity.CloneTeamTaskRequest{UserID: 2, TeamID: 10, TaskID: 1000, WorkspaceID: 201}
	for _, tc := range []struct {
		fail string
		call func(c Controller) error
	}{
		{"SystemListTasksInWorkspaces", func(c Controller) error {
			_, err := c.ListTasks(ctx, entity.ListTeamTasksRequest{UserID: 2, TeamID: 10})
			return err
		}},
		{"SystemGetTask", func(c Controller) error { _, err := c.GetTask(ctx, 2, 10, 1000); return err }},
		{"CanReadWorkspaceInTeam", func(c Controller) error { _, err := c.GetTask(ctx, 2, 10, 1000); return err }},
		{"TeamWorkspaceInfo", func(c Controller) error { _, err := c.GetTask(ctx, 2, 10, 1000); return err }},
		{"GetTask", func(c Controller) error { _, err := c.GetTask(ctx, 2, 10, 1000); return err }},
		{"GetTaskPlan", func(c Controller) error { _, err := c.GetTask(ctx, 2, 10, 1000); return err }},
		{"SystemGetTask", func(c Controller) error { _, err := c.GetAttachment(ctx, 2, 10, 1000, "a"); return err }},
		{"ListTeamWorkspaces", func(c Controller) error {
			_, err := c.GetStats(ctx, entity.GetTeamStatsRequest{UserID: 2, TeamID: 10})
			return err
		}},
		{"SystemGetTask", func(c Controller) error { _, err := c.CloneTask(ctx, clone); return err }},
		{"GetTeam", func(c Controller) error { _, err := c.CloneTask(ctx, clone); return err }},
		{"CreateTask", func(c Controller) error { _, err := c.CloneTask(ctx, clone); return err }},
		{"GetTeam", func(c Controller) error { _, err := c.GetMemory(ctx, 2, 10); return err }},
		{"GetTeamMemory", func(c Controller) error { _, err := c.GetMemory(ctx, 2, 10); return err }},
		{"SaveTeamMemory", func(c Controller) error {
			_, err := c.SaveMemory(ctx, entity.SaveTeamMemoryRequest{UserID: 2, TeamID: 10})
			return err
		}},
		{"ListTeamWorkspaces", func(c Controller) error { _, err := c.MemoryIndex(ctx, 2, 10); return err }},
		{"ListWorkspaceMemoryNames", func(c Controller) error { _, err := c.MemoryIndex(ctx, 2, 10); return err }},
		{"CanReadWorkspaceInTeam", func(c Controller) error { _, err := c.GetMemberMemory(ctx, 2, 10, 100, "memory.md"); return err }},
		{"GetMemory", func(c Controller) error { _, err := c.GetMemberMemory(ctx, 2, 10, 100, "memory.md"); return err }},
		{"ListTeamWorkspaces", func(c Controller) error { _, err := c.ListSkills(ctx, 2, 10); return err }},
		{"ListSkillsOnInWorkspaces", func(c Controller) error { _, err := c.ListSkills(ctx, 2, 10); return err }},
		{"ListSkillsOnInWorkspaces", func(c Controller) error { _, err := c.GetSkill(ctx, 2, 10, "deploy"); return err }},
		{"SystemGetSkillFile", func(c Controller) error { _, err := c.GetSkill(ctx, 2, 10, "deploy"); return err }},
		{"ListSkillsOnInWorkspaces", func(c Controller) error { _, err := c.AddSkill(ctx, 2, 10, "deploy", nil); return err }},
		{"CopySkill", func(c Controller) error { _, err := c.AddSkill(ctx, 2, 10, "deploy", nil); return err }},
	} {
		f, c := newFake()
		f.fail = tc.fail
		if err := tc.call(c); !errors.Is(err, errBoom) {
			t.Errorf("with %s failing: err = %v, want it passed on", tc.fail, err)
		}
		if len(f.published) != 0 {
			t.Errorf("with %s failing, %d events were counted", tc.fail, len(f.published))
		}
	}
}

// A workspace the team does not read is not found, and nothing past the
// check is read.
func TestNotReadable(t *testing.T) {
	ctx := context.Background()
	f, c := newFake()
	f.canRead = false
	f.fail = "TeamWorkspaceInfo" // would surface if read
	if _, err := c.GetTask(ctx, 2, 10, 1000); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("GetTask: %v", err)
	}
	if _, err := c.GetMemberMemory(ctx, 2, 10, 100, "memory.md"); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("GetMemberMemory: %v", err)
	}
	if _, err := c.CloneTask(ctx, entity.CloneTeamTaskRequest{UserID: 2, TeamID: 10, TaskID: 1000, WorkspaceID: 201}); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("CloneTask: %v", err)
	}
}

// The three writes are counted as team actions, by whom and on what.
func TestTelemetry(t *testing.T) {
	ctx := context.Background()
	f, c := newFake()
	if _, err := c.CloneTask(ctx, entity.CloneTeamTaskRequest{UserID: 2, TeamID: 10, TaskID: 1000, WorkspaceID: 201}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SaveMemory(ctx, entity.SaveTeamMemoryRequest{UserID: 2, TeamID: 10, Content: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddSkill(ctx, 2, 10, "deploy", nil); err != nil {
		t.Fatal(err)
	}
	want := []entity.CRUDEvent{
		{Action: entity.ActionTeamTaskClone, WorkspaceID: 201, UserID: 2, ResourceID: 9},
		{Action: entity.ActionTeamMemorySave, UserID: 2, ResourceID: 10},
		{Action: entity.ActionTeamSkillAdd, UserID: 2, ResourceID: 8},
	}
	if len(f.published) != len(want) {
		t.Fatalf("counted %v", f.published)
	}
	for i, w := range want {
		g := f.published[i]
		if g.Action != w.Action || g.WorkspaceID != w.WorkspaceID || g.UserID != w.UserID || g.ResourceID != w.ResourceID ||
			g.ResourceType != entity.ResourceTeam || g.Actor != entity.ActorHuman || g.Origin != entity.OriginAPI {
			t.Errorf("event %d is %+v, want %+v", i, g, w)
		}
	}

	// With no publisher nothing is counted, and nothing fails.
	quiet := New(Params{Teams: f, Owner: f, Repository: f, IDGen: &ids{}, MaxMemoryBytes: 10})
	if _, err := quiet.SaveMemory(ctx, entity.SaveTeamMemoryRequest{UserID: 2, TeamID: 10}); err != nil {
		t.Fatal(err)
	}
}

// A plan the task has none of is left out; a writer whose account is gone
// is named by id alone; a task whose attachments are not JSON clones with none.
func TestOptionalReads(t *testing.T) {
	ctx := context.Background()
	f, c := newFake()

	notFound := &planNotFound{fake: f}
	c = New(Params{Teams: f, Owner: notFound, Repository: f, IDGen: &ids{}, MaxMemoryBytes: 10})
	if d, err := c.GetTask(ctx, 2, 10, 1000); err != nil || d.Plan != nil {
		t.Errorf("a task with no plan: %+v, %v", d.Plan, err)
	}

	f.fail = "SystemGetUser"
	if m, err := c.SaveMemory(ctx, entity.SaveTeamMemoryRequest{UserID: 2, TeamID: 10, Content: "x"}); err != nil || m.UpdatedBy != 2 || m.UpdatedByName != "" {
		t.Errorf("a writer whose account is gone: %+v, %v", m, err)
	}
	f.fail = ""

	f.task.Attachments = []byte(`not json`)
	if _, err := c.CloneTask(ctx, entity.CloneTeamTaskRequest{UserID: 2, TeamID: 10, TaskID: 1000, WorkspaceID: 201}); err != nil || f.created.Task.Attachments != nil {
		t.Errorf("a clone of a task whose attachments are not JSON: %v, files %+v", err, f.created.Task.Attachments)
	}
}

// A clone carries no file a browser could run as a page, and leaves out a
// file that is gone rather than failing.
func TestCloneFiles(t *testing.T) {
	f, c := newFake()
	f.task.Attachments = []byte(`[{"id":"a","filename":"a.html","mimeType":"text/html"},{"id":"gone","filename":"g.txt","mimeType":"text/plain"},` +
		`{"id":"s","filename":"s.svg","mimeType":"image/svg+xml"},{"id":"p","filename":"p.png","mimeType":"image/png"}]`)
	f.missing = "gone"
	if _, err := c.CloneTask(context.Background(), entity.CloneTeamTaskRequest{UserID: 2, TeamID: 10, TaskID: 1000, WorkspaceID: 201}); err != nil {
		t.Fatalf("a clone with a file gone: %v", err)
	}
	var got []string
	for _, a := range f.created.Task.Attachments {
		got = append(got, a.Filename+" "+a.MimeType+" "+a.Data)
	}
	want := []string{"a.html application/octet-stream eA==", "s.svg application/octet-stream eA==", "p.png image/png eA=="}
	if strings.Join(got, ", ") != strings.Join(want, ", ") {
		t.Errorf("the clone's files are %q, want %q", got, want)
	}
}

type planNotFound struct{ *fake }

func (p *planNotFound) GetTaskPlan(context.Context, entity.GetTaskPlanRequest) (*entity.GetTaskPlanResponse, error) {
	return nil, base.ErrNotFound
}

// The memory index is by group, ignoring case and spaces, then owner, then
// workspace, each with its latest change; a fork, which reads its parent's,
// is left out.
func TestMemoryIndexOrder(t *testing.T) {
	f, c := newFake()
	t1, t2 := time.Unix(100, 0), time.Unix(200, 0)
	f.visible = []entity.TeamWorkspace{
		{ID: 3, Group: "x", OwnerName: "Bob"},
		{ID: 2, Group: "X ", OwnerName: "alice"},
		{ID: 1, Group: "x", OwnerName: "Bob"},
		{ID: 4, Group: "a", OwnerName: "Zed"},
		{ID: 5, Group: "a", ForkOfID: 4},
	}
	f.memRows = []base.WorkspaceMemoryRow{
		{WorkspaceID: 1, Name: "a.md", UpdatedAt: t2}, {WorkspaceID: 1, Name: "b.md", UpdatedAt: t1},
		{WorkspaceID: 2, Name: "a.md", UpdatedAt: t1}, {WorkspaceID: 3, Name: "a.md", UpdatedAt: t1},
		{WorkspaceID: 4, Name: "a.md", UpdatedAt: t1},
	}
	es, err := c.MemoryIndex(context.Background(), 2, 10)
	if err != nil {
		t.Fatal(err)
	}
	var order []int64
	for _, e := range es {
		order = append(order, e.Workspace.ID)
	}
	if len(order) != 4 || order[0] != 4 || order[1] != 2 || order[2] != 1 || order[3] != 3 {
		t.Errorf("the index is in the order %v, want 4, 2, 1, 3", order)
	}
	if len(es) == 4 && (len(es[2].Names) != 2 || !es[2].UpdatedAt.Equal(t2)) {
		t.Errorf("workspace 1 reads %+v", es[2])
	}
}

// A skill two workspaces of one owner have on names that owner once.
func TestSkillOwnersOnce(t *testing.T) {
	f, c := newFake()
	f.visible = []entity.TeamWorkspace{{ID: 100, OwnerID: 1}, {ID: 101, OwnerID: 1}, {ID: 200, OwnerID: 2}}
	f.skillRows = []base.WorkspaceSkillRow{
		{WorkspaceID: 100, UserID: 1, Name: "deploy"}, {WorkspaceID: 101, UserID: 1, Name: "deploy"}, {WorkspaceID: 200, UserID: 2, Name: "deploy"},
	}
	ss, err := c.ListSkills(context.Background(), 2, 10)
	if err != nil || len(ss) != 1 || len(ss[0].Owners) != 2 {
		t.Errorf("the skills are %+v, %v: want deploy, owned by 1 and 2", ss, err)
	}
}
