// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package team runs teams, their members, invitations and workspace shares,
// and decides who may change them. The rules:
//
//   - A team is seen only by its members. To anyone else it does not exist:
//     base.ErrNotFound, as for an id nobody has.
//   - Owners and admins rename the team, change members' roles, remove
//     members and invite. Making someone an owner, or changing or removing an
//     owner, takes an owner. Only an owner deletes the team. A team always
//     keeps one owner.
//   - Anyone may leave; leaving or being removed unshares that member's
//     workspaces from the team.
//   - An invitation is accepted only by an account whose email is the invited
//     address, ignoring case, once, within 7 days. A team creates at most
//     MaxInvitesPerDay invitations in any 24 hours, and an account at most
//     MaxInvitesPerUserPerDay across all its teams.
//   - Only a workspace's owner shares it, and only with a team they are in.
//     A fork is never shared on its own: it follows its parent.
package team

import (
	"context"
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

// maxName is the longest team name or group, in characters.
const maxName = 128

// Controller runs teams for the signed-in account, userID.
type Controller interface {
	ListTeams(ctx context.Context, userID int64) ([]entity.Team, error)
	CreateTeam(ctx context.Context, userID int64, name string) (entity.Team, error)
	GetTeam(ctx context.Context, userID, teamID int64) (entity.Team, error)
	RenameTeam(ctx context.Context, userID, teamID int64, name string) (entity.Team, error)
	DeleteTeam(ctx context.Context, userID, teamID int64) error

	ListMembers(ctx context.Context, userID, teamID int64) ([]entity.TeamMember, error)
	SetMemberRole(ctx context.Context, userID, teamID, memberID int64, role string) error
	// RemoveMember removes a member; removing yourself is leaving.
	RemoveMember(ctx context.Context, userID, teamID, memberID int64) error
	LeaveTeam(ctx context.Context, userID, teamID int64) error

	ListTeamInvitations(ctx context.Context, userID, teamID int64) ([]entity.TeamInvitation, error)
	CreateInvitation(ctx context.Context, userID, teamID int64, email, role string) (entity.CreateTeamInvitationResponse, error)
	RevokeInvitation(ctx context.Context, userID, teamID, invitationID int64) error
	// ListMyInvitations is the invitations waiting for the account's email.
	ListMyInvitations(ctx context.Context, userID int64) ([]entity.TeamInvitation, error)
	PreviewInvitation(ctx context.Context, userID int64, token string) (entity.TeamInvitationPreview, error)
	// AcceptInvitation accepts by the link's token; AcceptInvitationByID from
	// the account's own list. Either way the email must be the account's.
	AcceptInvitation(ctx context.Context, userID int64, token string) (entity.Team, error)
	AcceptInvitationByID(ctx context.Context, userID, invitationID int64) (entity.Team, error)
	DeclineInvitation(ctx context.Context, userID, invitationID int64) error

	ListShares(ctx context.Context, userID, workspaceID int64) ([]entity.WorkspaceShare, error)
	ShareWorkspace(ctx context.Context, userID, workspaceID, teamID int64, group string) (entity.WorkspaceShare, error)
	UnshareWorkspace(ctx context.Context, userID, workspaceID, teamID int64) error

	// CanReadWorkspaceInTeam is the one check behind every team read: the
	// account is in the team, and the workspace, or the parent it is a fork
	// of, is shared with it, and neither is archived or deleted.
	CanReadWorkspaceInTeam(ctx context.Context, userID, teamID, workspaceID int64) (bool, error)
	// ListTeamWorkspaces is the workspaces CanReadWorkspaceInTeam allows in
	// the team, by group; base.ErrNotFound unless the account is in it.
	ListTeamWorkspaces(ctx context.Context, userID, teamID int64) ([]entity.TeamWorkspace, error)
}

// Repository is what the controller reads and writes.
type Repository interface {
	SystemGetUser(ctx context.Context, id int64) (model.User, error)
	GetWorkspace(ctx context.Context, id int64, userID int64) (model.Workspace, error)

	CreateTeam(ctx context.Context, team model.Team, owner model.TeamMember) error
	GetTeam(ctx context.Context, id int64) (model.Team, error)
	ListTeamsForUser(ctx context.Context, userID int64) ([]base.TeamWithRole, error)
	CountTeamMembers(ctx context.Context, teamID int64) (int64, error)
	RenameTeam(ctx context.Context, id int64, name string) error
	DeleteTeam(ctx context.Context, id, callerID int64) error
	GetTeamMember(ctx context.Context, teamID, userID int64) (model.TeamMember, error)
	ListTeamMembers(ctx context.Context, teamID int64) ([]base.TeamMemberWithUser, error)
	TeamHasMemberWithEmail(ctx context.Context, teamID int64, email string) (bool, error)
	SetTeamMemberRole(ctx context.Context, teamID, callerID, userID int64, role string) error
	RemoveTeamMember(ctx context.Context, teamID, callerID, userID int64) error
	CreateTeamInvitation(ctx context.Context, inv model.TeamInvitation, since time.Time, teamLimit, userLimit int64) error
	ListTeamInvitations(ctx context.Context, teamID int64, now time.Time) ([]model.TeamInvitation, error)
	ListInvitationsForEmail(ctx context.Context, email string, now time.Time) ([]model.TeamInvitation, error)
	GetTeamInvitation(ctx context.Context, id int64) (model.TeamInvitation, error)
	GetTeamInvitationByTokenHash(ctx context.Context, tokenHash string) (model.TeamInvitation, error)
	RevokeTeamInvitation(ctx context.Context, teamID, id int64, now time.Time) error
	AcceptTeamInvitation(ctx context.Context, id int64, member model.TeamMember, now time.Time) error
	SetWorkspaceShare(ctx context.Context, s model.WorkspaceShare) (base.WorkspaceShareWithTeam, error)
	ListWorkspaceShares(ctx context.Context, workspaceID, userID int64) ([]base.WorkspaceShareWithTeam, error)
	DeleteWorkspaceShare(ctx context.Context, workspaceID, teamID, userID int64) error
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

var (
	errForbidden = teamErr(entity.TeamForbidden, "your role in this team does not allow that")
	errLastOwner = teamErr(entity.TeamConflict, "a team keeps at least one owner: make someone else an owner first")
)

// fromRepo turns the repository's refusals into the person's messages.
func fromRepo(err error) error {
	switch {
	case errors.Is(err, base.ErrLastTeamOwner):
		return errLastOwner
	case errors.Is(err, base.ErrTeamRoleForbidden):
		return errForbidden
	}
	return err
}

func canManage(role string) bool { return role == model.TeamRoleOwner || role == model.TeamRoleAdmin }

func validRole(role string) bool { return canManage(role) || role == model.TeamRoleMember }

// cleanName is a team name or group trimmed, 1 to maxName characters, on
// one line: a team's name goes into the invitation mail sent in its name.
func cleanName(name, what string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxName {
		return "", teamErr(entity.TeamInvalid, "a %s is 1 to %d characters", what, maxName)
	}
	if strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", teamErr(entity.TeamInvalid, "a %s is one line of text", what)
	}
	return name, nil
}

// role is the account's role in the team; base.ErrNotFound when they are
// not in it, or there is no such team.
func (c *controller) role(ctx context.Context, userID, teamID int64) (string, error) {
	m, err := c.repo.GetTeamMember(ctx, teamID, userID)
	if err != nil {
		return "", err
	}
	return m.Role, nil
}

// manager is role for a change only owners and admins make.
func (c *controller) manager(ctx context.Context, userID, teamID int64) (string, error) {
	role, err := c.role(ctx, userID, teamID)
	if err != nil {
		return "", err
	}
	if !canManage(role) {
		return "", errForbidden
	}
	return role, nil
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
		out[i] = entity.Team{ID: t.ID, Name: t.Name, Role: t.Role, MemberCount: t.MemberCount, CreatedAt: t.CreatedAt}
	}
	return out, nil
}

