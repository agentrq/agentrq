// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package teamview is team mode: what a team's members read of the
// workspaces shared with it, and the two writes it allows: cloning a
// teammate's task into a workspace of your own, and adding a teammate's
// skill to your account, as a link to it.
//
// Who may read what is decided in exactly two places, both in
// controller/team:
//
//   - A read of one task (its detail, an attachment, a clone) passes
//     CanReadTasksInTeam for the task's own workspace first.
//   - A read across the team takes its workspaces from ListTeamWorkspaces,
//     which refuses anyone not in the team, and reads nothing else: tasks
//     only of those whose share lets the team read tasks, skills only of
//     those whose share lets it read skills.
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
	"strings"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/skill"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
	"github.com/mustafaturan/monoflake"
	zlog "github.com/rs/zerolog/log"
)

// skillFile is the file a teammate's skill is read by.
const skillFile = "SKILL.md"

// Controller is team mode for the signed-in account, userID.
type Controller interface {
	ListTasks(ctx context.Context, req entity.ListTeamTasksRequest) ([]entity.TeamTask, error)
	GetTask(ctx context.Context, userID, teamID, taskID int64) (entity.TeamTaskDetail, error)
	GetAttachment(ctx context.Context, userID, teamID, taskID int64, attachmentID string) (*entity.GetAttachmentResponse, error)
	// GetStats is a workspace's analytics summed over the team's workspaces,
	// or over one team workspace's.
	GetStats(ctx context.Context, req entity.GetTeamStatsRequest) (*entity.GetDetailedWorkspaceStatsResponse, error)
	// CloneTask creates a copy of a teammate's task in one of the caller's
	// workspaces, not started, through the normal task creation, noting
	// where it came from.
	CloneTask(ctx context.Context, req entity.CloneTeamTaskRequest) (*entity.CreateTaskResponse, error)

	// ListPanoDocs is the Pano docs shared with the team, without their
	// markdown. GetPanoDoc is one of them, read-only: its files' links are
	// the team's route, and it carries no comments and no tasks, which name
	// the owner's workspaces. GetPanoFile is one of its files.
	ListPanoDocs(ctx context.Context, userID, teamID int64) ([]entity.TeamPanoDoc, error)
	GetPanoDoc(ctx context.Context, userID, teamID, docID int64) (entity.TeamPanoDoc, error)
	GetPanoFile(ctx context.Context, userID, teamID, docID, fileID int64) (*entity.GetAttachmentResponse, error)

	// The skills on in one team workspace, named by workspace as
	// WorkspaceKey matches it; every team workspace's when it is empty.
	ListSkills(ctx context.Context, userID, teamID int64, workspace string) ([]entity.TeamSkill, error)
	GetSkill(ctx context.Context, userID, teamID int64, workspace, name string) (entity.TeamSkill, error)
	// AddSkill links a teammate's skill into the caller's account, on in the
	// given workspaces of theirs: their agents read and improve that one
	// skill, not a copy, while the team shares it.
	// With replace, the caller's own skill of that name becomes the link,
	// its files dropped.
	AddSkill(ctx context.Context, userID, teamID int64, workspace, name string, workspaceIDs []int64, replace bool) (entity.Skill, error)

	// ListTeammateMemories is the teammates in the team workspace of one of
	// the caller's own workspaces, and whether its agent's memory index
	// includes each. base.ErrNotFound for a workspace not the caller's.
	ListTeammateMemories(ctx context.Context, userID, workspaceID int64) ([]entity.TeammateMemory, error)
	// SetTeammateMemory includes a teammate in, or leaves them out of, that
	// index. base.ErrNotFound for a workspace not the caller's, or someone
	// not a teammate there.
	SetTeammateMemory(ctx context.Context, userID, workspaceID, teammateID int64, included bool) (entity.TeammateMemory, error)
}

// Teams is the team check, and the team a clone note names.
type Teams interface {
	GetTeam(ctx context.Context, userID, teamID int64) (entity.Team, error)
	CanReadTasksInTeam(ctx context.Context, userID, teamID, workspaceID int64) (bool, error)
	ListTeamWorkspaces(ctx context.Context, userID, teamID int64) ([]entity.TeamWorkspace, error)
}

// Owner is the owner's own reads, made here as the owner, and the creation
// of a task in the caller's workspace.
type Owner interface {
	GetTask(ctx context.Context, req entity.GetTaskRequest) (*entity.GetTaskResponse, error)
	GetTaskPlan(ctx context.Context, req entity.GetTaskPlanRequest) (*entity.GetTaskPlanResponse, error)
	GetAttachment(ctx context.Context, req entity.GetAttachmentRequest) (*entity.GetAttachmentResponse, error)
	CreateTask(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error)
	SystemListTasksInWorkspaces(ctx context.Context, req entity.ListTasksRequest, workspaceIDs []int64) (*entity.ListTasksResponse, error)
	SystemGetDetailedStatsOfWorkspaces(ctx context.Context, req entity.GetStatsOfWorkspacesRequest) (*entity.GetDetailedWorkspaceStatsResponse, error)
	SystemGetSkillFile(ctx context.Context, req entity.SystemGetSkillFileRequest) (*entity.GetSkillFileResponse, error)
	LinkSkill(ctx context.Context, req entity.LinkSkillRequest) (*entity.LinkSkillResponse, error)
	GetPanoDoc(ctx context.Context, req entity.GetPanoDocRequest) (*entity.GetPanoDocResponse, error)
	GetPanoFile(ctx context.Context, req entity.GetPanoFileRequest) (*entity.GetAttachmentResponse, error)
}

