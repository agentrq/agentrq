// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package team runs teams, their members, invitations and workspace shares,
// and records each change. The rules:
//
//   - A team is seen only by its members. To anyone else it does not exist:
//     base.ErrNotFound, as for an id nobody has.
//   - Every member is equal: any member renames the team, invites, revokes
//     an invitation, removes a member and deletes the team. Who may do what
//     is left to a system outside AgentRQ. The last member to leave deletes
//     the team.
//   - Every change names the team version it was made from, and is refused
//     with entity.TeamVersionError when another change landed first. An
//     invitee accepting or declining names none: they never loaded the team.
//   - Leaving or being removed unshares that member's workspaces from the
//     team.
//   - An invitation is accepted only by an account whose email is the invited
//     address, ignoring case, once, within 7 days, while its inviter is still
//     in the team. A team creates at most MaxInvitesPerDay invitations in any
//     24 hours, and an account at most MaxInvitesPerUserPerDay across all its
//     teams.
//   - Only a workspace's owner shares it, and only with a team they are in.
//     A fork is never shared on its own: it follows its parent.
package team

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/idgen"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/smtp"
)

// maxName is the longest team name, in characters.
const maxName = 128

// Controller runs teams for the signed-in account, userID. A change returns
// the team's version after it.
type Controller interface {
	ListTeams(ctx context.Context, userID int64) ([]entity.Team, error)
	CreateTeam(ctx context.Context, userID int64, name string) (entity.Team, error)
	GetTeam(ctx context.Context, userID, teamID int64) (entity.Team, error)
	RenameTeam(ctx context.Context, ch entity.TeamChange, name string) (entity.Team, error)
	DeleteTeam(ctx context.Context, ch entity.TeamChange) error

	ListMembers(ctx context.Context, userID, teamID int64) (entity.TeamMembers, error)
	// RemoveMember removes a member; removing yourself is leaving.
	RemoveMember(ctx context.Context, ch entity.TeamChange, memberID int64) (int64, error)
	LeaveTeam(ctx context.Context, ch entity.TeamChange) error

	ListTeamInvitations(ctx context.Context, userID, teamID int64) (entity.TeamInvitations, error)
	CreateInvitation(ctx context.Context, ch entity.TeamChange, email string) (entity.CreateTeamInvitationResponse, error)
	RevokeInvitation(ctx context.Context, ch entity.TeamChange, invitationID int64) (int64, error)
	// ListMyInvitations is the invitations waiting for the account's email.
	ListMyInvitations(ctx context.Context, userID int64) ([]entity.TeamInvitation, error)
	PreviewInvitation(ctx context.Context, userID int64, token string) (entity.TeamInvitationPreview, error)
	// AcceptInvitation accepts by the link's token; AcceptInvitationByID from
	// the account's own list. Either way the email must be the account's.
	AcceptInvitation(ctx context.Context, userID int64, token string) (entity.Team, error)
	AcceptInvitationByID(ctx context.Context, userID, invitationID int64) (entity.Team, error)
	DeclineInvitation(ctx context.Context, userID, invitationID int64) error

	// ListActivities is a page of the team's changes, newest first.
	ListActivities(ctx context.Context, req entity.ListTeamActivitiesRequest) (entity.TeamActivities, error)

	ListShares(ctx context.Context, userID, workspaceID int64) ([]entity.WorkspaceShare, error)
	ShareWorkspace(ctx context.Context, ch entity.TeamChange, workspaceID int64) (entity.WorkspaceShare, error)
	UnshareWorkspace(ctx context.Context, ch entity.TeamChange, workspaceID int64) (int64, error)

	// CanReadWorkspaceInTeam is the one check behind every team read: the
	// account is in the team, and the workspace, or the parent it is a fork
	// of, is shared with it, and neither is archived or deleted.
	CanReadWorkspaceInTeam(ctx context.Context, userID, teamID, workspaceID int64) (bool, error)
	// ListTeamWorkspaces is the workspaces CanReadWorkspaceInTeam allows in
	// the team, by shared name; base.ErrNotFound unless the account is in it.
	ListTeamWorkspaces(ctx context.Context, userID, teamID int64) ([]entity.TeamWorkspace, error)
}

