// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package teamview is team mode: what a team's members read of the
// workspaces shared with it, and the three writes it allows: cloning a
// teammate's task into a workspace of your own, the team's own memory, and
// adding a teammate's skill to your account.
//
// Who may read what is decided in exactly two places, both in
// controller/team:
//
//   - A read of one workspace's row (a task, an attachment, a memory) passes
//     CanReadWorkspaceInTeam for the row's own workspace first.
//   - A read across the team takes its workspaces from ListTeamWorkspaces,
//     which refuses anyone not in the team, and reads nothing else.
//
// Past that, the owner's own read is reused, made as the owner, so a team
// sees a task, a memory or a fork exactly as its owner does. Anything the
// team cannot see answers base.ErrNotFound, never "forbidden".
package teamview

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/idgen"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/skill"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
	"github.com/mustafaturan/monoflake"
	zlog "github.com/rs/zerolog/log"
)

// MemoryName is the team's one memory file.
const MemoryName = "memory.md"

// skillFile is the file a teammate's skill is read by.
const skillFile = "SKILL.md"

// Controller is team mode for the signed-in account, userID.
type Controller interface {
	ListTasks(ctx context.Context, req entity.ListTeamTasksRequest) ([]entity.TeamTask, error)
	GetTask(ctx context.Context, userID, teamID, taskID int64) (entity.TeamTaskDetail, error)
	GetAttachment(ctx context.Context, userID, teamID, taskID int64, attachmentID string) (*entity.GetAttachmentResponse, error)
	GetStats(ctx context.Context, req entity.GetTeamStatsRequest) (*entity.GetStatsOfWorkspacesResponse, error)
	// CloneTask creates a copy of a teammate's task in one of the caller's
	// workspaces, through the normal task creation, noting where it came
	// from.
	CloneTask(ctx context.Context, req entity.CloneTeamTaskRequest) (*entity.CreateTaskResponse, error)

	GetMemory(ctx context.Context, userID, teamID int64) (entity.TeamMemory, error)
	// SaveMemory saves the team's memory: the last save wins, and Conflict
	// says it replaced one saved after BaseUpdatedAt.
	SaveMemory(ctx context.Context, req entity.SaveTeamMemoryRequest) (entity.TeamMemory, error)
	MemoryIndex(ctx context.Context, userID, teamID int64) ([]entity.TeamMemoryIndexEntry, error)
	GetMemberMemory(ctx context.Context, userID, teamID, workspaceID int64, name string) (entity.Memory, error)

	ListSkills(ctx context.Context, userID, teamID int64) ([]entity.TeamSkill, error)
	GetSkill(ctx context.Context, userID, teamID int64, name string) (entity.TeamSkill, error)
	// AddSkill copies a teammate's skill into the caller's account, on in
	// the given workspaces of theirs.
	AddSkill(ctx context.Context, userID, teamID int64, name string, workspaceIDs []int64) (entity.Skill, error)
}

// Teams is the team check, and the team a clone note names.
type Teams interface {
	GetTeam(ctx context.Context, userID, teamID int64) (entity.Team, error)
	CanReadWorkspaceInTeam(ctx context.Context, userID, teamID, workspaceID int64) (bool, error)
	ListTeamWorkspaces(ctx context.Context, userID, teamID int64) ([]entity.TeamWorkspace, error)
}

// Owner is the owner's own reads, made here as the owner, and the creation
// of a task in the caller's workspace.
type Owner interface {
	GetTask(ctx context.Context, req entity.GetTaskRequest) (*entity.GetTaskResponse, error)
	GetTaskPlan(ctx context.Context, req entity.GetTaskPlanRequest) (*entity.GetTaskPlanResponse, error)
	GetAttachment(ctx context.Context, req entity.GetAttachmentRequest) (*entity.GetAttachmentResponse, error)
	GetMemory(ctx context.Context, req entity.GetMemoryRequest) (*entity.GetMemoryResponse, error)
	CreateTask(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error)
	SystemListTasksInWorkspaces(ctx context.Context, req entity.ListTasksRequest, workspaceIDs []int64) (*entity.ListTasksResponse, error)
	SystemGetDetailedStatsOfWorkspaces(ctx context.Context, req entity.GetStatsOfWorkspacesRequest) (*entity.GetStatsOfWorkspacesResponse, error)
	SystemGetSkillFile(ctx context.Context, req entity.SystemGetSkillFileRequest) (*entity.GetSkillFileResponse, error)
	CopySkill(ctx context.Context, req entity.CopySkillRequest) (*entity.CopySkillResponse, error)
}