// Repository is what team mode reads and writes itself.
type Repository interface {
	SystemGetTask(ctx context.Context, id int64) (model.Task, error)
	TeamWorkspaceInfo(ctx context.Context, workspaceID int64) (base.TeamWorkspaceRow, error)
	ListSkillsOnInWorkspaces(ctx context.Context, workspaceIDs []int64) ([]base.WorkspaceSkillRow, error)
	GetWorkspace(ctx context.Context, id int64, userID int64) (model.Workspace, error)
	ListTeamPeerWorkspaces(ctx context.Context, workspaceID int64) ([]base.TeamWorkspacePeer, error)
	ListMemoryExclusions(ctx context.Context, userID, workspaceID int64) ([]int64, error)
	ExcludeTeammateMemory(ctx context.Context, e model.MemoryExclusion) error
	IncludeTeammateMemory(ctx context.Context, userID, workspaceID, teammateID int64) error
	MemoryUpdates(ctx context.Context, workspaceIDs []int64) (map[int64]time.Time, error)
	ListTeamPanoDocs(ctx context.Context, teamID int64, now time.Time) ([]base.TeamPanoDocRow, error)
	PanoDocOwnerInTeam(ctx context.Context, userID, teamID, docID int64) (base.PanoDocOwner, error)
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
	PubSub     Publisher
}

type controller struct {
	teams  Teams
	owner  Owner
	repo   Repository
	pubsub Publisher
}