// Repository is what the controller reads and writes.
type Repository interface {
	SystemGetUser(ctx context.Context, id int64) (model.User, error)
	GetWorkspace(ctx context.Context, id int64, userID int64) (model.Workspace, error)

	CreateTeam(ctx context.Context, team model.Team, first model.TeamMember, act model.TeamManagementActivity) error
	GetTeam(ctx context.Context, id int64) (model.Team, error)
	ListTeamsForUser(ctx context.Context, userID int64) ([]base.TeamWithCount, error)
	CountTeamMembers(ctx context.Context, teamID int64) (int64, error)
	RenameTeam(ctx context.Context, teamID, callerID, version int64, name string, act model.TeamManagementActivity) (int64, error)
	DeleteTeam(ctx context.Context, teamID, callerID, version int64, act model.TeamManagementActivity) error
	GetTeamMember(ctx context.Context, teamID, userID int64) (model.TeamMember, error)
	ListTeamMembers(ctx context.Context, teamID int64) ([]base.TeamMemberWithUser, error)
	TeamHasMemberWithEmail(ctx context.Context, teamID int64, email string) (bool, error)
	RemoveTeamMember(ctx context.Context, teamID, callerID, userID, version int64, act model.TeamManagementActivity, deletedID int64) (int64, bool, error)
	CreateTeamInvitation(ctx context.Context, inv model.TeamInvitation, version int64, since time.Time, teamLimit, userLimit int64, act model.TeamManagementActivity) (int64, error)
	ListTeamInvitations(ctx context.Context, teamID int64, now time.Time) ([]model.TeamInvitation, error)
	ListInvitationsForEmail(ctx context.Context, email string, now time.Time) ([]model.TeamInvitation, error)
	GetTeamInvitation(ctx context.Context, id int64) (model.TeamInvitation, error)
	GetTeamInvitationByTokenHash(ctx context.Context, tokenHash string) (model.TeamInvitation, error)
	RevokeTeamInvitation(ctx context.Context, teamID, callerID, version, id int64, act model.TeamManagementActivity) (int64, error)
	DeclineTeamInvitation(ctx context.Context, teamID, id int64, act model.TeamManagementActivity) error
	AcceptTeamInvitation(ctx context.Context, id int64, member model.TeamMember, now time.Time, act model.TeamManagementActivity) error
	SetWorkspaceShare(ctx context.Context, s model.WorkspaceShare, version int64, act model.TeamManagementActivity) (base.WorkspaceShareWithTeam, int64, error)
	ListWorkspaceShares(ctx context.Context, workspaceID, userID int64) ([]base.WorkspaceShareWithTeam, error)
	DeleteWorkspaceShare(ctx context.Context, workspaceID, teamID, userID, version int64, act model.TeamManagementActivity) (int64, error)
	ListTeamActivities(ctx context.Context, teamID int64, limit int, before int64) ([]base.TeamActivityRow, error)
	CanReadWorkspaceInTeam(ctx context.Context, userID, teamID, workspaceID int64) (bool, error)
	ListTeamWorkspaces(ctx context.Context, teamID int64) ([]base.TeamWorkspaceRow, error)
}

// Publisher carries telemetry.
type Publisher interface {
	Publish(ctx context.Context, req pubsub.PublishRequest) (*pubsub.PublishResponse, error)
}

// Params are a controller's dependencies.
type Params struct {
	Repository Repository
	IDGen      idgen.Service
	PubSub     Publisher
	// Mailer sends invitations; nil when no SMTP is configured, and the
	// inviter is then given the link to send.
	Mailer smtp.Service
	// BaseURL is the web app's, which invitation links start with.
	BaseURL string
	// MaxInvitesPerDay is how many invitations a team may create in any 24
	// hours; 0 turns invitations off.
	MaxInvitesPerDay int
	// MaxInvitesPerUserPerDay is how many invitations one account may create
	// in any 24 hours, across all its teams; 0 is no such cap.
	MaxInvitesPerUserPerDay int
	// Now is the clock; nil is time.Now in UTC.
	Now func() time.Time
}