func (c *controller) CreateTeam(ctx context.Context, userID int64, name string) (entity.Team, error) {
	name, err := cleanName(name, "team name")
	if err != nil {
		return entity.Team{}, err
	}
	now := c.now()
	team := model.Team{ID: c.ids.NextID(), Name: name, CreatedAt: now, UpdatedAt: now}
	owner := model.TeamMember{ID: c.ids.NextID(), TeamID: team.ID, UserID: userID, Role: model.TeamRoleOwner, CreatedAt: now, UpdatedAt: now}
	if err := c.repo.CreateTeam(ctx, team, owner); err != nil {
		return entity.Team{}, err
	}
	c.emit(ctx, entity.ActionTeamCreate, userID, 0, team.ID)
	return entity.Team{ID: team.ID, Name: name, Role: model.TeamRoleOwner, MemberCount: 1, CreatedAt: now}, nil
}

// team is the team as an account with role sees it.
func (c *controller) team(ctx context.Context, teamID int64, role string) (entity.Team, error) {
	t, err := c.repo.GetTeam(ctx, teamID)
	if err != nil {
		return entity.Team{}, err
	}
	n, err := c.repo.CountTeamMembers(ctx, teamID)
	if err != nil {
		return entity.Team{}, err
	}
	return entity.Team{ID: t.ID, Name: t.Name, Role: role, MemberCount: n, CreatedAt: t.CreatedAt}, nil
}