// New returns team mode over p.
func New(p Params) Controller {
	return &controller{teams: p.Teams, owner: p.Owner, repo: p.Repository, pubsub: p.PubSub}
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

// readable is the workspace, with its owner and shared name, when the
// user reads its tasks there; base.ErrNotFound when not. Every read of one
// task starts here.
func (c *controller) readable(ctx context.Context, userID, teamID, workspaceID int64) (entity.TeamWorkspace, error) {
	ok, err := c.teams.CanReadTasksInTeam(ctx, userID, teamID, workspaceID)
	if err != nil {
		return entity.TeamWorkspace{}, err
	}
	if !ok {
		return entity.TeamWorkspace{}, base.ErrNotFound
	}
	w, err := c.repo.TeamWorkspaceInfo(ctx, workspaceID)
	if err != nil {
		return entity.TeamWorkspace{}, err
	}
	return entity.TeamWorkspace{
		ID: w.ID, Name: w.Name, ForkOfID: w.ForkOfID, SharedName: w.SharedName,
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

// WorkspaceKey is the key a team workspace is matched by: its shared name,
// ignoring case and surrounding spaces.
func WorkspaceKey(name string) string {
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
		if !w.Tasks || req.Workspace != "" && WorkspaceKey(w.SharedName) != WorkspaceKey(req.Workspace) {
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

func (c *controller) GetStats(ctx context.Context, req entity.GetTeamStatsRequest) (*entity.GetDetailedWorkspaceStatsResponse, error) {
	visible, err := c.teams.ListTeamWorkspaces(ctx, req.UserID, req.TeamID)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for _, w := range visible {
		if w.Tasks && (req.Workspace == "" || WorkspaceKey(w.SharedName) == WorkspaceKey(req.Workspace)) {
			ids = append(ids, w.ID)
		}
	}
	return c.owner.SystemGetDetailedStatsOfWorkspaces(ctx, entity.GetStatsOfWorkspacesRequest{
		WorkspaceIDs: ids, Range: req.Range, From: req.From, To: req.To,
	})
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
		out.Plan = p.Plan
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

// cloneAssignees are who a clone may be for; empty is a human.
var cloneAssignees = map[string]bool{"human": true, "agent": true}

func (c *controller) CloneTask(ctx context.Context, req entity.CloneTeamTaskRequest) (*entity.CreateTaskResponse, error) {
	t, w, err := c.readableTask(ctx, req.UserID, req.TeamID, req.TaskID)
	if err != nil {
		return nil, err
	}
	assignee := req.Assignee
	if assignee == "" {
		assignee = "human"
	}
	if !cloneAssignees[assignee] {
		return nil, invalid("a clone is for a human or an agent, not %q", req.Assignee)
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
			Status:      "notstarted",
			Assignee:    assignee,
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
	link := "/teams/" + monoflake.ID(team.ID).String() + "/workspaces/" + monoflake.ID(t.WorkspaceID).String() +
		"/tasks/" + monoflake.ID(t.ID).String()
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

// ownWorkspaces is the visible workspaces of the team workspace named
// workspace ("" = any) that keep their own skills and share them with the
// team: a fork reads its parent's, which the team sees whenever it sees the
// fork.
func ownWorkspaces(visible []entity.TeamWorkspace, workspace string) (map[int64]entity.TeamWorkspace, []int64) {
	byID := make(map[int64]entity.TeamWorkspace, len(visible))
	ids := []int64{}
	for _, w := range visible {
		if w.ForkOfID == 0 && w.Skills && (workspace == "" || WorkspaceKey(w.SharedName) == WorkspaceKey(workspace)) {
			byID[w.ID] = w
			ids = append(ids, w.ID)
		}
	}
	return byID, ids
}

// teamSkills is the skills on in the team workspace named workspace ("" =
// every one), by name, each with its owners, and the row of the owner who
// changed it last first.
func (c *controller) teamSkills(ctx context.Context, userID, teamID int64, workspace string) ([]entity.TeamSkill, map[string][]base.WorkspaceSkillRow, error) {
	visible, err := c.teams.ListTeamWorkspaces(ctx, userID, teamID)
	if err != nil {
		return nil, nil, err
	}
	byID, ids := ownWorkspaces(visible, workspace)
	rows, err := c.repo.ListSkillsOnInWorkspaces(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	byName := map[string][]base.WorkspaceSkillRow{}
	at := map[string]int{}
	out := []entity.TeamSkill{}
	for _, r := range rows {
		i, ok := at[r.Name]
		if !ok {
			// Rows come by name, the latest change first.
			i = len(out)
			at[r.Name] = i
			out = append(out, entity.TeamSkill{Name: r.Name, Description: r.Description, UpdatedAt: r.UpdatedAt})
		}
		byName[r.Name] = append(byName[r.Name], r)
		s := &out[i]
		if !hasOwner(s.Owners, r.UserID) {
			w := byID[r.WorkspaceID]
			s.Owners = append(s.Owners, entity.TeamOwner{UserID: w.OwnerID, Name: w.OwnerName, Picture: w.OwnerPicture})
		}
	}
	return out, byName, nil
}

func hasOwner(owners []entity.TeamOwner, userID int64) bool {
	for _, o := range owners {
		if o.UserID == userID {
			return true
		}
	}
	return false
}

func (c *controller) ListSkills(ctx context.Context, userID, teamID int64, workspace string) ([]entity.TeamSkill, error) {
	out, _, err := c.teamSkills(ctx, userID, teamID, workspace)
	return out, err
}

// teamSkill is the team's skill by name, and its owners' rows, the one
// changed last first: that is the copy the team reads. base.ErrNotFound when
// no shared workspace of the team workspace has it on.
func (c *controller) teamSkill(ctx context.Context, userID, teamID int64, workspace, name string) (entity.TeamSkill, []base.WorkspaceSkillRow, error) {
	canonical, err := skill.CanonicalName(name)
	if err != nil {
		return entity.TeamSkill{}, nil, base.ErrNotFound
	}
	skills, byName, err := c.teamSkills(ctx, userID, teamID, workspace)
	if err != nil {
		return entity.TeamSkill{}, nil, err
	}
	for _, s := range skills {
		if s.Name == canonical {
			return s, byName[canonical], nil
		}
	}
	return entity.TeamSkill{}, nil, base.ErrNotFound
}

func (c *controller) GetSkill(ctx context.Context, userID, teamID int64, workspace, name string) (entity.TeamSkill, error) {
	s, rows, err := c.teamSkill(ctx, userID, teamID, workspace, name)
	if err != nil {
		return entity.TeamSkill{}, err
	}
	f, err := c.owner.SystemGetSkillFile(ctx, entity.SystemGetSkillFileRequest{UserID: rows[0].UserID, Name: s.Name, Path: skillFile})
	if err != nil {
		return entity.TeamSkill{}, err
	}
	s.Content = f.File.Content
	return s, nil
}

// AddSkill links the copy changed last that is not the caller's own; with
// replace, the caller's own copy becomes that link.
func (c *controller) AddSkill(ctx context.Context, userID, teamID int64, workspace, name string, workspaceIDs []int64, replace bool) (entity.Skill, error) {
	_, rows, err := c.teamSkill(ctx, userID, teamID, workspace, name)
	if err != nil {
		return entity.Skill{}, err
	}
	src := rows[0]
	for _, r := range rows {
		if r.UserID != userID {
			src = r
			break
		}
	}
	rs, err := c.owner.LinkSkill(ctx, entity.LinkSkillRequest{
		SkillID: src.SkillID, UserID: monoflake.ID(userID).String(), WorkspaceIDs: workspaceIDs, Replace: replace,
	})
	if err != nil {
		return entity.Skill{}, err
	}
	c.emit(ctx, entity.ActionTeamSkillAdd, userID, 0, rs.Skill.ID)
	return rs.Skill, nil
}