type controller struct {
	repo      Repository
	ids       idgen.Service
	pubsub    Publisher
	mailer    smtp.Service
	baseURL   string
	maxInvite int
	maxByUser int
	now       func() time.Time
}

// New returns a teams controller.
func New(p Params) Controller {
	c := &controller{
		repo: p.Repository, ids: p.IDGen, pubsub: p.PubSub, mailer: p.Mailer,
		baseURL: strings.TrimRight(p.BaseURL, "/"), maxInvite: p.MaxInvitesPerDay,
		maxByUser: p.MaxInvitesPerUserPerDay, now: p.Now,
	}
	if c.now == nil {
		c.now = func() time.Time { return time.Now().UTC() }
	}
	return c
}

func teamErr(kind entity.TeamErrorKind, format string, a ...any) error {
	return &entity.TeamError{Kind: kind, Message: fmt.Sprintf(format, a...)}
}

// fromRepo turns a version conflict into the person's error, carrying the
// team's version now.
func fromRepo(err error) error {
	var vc *base.TeamVersionConflict
	if errors.As(err, &vc) {
		return &entity.TeamVersionError{Version: vc.Current}
	}
	return err
}

// cleanName is a team name trimmed, 1 to maxName characters, on one line: a
// team's name goes into the invitation mail sent in its name.
func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxName {
		return "", teamErr(entity.TeamInvalid, "a team name is 1 to %d characters", maxName)
	}
	if strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", teamErr(entity.TeamInvalid, "a team name is one line of text")
	}
	return name, nil
}

// activity is a new activity row, made now; the repository fills in what
// the change was.
func (c *controller) activity() model.TeamManagementActivity {
	return model.TeamManagementActivity{ID: c.ids.NextID(), CreatedAt: c.now()}
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

func (c *controller) ListTeams(ctx context.Context, userID int64) ([]entity.Team, error) {
	rows, err := c.repo.ListTeamsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]entity.Team, len(rows))
	for i, t := range rows {
		out[i] = entity.Team{ID: t.ID, Name: t.Name, Version: t.Version, MemberCount: t.MemberCount, CreatedAt: t.CreatedAt}
	}
	return out, nil
}

func (c *controller) CreateTeam(ctx context.Context, userID int64, name string) (entity.Team, error) {
	name, err := cleanName(name)
	if err != nil {
		return entity.Team{}, err
	}
	now := c.now()
	team := model.Team{ID: c.ids.NextID(), Name: name, CreatedAt: now, UpdatedAt: now}
	first := model.TeamMember{ID: c.ids.NextID(), TeamID: team.ID, UserID: userID, CreatedAt: now, UpdatedAt: now}
	act := c.activity()
	act.ActorUserID = userID
	if err := c.repo.CreateTeam(ctx, team, first, act); err != nil {
		return entity.Team{}, err
	}
	c.emit(ctx, entity.ActionTeamCreate, userID, 0, team.ID)
	return entity.Team{ID: team.ID, Name: name, Version: 1, MemberCount: 1, CreatedAt: now}, nil
}

// member is base.ErrNotFound unless the account is in the team.
func (c *controller) member(ctx context.Context, userID, teamID int64) error {
	_, err := c.repo.GetTeamMember(ctx, teamID, userID)
	return err
}

// team is the team as its members see it.
func (c *controller) team(ctx context.Context, teamID int64) (entity.Team, error) {
	t, err := c.repo.GetTeam(ctx, teamID)
	if err != nil {
		return entity.Team{}, err
	}
	n, err := c.repo.CountTeamMembers(ctx, teamID)
	if err != nil {
		return entity.Team{}, err
	}
	return entity.Team{ID: t.ID, Name: t.Name, Version: t.Version, MemberCount: n, CreatedAt: t.CreatedAt}, nil
}

func (c *controller) GetTeam(ctx context.Context, userID, teamID int64) (entity.Team, error) {
	if err := c.member(ctx, userID, teamID); err != nil {
		return entity.Team{}, err
	}
	return c.team(ctx, teamID)
}

// version is the team's version now; base.ErrNotFound when it is gone.
func (c *controller) version(ctx context.Context, teamID int64) (int64, error) {
	t, err := c.repo.GetTeam(ctx, teamID)
	return t.Version, err
}