// Repository is what team mode reads and writes itself.
type Repository interface {
	SystemGetTask(ctx context.Context, id int64) (model.Task, error)
	SystemGetUser(ctx context.Context, id int64) (model.User, error)
	TeamWorkspaceInfo(ctx context.Context, teamID, workspaceID int64) (base.TeamWorkspaceRow, error)
	GetTeamMemory(ctx context.Context, teamID int64, name string) (model.TeamMemory, error)
	SaveTeamMemory(ctx context.Context, mem model.TeamMemory) (model.TeamMemory, bool, error)
	ListWorkspaceMemoryNames(ctx context.Context, workspaceIDs []int64) ([]base.WorkspaceMemoryRow, error)
	ListSkillsOnInWorkspaces(ctx context.Context, workspaceIDs []int64) ([]base.WorkspaceSkillRow, error)
}

// Publisher carries telemetry.
type Publisher interface {
	Publish(ctx context.Context, req pubsub.PublishRequest) (*pubsub.PublishResponse, error)
}

// Params are a controller's dependencies.
type Params struct {
	Teams      Teams
	Owner      Owner
	Repository Repository
	IDGen      idgen.Service
	PubSub     Publisher
	// MaxMemoryBytes caps the team's memory, as a workspace's is capped.
	MaxMemoryBytes int
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

type controller struct {
	teams     Teams
	owner     Owner
	repo      Repository
	ids       idgen.Service
	pubsub    Publisher
	maxMemory int
	now       func() time.Time
}

// New returns team mode over p.
func New(p Params) Controller {
	now := p.Now
	if now == nil {
		now = time.Now
	}
	return &controller{teams: p.Teams, owner: p.Owner, repo: p.Repository, ids: p.IDGen, pubsub: p.PubSub, maxMemory: p.MaxMemoryBytes, now: now}
}

func invalid(format string, args ...any) error {
	return &entity.TeamError{Kind: entity.TeamInvalid, Message: fmt.Sprintf(format, args...)}
}

func (c *controller) emit(ctx context.Context, action entity.Action, userID, workspaceID, resourceID int64) {
	if c.pubsub == nil {
		return
	}
	_, _ = c.pubsub.Publish(ctx, pubsub.PublishRequest{PubSubID: entity.PubSubTopicCRUD, Event: entity.CRUDEvent{
		Action: action, WorkspaceID: workspaceID, UserID: userID, ResourceType: entity.ResourceTeam,
		ResourceID: resourceID, Actor: entity.ActorHuman, Origin: entity.OriginAPI,
	}})
}

// readable is the workspace, with its owner and group in the team, when the
// user reads it there; base.ErrNotFound when not. Every read of one
// workspace's row starts here.
func (c *controller) readable(ctx context.Context, userID, teamID, workspaceID int64) (entity.TeamWorkspace, error) {
	ok, err := c.teams.CanReadWorkspaceInTeam(ctx, userID, teamID, workspaceID)
	if err != nil {
		return entity.TeamWorkspace{}, err
	}
	if !ok {
		return entity.TeamWorkspace{}, base.ErrNotFound
	}
	w, err := c.repo.TeamWorkspaceInfo(ctx, teamID, workspaceID)
	if err != nil {
		return entity.TeamWorkspace{}, err
	}
	return entity.TeamWorkspace{
		ID: w.ID, Name: w.Name, ForkOfID: w.ForkOfID, Group: w.GroupName,
		OwnerID: w.UserID, OwnerName: w.OwnerName, OwnerPicture: w.OwnerPicture,
	}, nil
}

// readableTask is a task the user reads in the team, and its workspace.
func (c *controller) readableTask(ctx context.Context, userID, teamID, taskID int64) (model.Task, entity.TeamWorkspace, error) {
	t, err := c.repo.SystemGetTask(ctx, taskID)
	if err != nil {
		return model.Task{}, entity.TeamWorkspace{}, err
	}
	w, err := c.readable(ctx, userID, teamID, t.WorkspaceID)
	return t, w, err
}

// groupKey is the key a group is matched by: its name, ignoring case and
// surrounding spaces.
func groupKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// ownerID is the owner's id as the owner's own reads take it.
func ownerID(w entity.TeamWorkspace) string {
	return monoflake.ID(w.OwnerID).String()
}

func (c *controller) ListTasks(ctx context.Context, req entity.ListTeamTasksRequest) ([]entity.TeamTask, error) {
	visible, err := c.teams.ListTeamWorkspaces(ctx, req.UserID, req.TeamID)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]entity.TeamWorkspace, len(visible))
	ids := make([]int64, 0, len(visible))
	for _, w := range visible {
		if req.Group != "" && groupKey(w.Group) != groupKey(req.Group) {
			continue
		}
		if req.Member != 0 && w.OwnerID != req.Member {
			continue
		}
		byID[w.ID] = w
		ids = append(ids, w.ID)
	}
	out := []entity.TeamTask{}
	if len(ids) == 0 {
		return out, nil
	}
	rs, err := c.owner.SystemListTasksInWorkspaces(ctx, entity.ListTasksRequest{
		Status: req.Status, Limit: req.Limit, Offset: req.Offset, PreloadMessages: true, PreloadAgents: true,
	}, ids)
	if err != nil {
		return nil, err
	}
	for _, t := range rs.Tasks {
		out = append(out, entity.TeamTask{Task: t, Workspace: byID[t.WorkspaceID]})
	}
	return out, nil
}

