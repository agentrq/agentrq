// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package team

import (
	"context"
	"errors"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
)

// failing is the real repository with one method failing.
type failing struct {
	Repository
	method string
}

func (f failing) ListTeamsForUser(ctx context.Context, userID int64) ([]base.TeamWithRole, error) {
	if f.method == "ListTeamsForUser" {
		return nil, errDB
	}
	return f.Repository.ListTeamsForUser(ctx, userID)
}

func (f failing) CreateTeam(ctx context.Context, team model.Team, owner model.TeamMember) error {
	if f.method == "CreateTeam" {
		return errDB
	}
	return f.Repository.CreateTeam(ctx, team, owner)
}

func (f failing) GetTeam(ctx context.Context, id int64) (model.Team, error) {
	if f.method == "GetTeam" {
		return model.Team{}, errDB
	}
	return f.Repository.GetTeam(ctx, id)
}

func (f failing) CountTeamMembers(ctx context.Context, teamID int64) (int64, error) {
	if f.method == "CountTeamMembers" {
		return 0, errDB
	}
	return f.Repository.CountTeamMembers(ctx, teamID)
}

func (f failing) RenameTeam(ctx context.Context, id int64, name string) error {
	if f.method == "RenameTeam" {
		return errDB
	}
	return f.Repository.RenameTeam(ctx, id, name)
}

func (f failing) ListTeamMembers(ctx context.Context, teamID int64) ([]base.TeamMemberWithUser, error) {
	if f.method == "ListTeamMembers" {
		return nil, errDB
	}
	return f.Repository.ListTeamMembers(ctx, teamID)
}

func (f failing) TeamHasMemberWithEmail(ctx context.Context, teamID int64, email string) (bool, error) {
	if f.method == "TeamHasMemberWithEmail" {
		return false, errDB
	}
	return f.Repository.TeamHasMemberWithEmail(ctx, teamID, email)
}

func (f failing) CreateTeamInvitation(ctx context.Context, inv model.TeamInvitation, since time.Time, teamLimit, userLimit int64) error {
	if f.method == "CreateTeamInvitation" {
		return errDB
	}
	return f.Repository.CreateTeamInvitation(ctx, inv, since, teamLimit, userLimit)
}

func (f failing) ListTeamInvitations(ctx context.Context, teamID int64, now time.Time) ([]model.TeamInvitation, error) {
	if f.method == "ListTeamInvitations" {
		return nil, errDB
	}
	return f.Repository.ListTeamInvitations(ctx, teamID, now)
}

func (f failing) ListInvitationsForEmail(ctx context.Context, email string, now time.Time) ([]model.TeamInvitation, error) {
	if f.method == "ListInvitationsForEmail" {
		return nil, errDB
	}
	return f.Repository.ListInvitationsForEmail(ctx, email, now)
}

func (f failing) AcceptTeamInvitation(ctx context.Context, id int64, member model.TeamMember, now time.Time) error {
	if f.method == "AcceptTeamInvitation" {
		return errDB
	}
	return f.Repository.AcceptTeamInvitation(ctx, id, member, now)
}

func (f failing) ListWorkspaceShares(ctx context.Context, workspaceID, userID int64) ([]base.WorkspaceShareWithTeam, error) {
	if f.method == "ListWorkspaceShares" {
		return nil, errDB
	}
	return f.Repository.ListWorkspaceShares(ctx, workspaceID, userID)
}

func (f failing) SetWorkspaceShare(ctx context.Context, s model.WorkspaceShare) (base.WorkspaceShareWithTeam, error) {
	if f.method == "SetWorkspaceShare" {
		return base.WorkspaceShareWithTeam{}, errDB
	}
	return f.Repository.SetWorkspaceShare(ctx, s)
}

func (f failing) GetTeamMember(ctx context.Context, teamID, userID int64) (model.TeamMember, error) {
	if f.method == "GetTeamMember" {
		return model.TeamMember{}, errDB
	}
	return f.Repository.GetTeamMember(ctx, teamID, userID)
}