// RenameTeam answers the team at the version the rename made, which a
// change landing right after must not move.
func (c *controller) RenameTeam(ctx context.Context, ch entity.TeamChange, name string) (entity.Team, error) {
	name, err := cleanName(name)
	if err != nil {
		return entity.Team{}, err
	}
	next, err := c.repo.RenameTeam(ctx, ch.TeamID, ch.UserID, ch.Version, name, c.activity())
	if err != nil {
		return entity.Team{}, fromRepo(err)
	}
	t, err := c.team(ctx, ch.TeamID)
	if err != nil {
		return entity.Team{}, err
	}
	t.Name, t.Version = name, next
	return t, nil
}

func (c *controller) DeleteTeam(ctx context.Context, ch entity.TeamChange) error {
	return fromRepo(c.repo.DeleteTeam(ctx, ch.TeamID, ch.UserID, ch.Version, c.activity()))
}

func (c *controller) ListMembers(ctx context.Context, userID, teamID int64) (entity.TeamMembers, error) {
	if err := c.member(ctx, userID, teamID); err != nil {
		return entity.TeamMembers{}, err
	}
	rows, err := c.repo.ListTeamMembers(ctx, teamID)
	if err != nil {
		return entity.TeamMembers{}, err
	}
	v, err := c.version(ctx, teamID)
	if err != nil {
		return entity.TeamMembers{}, err
	}
	out := entity.TeamMembers{Members: make([]entity.TeamMember, len(rows)), Version: v}
	for i, m := range rows {
		out.Members[i] = entity.TeamMember{UserID: m.UserID, Name: m.Name, Email: m.Email, Picture: m.Picture, JoinedAt: m.CreatedAt}
	}
	return out, nil
}

// RemoveMember leaves the membership checks to the repository, which reads
// them under the team's lock.
func (c *controller) RemoveMember(ctx context.Context, ch entity.TeamChange, memberID int64) (int64, error) {
	next, _, err := c.repo.RemoveTeamMember(ctx, ch.TeamID, ch.UserID, memberID, ch.Version, c.activity(), c.ids.NextID())
	return next, fromRepo(err)
}

func (c *controller) LeaveTeam(ctx context.Context, ch entity.TeamChange) error {
	_, err := c.RemoveMember(ctx, ch, ch.UserID)
	return err
}

// maxActivities is the most activities one page holds; defaultActivities a
// page that names no limit.
const (
	maxActivities     = 100
	defaultActivities = 20
)

func (c *controller) ListActivities(ctx context.Context, req entity.ListTeamActivitiesRequest) (entity.TeamActivities, error) {
	if err := c.member(ctx, req.UserID, req.TeamID); err != nil {
		return entity.TeamActivities{}, err
	}
	limit := req.Limit
	if limit <= 0 {
		limit = defaultActivities
	}
	rows, err := c.repo.ListTeamActivities(ctx, req.TeamID, min(limit, maxActivities), req.Before)
	if err != nil {
		return entity.TeamActivities{}, err
	}
	v, err := c.version(ctx, req.TeamID)
	if err != nil {
		return entity.TeamActivities{}, err
	}
	out := entity.TeamActivities{Activities: make([]entity.TeamActivity, len(rows)), Version: v}
	for i, r := range rows {
		a := entity.TeamActivity{
			ID: r.ID, Action: r.Action, CreatedAt: r.CreatedAt,
			Actor:   entity.TeamOwner{UserID: r.ActorUserID, Name: r.ActorName, Picture: r.ActorPicture},
			Details: map[string]string{},
		}
		if r.TargetUserID != nil {
			a.Target = entity.TeamOwner{UserID: *r.TargetUserID, Name: r.TargetName, Picture: r.TargetPicture}
		}
		if r.TargetEmail != nil {
			a.TargetEmail = *r.TargetEmail
		}
		if r.TargetWorkspaceID != nil {
			a.TargetWorkspaceID = *r.TargetWorkspaceID
		}
		// Written by this package only, as an object of strings.
		_ = json.Unmarshal([]byte(r.Details), &a.Details)
		out.Activities[i] = a
	}
	return out, nil
}