func (c *controller) GetTask(ctx context.Context, userID, teamID, taskID int64) (entity.TeamTaskDetail, error) {
	t, w, err := c.readableTask(ctx, userID, teamID, taskID)
	if err != nil {
		return entity.TeamTaskDetail{}, err
	}
	rs, err := c.owner.GetTask(ctx, entity.GetTaskRequest{WorkspaceID: t.WorkspaceID, TaskID: t.ID, UserID: ownerID(w)})
	if err != nil {
		return entity.TeamTaskDetail{}, err
	}
	// The owner's Pano is theirs alone: a teammate sees no doc of it.
	rs.PanoDocs = nil
	out := entity.TeamTaskDetail{Detail: *rs, Workspace: w}
	switch p, err := c.owner.GetTaskPlan(ctx, entity.GetTaskPlanRequest{WorkspaceID: t.WorkspaceID, TaskID: t.ID, UserID: ownerID(w)}); {
	case err == nil:
		out.Plan = &p.Plan
	case !errors.Is(err, base.ErrNotFound):
		return entity.TeamTaskDetail{}, err
	}
	return out, nil
}

func (c *controller) GetAttachment(ctx context.Context, userID, teamID, taskID int64, attachmentID string) (*entity.GetAttachmentResponse, error) {
	t, w, err := c.readableTask(ctx, userID, teamID, taskID)
	if err != nil {
		return nil, err
	}
	return c.owner.GetAttachment(ctx, entity.GetAttachmentRequest{WorkspaceID: t.WorkspaceID, TaskID: t.ID, AttachmentID: attachmentID, UserID: ownerID(w)})
}

func (c *controller) GetStats(ctx context.Context, req entity.GetTeamStatsRequest) (*entity.GetStatsOfWorkspacesResponse, error) {
	visible, err := c.teams.ListTeamWorkspaces(ctx, req.UserID, req.TeamID)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for _, w := range visible {
		if req.Group == "" || groupKey(w.Group) == groupKey(req.Group) {
			ids = append(ids, w.ID)
		}
	}
	return c.owner.SystemGetDetailedStatsOfWorkspaces(ctx, entity.GetStatsOfWorkspacesRequest{
		WorkspaceIDs: ids, Range: req.Range, From: req.From, To: req.To,
	})
}