func (f failing) ListTeamWorkspaces(ctx context.Context, teamID int64) ([]base.TeamWorkspaceRow, error) {
	if f.method == "ListTeamWorkspaces" {
		return nil, errDB
	}
	return f.Repository.ListTeamWorkspaces(ctx, teamID)
}

// answeredFirst is the real repository with every invitation answered, by
// another request, between the controller's check and its accept.
type answeredFirst struct{ Repository }

func (a answeredFirst) AcceptTeamInvitation(ctx context.Context, id int64, member model.TeamMember, now time.Time) error {
	other := member
	other.ID, other.UserID = member.ID+1000, eve
	if err := a.Repository.AcceptTeamInvitation(ctx, id, other, now); err != nil {
		return err
	}
	return a.Repository.AcceptTeamInvitation(ctx, id, member, now)
}

// An invitation used by another request after it was checked is gone, as if it
// had been used before.
func TestAnInvitationAnsweredMeanwhileIsGone(t *testing.T) {
	each(t, func(f *fixture) {
		rs := must(f.c.CreateInvitation(f.ctx, alice, f.team, "dave@example.com", ""))
		f.c.(*controller).repo = answeredFirst{f.repo}
		expectKind(t, "accepting", errOf(f.c.AcceptInvitation(f.ctx, dave, token(rs.Link))), entity.TeamGone)
	})
}

// A failure reading or writing reaches the caller as it is.
func TestRepositoryFailuresReachTheCaller(t *testing.T) {
	type call func(f *fixture) error
	cases := []struct {
		name, method string
		call         call
	}{
		{"listing teams", "ListTeamsForUser", func(f *fixture) error { return errOf(f.c.ListTeams(f.ctx, alice)) }},
		{"creating a team", "CreateTeam", func(f *fixture) error { return errOf(f.c.CreateTeam(f.ctx, alice, "x")) }},
		{"reading a team", "GetTeam", func(f *fixture) error { return errOf(f.c.GetTeam(f.ctx, alice, f.team)) }},
		{"counting a team's members", "CountTeamMembers", func(f *fixture) error { return errOf(f.c.GetTeam(f.ctx, alice, f.team)) }},
		{"renaming a team", "RenameTeam", func(f *fixture) error { return errOf(f.c.RenameTeam(f.ctx, alice, f.team, "x")) }},
		{"listing members", "ListTeamMembers", func(f *fixture) error { return errOf(f.c.ListMembers(f.ctx, alice, f.team)) }},
		{"checking an invitee is not a member", "TeamHasMemberWithEmail", func(f *fixture) error {
			return errOf(f.c.CreateInvitation(f.ctx, alice, f.team, "dave@example.com", ""))
		}},
		{"reading the team to invite to", "GetTeam", func(f *fixture) error {
			return errOf(f.c.CreateInvitation(f.ctx, alice, f.team, "dave@example.com", ""))
		}},
		{"storing an invitation", "CreateTeamInvitation", func(f *fixture) error {
			return errOf(f.c.CreateInvitation(f.ctx, alice, f.team, "dave@example.com", ""))
		}},
		{"listing a team's invitations", "ListTeamInvitations", func(f *fixture) error { return errOf(f.c.ListTeamInvitations(f.ctx, alice, f.team)) }},
		{"naming the team of an invitation", "", func(f *fixture) error {
			must(f.c.CreateInvitation(f.ctx, alice, f.team, "dave@example.com", ""))
			f.c.(*controller).repo = failing{f.repo, "GetTeam"}
			return errOf(f.c.ListMyInvitations(f.ctx, dave))
		}},
		{"listing the account's invitations", "ListInvitationsForEmail", func(f *fixture) error { return errOf(f.c.ListMyInvitations(f.ctx, dave)) }},
		{"reading the inviter of a previewed invitation", "", func(f *fixture) error {
			rs := must(f.c.CreateInvitation(f.ctx, alice, f.team, "dave@example.com", ""))
			f.c.(*controller).repo = failing{f.repo, "GetTeamMember"}
			return errOf(f.c.PreviewInvitation(f.ctx, dave, token(rs.Link)))
		}},
		{"reading the team of a previewed invitation", "", func(f *fixture) error {
			rs := must(f.c.CreateInvitation(f.ctx, alice, f.team, "dave@example.com", ""))
			f.c.(*controller).repo = failing{f.repo, "GetTeam"}
			return errOf(f.c.PreviewInvitation(f.ctx, dave, token(rs.Link)))
		}},
		{"accepting an invitation", "", func(f *fixture) error {
			rs := must(f.c.CreateInvitation(f.ctx, alice, f.team, "dave@example.com", ""))
			f.c.(*controller).repo = failing{f.repo, "AcceptTeamInvitation"}
			return errOf(f.c.AcceptInvitation(f.ctx, dave, token(rs.Link)))
		}},
		{"listing a workspace's shares", "ListWorkspaceShares", func(f *fixture) error {
			f.db.Create(&model.Workspace{ID: 100, UserID: alice, Name: "w"})
			return errOf(f.c.ListShares(f.ctx, alice, 100))
		}},
		{"storing a share", "SetWorkspaceShare", func(f *fixture) error {
			f.db.Create(&model.Workspace{ID: 100, UserID: alice, Name: "w"})
			return errOf(f.c.ShareWorkspace(f.ctx, alice, 100, f.team, ""))
		}},
		{"listing a team's workspaces", "ListTeamWorkspaces", func(f *fixture) error { return errOf(f.c.ListTeamWorkspaces(f.ctx, alice, f.team)) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			each(t, func(f *fixture) {
				if c.method != "" {
					f.c.(*controller).repo = failing{f.repo, c.method}
				}
				if err := c.call(f); !errors.Is(err, errDB) {
					t.Errorf("returned %v, want %v", err, errDB)
				}
			})
		})
	}
}