func (c *controller) GetTeam(ctx context.Context, userID, teamID int64) (entity.Team, error) {
	role, err := c.role(ctx, userID, teamID)
	if err != nil {
		return entity.Team{}, err
	}
	return c.team(ctx, teamID, role)
}

func (c *controller) RenameTeam(ctx context.Context, userID, teamID int64, name string) (entity.Team, error) {
	role, err := c.manager(ctx, userID, teamID)
	if err != nil {
		return entity.Team{}, err
	}
	if name, err = cleanName(name, "team name"); err != nil {
		return entity.Team{}, err
	}
	if err := c.repo.RenameTeam(ctx, teamID, name); err != nil {
		return entity.Team{}, err
	}
	return c.team(ctx, teamID, role)
}

func (c *controller) DeleteTeam(ctx context.Context, userID, teamID int64) error {
	return fromRepo(c.repo.DeleteTeam(ctx, teamID, userID))
}

func (c *controller) ListMembers(ctx context.Context, userID, teamID int64) ([]entity.TeamMember, error) {
	if _, err := c.role(ctx, userID, teamID); err != nil {
		return nil, err
	}
	rows, err := c.repo.ListTeamMembers(ctx, teamID)
	if err != nil {
		return nil, err
	}
	out := make([]entity.TeamMember, len(rows))
	for i, m := range rows {
		out[i] = entity.TeamMember{UserID: m.UserID, Name: m.Name, Email: m.Email, Picture: m.Picture, Role: m.Role, JoinedAt: m.CreatedAt}
	}
	return out, nil
}

// SetMemberRole leaves the role rules to the repository, which reads both
// roles under the team's lock.
func (c *controller) SetMemberRole(ctx context.Context, userID, teamID, memberID int64, role string) error {
	if !validRole(role) {
		return teamErr(entity.TeamInvalid, "a role is owner, admin or member")
	}
	return fromRepo(c.repo.SetTeamMemberRole(ctx, teamID, userID, memberID, role))
}

// RemoveMember leaves the role rules to the repository, which reads both
// roles under the team's lock.
func (c *controller) RemoveMember(ctx context.Context, userID, teamID, memberID int64) error {
	return fromRepo(c.repo.RemoveTeamMember(ctx, teamID, userID, memberID))
}

func (c *controller) LeaveTeam(ctx context.Context, userID, teamID int64) error {
	return c.RemoveMember(ctx, userID, teamID, userID)
}