// cloneStatuses are what a clone may start as.
var cloneStatuses = map[string]bool{"notstarted": true, "ongoing": true}

func (c *controller) CloneTask(ctx context.Context, req entity.CloneTeamTaskRequest) (*entity.CreateTaskResponse, error) {
	t, w, err := c.readableTask(ctx, req.UserID, req.TeamID, req.TaskID)
	if err != nil {
		return nil, err
	}
	status := req.Status
	if status == "" {
		status = "notstarted"
	}
	if !cloneStatuses[status] {
		return nil, invalid("a clone starts as notstarted or ongoing, not %q", req.Status)
	}
	team, err := c.teams.GetTeam(ctx, req.UserID, req.TeamID)
	if err != nil {
		return nil, err
	}
	rs, err := c.owner.CreateTask(ctx, entity.CreateTaskRequest{
		UserID: monoflake.ID(req.UserID).String(),
		Task: entity.Task{
			WorkspaceID: req.WorkspaceID,
			Title:       t.Title,
			Body:        cloneBody(t, w, team),
			Status:      status,
			Assignee:    "agent",
			CreatedBy:   "human",
			Attachments: c.cloneAttachments(ctx, t, w),
		},
	})
	if err != nil {
		return nil, err
	}
	c.emit(ctx, entity.ActionTeamTaskClone, req.UserID, req.WorkspaceID, rs.Task.ID)
	return rs, nil
}

// cloneBody is the task's body, with a note of where the clone came from
// that links back to the original in team mode.
func cloneBody(t model.Task, w entity.TeamWorkspace, team entity.Team) string {
	link := "/workspaces/" + monoflake.ID(t.WorkspaceID).String() + "/tasks/" + monoflake.ID(t.ID).String() +
		"?team=" + monoflake.ID(team.ID).String()
	note := fmt.Sprintf("Cloned from [%s / %s / %s](%s)", team.Name, w.OwnerName, w.Name, link)
	if strings.TrimSpace(t.Body) == "" {
		return note
	}
	return t.Body + "\n\n---\n" + note
}

// cloneAttachments reads the files attached to the task itself, as its
// owner, for the clone to store as its own, each typed as one a browser cannot
// run. A file that can no longer be read — attachments are cleaned up after a
// while — is left out rather than failing the clone, as a fork does.
func (c *controller) cloneAttachments(ctx context.Context, t model.Task, w entity.TeamWorkspace) []entity.Attachment {
	var atts []entity.Attachment
	if len(t.Attachments) == 0 || json.Unmarshal(t.Attachments, &atts) != nil {
		return nil
	}
	out := make([]entity.Attachment, 0, len(atts))
	for _, a := range atts {
		f, err := c.owner.GetAttachment(ctx, entity.GetAttachmentRequest{WorkspaceID: t.WorkspaceID, TaskID: t.ID, AttachmentID: a.ID, UserID: ownerID(w)})
		if err != nil {
			zlog.Warn().Err(err).Str("attachmentID", a.ID).Msg("[teams] clone: attachment file missing, skipped")
			continue
		}
		out = append(out, entity.Attachment{Filename: a.Filename, MimeType: storage.SafeContentType(a.MimeType), Data: base64.StdEncoding.EncodeToString(f.Data)})
	}
	return out
}

// teamMemory is the stored memory as the team reads it, with its writer.
func (c *controller) teamMemory(ctx context.Context, m model.TeamMemory) entity.TeamMemory {
	out := entity.TeamMemory{Content: m.Content, UpdatedAt: m.UpdatedAt, UpdatedBy: m.UpdatedBy}
	// A writer whose account is gone is still named by id.
	if u, err := c.repo.SystemGetUser(ctx, m.UpdatedBy); err == nil {
		out.UpdatedByName, out.UpdatedByPicture = u.Name, u.Picture
	}
	return out
}