// An account that cannot be read has no email to match an invitation with.
func TestAnAccountThatCannotBeReadMatchesNoInvitation(t *testing.T) {
	each(t, func(f *fixture) {
		rs := must(f.c.CreateInvitation(f.ctx, alice, f.team, "dave@example.com", ""))
		for name, err := range map[string]error{
			"listing its invitations": errOf(f.c.ListMyInvitations(f.ctx, nobody)),
			"previewing":              errOf(f.c.PreviewInvitation(f.ctx, nobody, token(rs.Link))),
			"accepting by link":       errOf(f.c.AcceptInvitation(f.ctx, nobody, token(rs.Link))),
			"accepting by id":         errOf(f.c.AcceptInvitationByID(f.ctx, nobody, rs.Invitation.ID)),
		} {
			expectNotFound(t, name, err)
		}
	})
}

// An inviter whose account is gone is shown without a name.
func TestAnInviterWithoutAnAccountHasNoName(t *testing.T) {
	each(t, func(f *fixture) {
		must(f.c.CreateInvitation(f.ctx, bob, f.team, "dave@example.com", ""))
		f.db.Delete(&model.User{}, bob)
		list := must(f.c.ListTeamInvitations(f.ctx, alice, f.team))
		if len(list) != 1 || list[0].InvitedByName != "" || list[0].InvitedBy != bob {
			t.Errorf("invitations are %+v, want Bob's with no name", list)
		}
	})
}

func TestNewDefaultsTheClockAndNeedsNoPublisher(t *testing.T) {
	c := New(Params{}).(*controller)
	if c.now == nil || time.Since(c.now()) > time.Minute || c.now().Location() != time.UTC {
		t.Error("New left the clock unset, or it is not the wall clock in UTC")
	}
	c.emit(context.Background(), 0, alice, 0, 1) // no publisher: nothing to do, no panic
}