func (c *controller) GetMemory(ctx context.Context, userID, teamID int64) (entity.TeamMemory, error) {
	if _, err := c.teams.GetTeam(ctx, userID, teamID); err != nil {
		return entity.TeamMemory{}, err
	}
	m, err := c.repo.GetTeamMemory(ctx, teamID, MemoryName)
	if errors.Is(err, base.ErrNotFound) {
		return entity.TeamMemory{}, nil
	}
	if err != nil {
		return entity.TeamMemory{}, err
	}
	return c.teamMemory(ctx, m), nil
}

func (c *controller) SaveMemory(ctx context.Context, req entity.SaveTeamMemoryRequest) (entity.TeamMemory, error) {
	if n := len(req.Content); n > c.maxMemory {
		return entity.TeamMemory{}, invalid("the team's memory is %d bytes; the limit is %d bytes (%d KiB)", n, c.maxMemory, c.maxMemory/1024)
	}
	// Microseconds are what Postgres keeps, so the time handed back is the
	// one the next save compares against.
	now := c.now().UTC().Truncate(time.Microsecond)
	m := model.TeamMemory{
		ID: c.ids.NextID(), CreatedAt: now, UpdatedAt: now,
		TeamID: req.TeamID, Name: MemoryName, Content: req.Content, UpdatedBy: req.UserID,
	}
	prev, found, err := c.repo.SaveTeamMemory(ctx, m)
	if err != nil {
		return entity.TeamMemory{}, err
	}
	out := c.teamMemory(ctx, m)
	// A save with no base replaced one it never loaded, as a first save
	// does when another member saved first.
	out.Conflict = found && (req.BaseUpdatedAt.IsZero() || prev.UpdatedAt.After(req.BaseUpdatedAt))
	c.emit(ctx, entity.ActionTeamMemorySave, req.UserID, 0, req.TeamID)
	return out, nil
}

// ownWorkspaces is the visible workspaces that keep their own memories and
// skills: a fork reads its parent's, which the team sees whenever it sees
// the fork.
func ownWorkspaces(visible []entity.TeamWorkspace) (map[int64]entity.TeamWorkspace, []int64) {
	byID := make(map[int64]entity.TeamWorkspace, len(visible))
	ids := []int64{}
	for _, w := range visible {
		if w.ForkOfID == 0 {
			byID[w.ID] = w
			ids = append(ids, w.ID)
		}
	}
	return byID, ids
}

func (c *controller) MemoryIndex(ctx context.Context, userID, teamID int64) ([]entity.TeamMemoryIndexEntry, error) {
	visible, err := c.teams.ListTeamWorkspaces(ctx, userID, teamID)
	if err != nil {
		return nil, err
	}
	byID, ids := ownWorkspaces(visible)
	rows, err := c.repo.ListWorkspaceMemoryNames(ctx, ids)
	if err != nil {
		return nil, err
	}
	at := map[int64]int{}
	out := []entity.TeamMemoryIndexEntry{}
	for _, r := range rows {
		i, ok := at[r.WorkspaceID]
		if !ok {
			i = len(out)
			at[r.WorkspaceID] = i
			out = append(out, entity.TeamMemoryIndexEntry{Workspace: byID[r.WorkspaceID]})
		}
		e := &out[i]
		e.Names = append(e.Names, r.Name)
		if r.UpdatedAt.After(e.UpdatedAt) {
			e.UpdatedAt = r.UpdatedAt
		}
	}
	// By group, then owner, then workspace.
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Workspace, out[j].Workspace
		if ka, kb := groupKey(a.Group), groupKey(b.Group); ka != kb {
			return ka < kb
		}
		if ka, kb := strings.ToLower(a.OwnerName), strings.ToLower(b.OwnerName); ka != kb {
			return ka < kb
		}
		return a.ID < b.ID
	})
	return out, nil
}

func (c *controller) GetMemberMemory(ctx context.Context, userID, teamID, workspaceID int64, name string) (entity.Memory, error) {
	w, err := c.readable(ctx, userID, teamID, workspaceID)
	if err != nil {
		return entity.Memory{}, err
	}
	rs, err := c.owner.GetMemory(ctx, entity.GetMemoryRequest{WorkspaceID: workspaceID, UserID: ownerID(w), Name: name})
	if err != nil {
		return entity.Memory{}, err
	}
	return rs.Memory, nil
}

// teamSkills is the skills on in the team's workspaces, by name, each with
// its owners, and the row of the owner who changed it last.
func (c *controller) teamSkills(ctx context.Context, userID, teamID int64) ([]entity.TeamSkill, map[string]base.WorkspaceSkillRow, error) {
	visible, err := c.teams.ListTeamWorkspaces(ctx, userID, teamID)
	if err != nil {
		return nil, nil, err
	}
	byID, ids := ownWorkspaces(visible)
	rows, err := c.repo.ListSkillsOnInWorkspaces(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	latest := map[string]base.WorkspaceSkillRow{}
	at := map[string]int{}
	out := []entity.TeamSkill{}
	for _, r := range rows {
		i, ok := at[r.Name]
		if !ok {
			// Rows come by name, the latest change first.
			i = len(out)
			at[r.Name] = i
			latest[r.Name] = r
			out = append(out, entity.TeamSkill{Name: r.Name, Description: r.Description, UpdatedAt: r.UpdatedAt})
		}
		s := &out[i]
		if !hasOwner(s.Owners, r.UserID) {
			w := byID[r.WorkspaceID]
			s.Owners = append(s.Owners, entity.TeamOwner{UserID: w.OwnerID, Name: w.OwnerName, Picture: w.OwnerPicture})
		}
	}
	return out, latest, nil
}

func hasOwner(owners []entity.TeamOwner, userID int64) bool {
	for _, o := range owners {
		if o.UserID == userID {
			return true
		}
	}
	return false
}

func (c *controller) ListSkills(ctx context.Context, userID, teamID int64) ([]entity.TeamSkill, error) {
	out, _, err := c.teamSkills(ctx, userID, teamID)
	return out, err
}

// teamSkill is the team's skill by name, and whose copy is read: the one
// changed last. base.ErrNotFound when no shared workspace has it on.
func (c *controller) teamSkill(ctx context.Context, userID, teamID int64, name string) (entity.TeamSkill, base.WorkspaceSkillRow, error) {
	canonical, err := skill.CanonicalName(name)
	if err != nil {
		return entity.TeamSkill{}, base.WorkspaceSkillRow{}, base.ErrNotFound
	}
	skills, latest, err := c.teamSkills(ctx, userID, teamID)
	if err != nil {
		return entity.TeamSkill{}, base.WorkspaceSkillRow{}, err
	}
	for _, s := range skills {
		if s.Name == canonical {
			return s, latest[canonical], nil
		}
	}
	return entity.TeamSkill{}, base.WorkspaceSkillRow{}, base.ErrNotFound
}

func (c *controller) GetSkill(ctx context.Context, userID, teamID int64, name string) (entity.TeamSkill, error) {
	s, src, err := c.teamSkill(ctx, userID, teamID, name)
	if err != nil {
		return entity.TeamSkill{}, err
	}
	f, err := c.owner.SystemGetSkillFile(ctx, entity.SystemGetSkillFileRequest{UserID: src.UserID, Name: s.Name, Path: skillFile})
	if err != nil {
		return entity.TeamSkill{}, err
	}
	s.Content = f.File.Content
	return s, nil
}

func (c *controller) AddSkill(ctx context.Context, userID, teamID int64, name string, workspaceIDs []int64) (entity.Skill, error) {
	s, src, err := c.teamSkill(ctx, userID, teamID, name)
	if err != nil {
		return entity.Skill{}, err
	}
	rs, err := c.owner.CopySkill(ctx, entity.CopySkillRequest{
		FromUserID: src.UserID, Name: s.Name, UserID: monoflake.ID(userID).String(), WorkspaceIDs: workspaceIDs,
	})
	if err != nil {
		return entity.Skill{}, err
	}
	c.emit(ctx, entity.ActionTeamSkillAdd, userID, 0, rs.Skill.ID)
	return rs.Skill, nil
}
