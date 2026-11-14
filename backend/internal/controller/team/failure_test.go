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

func (f failing) ListTeamsForUser(ctx context.Context, userID int64) ([]base.TeamWithCount, error) {
	if f.method == "ListTeamsForUser" {
		return nil, errDB
	}
	return f.Repository.ListTeamsForUser(ctx, userID)
}

func (f failing) CreateTeam(ctx context.Context, team model.Team, first model.TeamMember, act model.TeamManagementActivity) error {
	if f.method == "CreateTeam" {
		return errDB
	}
	return f.Repository.CreateTeam(ctx, team, first, act)
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

func (f failing) RenameTeam(ctx context.Context, teamID, callerID, version int64, name string, act model.TeamManagementActivity) (int64, error) {
	if f.method == "RenameTeam" {
		return 0, errDB
	}
	return f.Repository.RenameTeam(ctx, teamID, callerID, version, name, act)
}

func (f failing) DeleteTeam(ctx context.Context, teamID, callerID, version int64, act model.TeamManagementActivity) error {
	if f.method == "DeleteTeam" {
		return errDB
	}
	return f.Repository.DeleteTeam(ctx, teamID, callerID, version, act)
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

func (f failing) RemoveTeamMember(ctx context.Context, teamID, callerID, userID, version int64, act model.TeamManagementActivity, deletedID int64) (int64, bool, error) {
	if f.method == "RemoveTeamMember" {
		return 0, false, errDB
	}
	return f.Repository.RemoveTeamMember(ctx, teamID, callerID, userID, version, act, deletedID)
}

func (f failing) CreateTeamInvitation(ctx context.Context, inv model.TeamInvitation, version int64, since time.Time, teamLimit, userLimit int64, act model.TeamManagementActivity) (int64, error) {
	if f.method == "CreateTeamInvitation" {
		return 0, errDB
	}
	return f.Repository.CreateTeamInvitation(ctx, inv, version, since, teamLimit, userLimit, act)
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

func (f failing) GetTeamInvitation(ctx context.Context, id int64) (model.TeamInvitation, error) {
	if f.method == "GetTeamInvitation" {
		return model.TeamInvitation{}, errDB
	}
	return f.Repository.GetTeamInvitation(ctx, id)
}

func (f failing) RevokeTeamInvitation(ctx context.Context, teamID, callerID, version, id int64, act model.TeamManagementActivity) (int64, error) {
	if f.method == "RevokeTeamInvitation" {
		return 0, errDB
	}
	return f.Repository.RevokeTeamInvitation(ctx, teamID, callerID, version, id, act)
}

func (f failing) DeclineTeamInvitation(ctx context.Context, teamID, id int64, act model.TeamManagementActivity) error {
	if f.method == "DeclineTeamInvitation" {
		return errDB
	}
	return f.Repository.DeclineTeamInvitation(ctx, teamID, id, act)
}

func (f failing) AcceptTeamInvitation(ctx context.Context, id int64, member model.TeamMember, now time.Time, act model.TeamManagementActivity) error {
	if f.method == "AcceptTeamInvitation" {
		return errDB
	}
	return f.Repository.AcceptTeamInvitation(ctx, id, member, now, act)
}

func (f failing) ListWorkspaceShares(ctx context.Context, workspaceID, userID int64) ([]base.WorkspaceShareWithTeam, error) {
	if f.method == "ListWorkspaceShares" {
		return nil, errDB
	}
	return f.Repository.ListWorkspaceShares(ctx, workspaceID, userID)
}

func (f failing) SetWorkspaceShare(ctx context.Context, s model.WorkspaceShare, version int64, act model.TeamManagementActivity) (base.WorkspaceShareWithTeam, int64, error) {
	if f.method == "SetWorkspaceShare" {
		return base.WorkspaceShareWithTeam{}, 0, errDB
	}
	return f.Repository.SetWorkspaceShare(ctx, s, version, act)
}

func (f failing) DeleteWorkspaceShare(ctx context.Context, workspaceID, teamID, userID, version int64, act model.TeamManagementActivity) (int64, error) {
	if f.method == "DeleteWorkspaceShare" {
		return 0, errDB
	}
	return f.Repository.DeleteWorkspaceShare(ctx, workspaceID, teamID, userID, version, act)
}

func (f failing) ListTeamActivities(ctx context.Context, teamID int64, limit int, before int64) ([]base.TeamActivityRow, error) {
	if f.method == "ListTeamActivities" {
		return nil, errDB
	}
	return f.Repository.ListTeamActivities(ctx, teamID, limit, before)
}

func (f failing) GetTeamMember(ctx context.Context, teamID, userID int64) (model.TeamMember, error) {
	if f.method == "GetTeamMember" {
		return model.TeamMember{}, errDB
	}
	return f.Repository.GetTeamMember(ctx, teamID, userID)
}

func (f failing) GetWorkspace(ctx context.Context, id, userID int64) (model.Workspace, error) {
	if f.method == "GetWorkspace" {
		return model.Workspace{}, errDB
	}
	return f.Repository.GetWorkspace(ctx, id, userID)
}

func (f failing) CanReadWorkspaceInTeam(ctx context.Context, userID, teamID, workspaceID int64) (bool, error) {
	if f.method == "CanReadWorkspaceInTeam" {
		return false, errDB
	}
	return f.Repository.CanReadWorkspaceInTeam(ctx, userID, teamID, workspaceID)
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

func (a answeredFirst) AcceptTeamInvitation(ctx context.Context, id int64, member model.TeamMember, now time.Time, act model.TeamManagementActivity) error {
	other := member
	other.ID, other.UserID = member.ID+1000, eve
	otherAct := act
	otherAct.ID, otherAct.ActorUserID = act.ID+1000, eve
	if err := a.Repository.AcceptTeamInvitation(ctx, id, other, now, otherAct); err != nil {
		return err
	}
	return a.Repository.AcceptTeamInvitation(ctx, id, member, now, act)
}

// declinedFirst is the real repository with every invitation declined, by
// another request, between the controller's check and its decline.
type declinedFirst struct{ Repository }

func (d declinedFirst) DeclineTeamInvitation(ctx context.Context, teamID, id int64, act model.TeamManagementActivity) error {
	first := act
	first.ID += 1000
	if err := d.Repository.DeclineTeamInvitation(ctx, teamID, id, first); err != nil {
		return err
	}
	return d.Repository.DeclineTeamInvitation(ctx, teamID, id, act)
}

// An invitation used by another request after it was checked is gone, as if it
// had been used before.
func TestAnInvitationAnsweredMeanwhileIsGone(t *testing.T) {
	each(t, func(f *fixture) {
		rs := must(f.c.CreateInvitation(f.ctx, f.ch(alice), "dave@example.com"))
		f.c.(*controller).repo = answeredFirst{f.repo}
		expectKind(t, "accepting", errOf(f.c.AcceptInvitation(f.ctx, dave, token(rs.Link))), entity.TeamGone)
	})
}

// An invitation declined by another request after it was checked is gone.
func TestAnInvitationDeclinedMeanwhileIsGone(t *testing.T) {
	each(t, func(f *fixture) {
		rs := must(f.c.CreateInvitation(f.ctx, f.ch(alice), "dave@example.com"))
		f.c.(*controller).repo = declinedFirst{f.repo}
		expectKind(t, "declining", f.c.DeclineInvitation(f.ctx, dave, rs.Invitation.ID), entity.TeamGone)
	})
}

// A failure reading or writing reaches the caller as it is.
func TestRepositoryFailuresReachTheCaller(t *testing.T) {
	type call func(f *fixture) error
	invite := func(f *fixture) error { return errOf(f.c.CreateInvitation(f.ctx, f.ch(alice), "dave@example.com")) }
	workspace := func(f *fixture) { f.db.Create(&model.Workspace{ID: 100, UserID: alice, Name: "w"}) }
	pending := func(f *fixture, method string) entity.CreateTeamInvitationResponse {
		rs := must(f.c.CreateInvitation(f.ctx, f.ch(alice), "dave@example.com"))
		f.c.(*controller).repo = failing{f.repo, method}
		return rs
	}
	cases := []struct {
		name, method string
		call         call
	}{
		{"listing teams", "ListTeamsForUser", func(f *fixture) error { return errOf(f.c.ListTeams(f.ctx, alice)) }},
		{"creating a team", "CreateTeam", func(f *fixture) error { return errOf(f.c.CreateTeam(f.ctx, alice, "x")) }},
		{"reading a team", "GetTeam", func(f *fixture) error { return errOf(f.c.GetTeam(f.ctx, alice, f.team)) }},
		{"checking membership", "GetTeamMember", func(f *fixture) error { return errOf(f.c.GetTeam(f.ctx, alice, f.team)) }},
		{"counting a team's members", "CountTeamMembers", func(f *fixture) error { return errOf(f.c.GetTeam(f.ctx, alice, f.team)) }},
		{"renaming a team", "RenameTeam", func(f *fixture) error { return errOf(f.c.RenameTeam(f.ctx, f.ch(alice), "x")) }},
		{"reading the renamed team", "", func(f *fixture) error {
			f.c.(*controller).repo = failing{f.repo, "CountTeamMembers"}
			return errOf(f.c.RenameTeam(f.ctx, f.ch(alice), "x"))
		}},
		{"deleting a team", "DeleteTeam", func(f *fixture) error { return f.c.DeleteTeam(f.ctx, f.ch(alice)) }},
		{"listing members", "ListTeamMembers", func(f *fixture) error { return errOf(f.c.ListMembers(f.ctx, alice, f.team)) }},
		{"removing a member", "RemoveTeamMember", func(f *fixture) error { return errOf(f.c.RemoveMember(f.ctx, f.ch(alice), bob)) }},
		{"leaving a team", "RemoveTeamMember", func(f *fixture) error { return f.c.LeaveTeam(f.ctx, f.ch(bob)) }},
		{"checking an invitee is not a member", "TeamHasMemberWithEmail", invite},
		{"reading the team to invite to", "GetTeam", invite},
		{"storing an invitation", "CreateTeamInvitation", invite},
		{"listing a team's invitations", "ListTeamInvitations", func(f *fixture) error { return errOf(f.c.ListTeamInvitations(f.ctx, alice, f.team)) }},
		{"reading the version of a team's invitations", "GetTeam", func(f *fixture) error { return errOf(f.c.ListTeamInvitations(f.ctx, alice, f.team)) }},
		{"naming the team of a team's invitations", "", func(f *fixture) error {
			pending(f, "GetTeam")
			return errOf(f.c.ListTeamInvitations(f.ctx, alice, f.team))
		}},
		{"reading the version of a team's members", "GetTeam", func(f *fixture) error { return errOf(f.c.ListMembers(f.ctx, alice, f.team)) }},
		{"reading the version of a team's activity", "GetTeam", func(f *fixture) error {
			return errOf(f.c.ListActivities(f.ctx, entity.ListTeamActivitiesRequest{UserID: alice, TeamID: f.team}))
		}},
		{"revoking an invitation", "", func(f *fixture) error {
			rs := pending(f, "RevokeTeamInvitation")
			return errOf(f.c.RevokeInvitation(f.ctx, f.ch(alice), rs.Invitation.ID))
		}},
		{"naming the team of an invitation", "", func(f *fixture) error {
			pending(f, "GetTeam")
			return errOf(f.c.ListMyInvitations(f.ctx, dave))
		}},
		{"listing the account's invitations", "ListInvitationsForEmail", func(f *fixture) error { return errOf(f.c.ListMyInvitations(f.ctx, dave)) }},
		{"reading the inviter of a previewed invitation", "", func(f *fixture) error {
			rs := pending(f, "GetTeamMember")
			return errOf(f.c.PreviewInvitation(f.ctx, dave, token(rs.Link)))
		}},
		{"reading the team of a previewed invitation", "", func(f *fixture) error {
			rs := pending(f, "GetTeam")
			return errOf(f.c.PreviewInvitation(f.ctx, dave, token(rs.Link)))
		}},
		{"accepting an invitation", "", func(f *fixture) error {
			rs := pending(f, "AcceptTeamInvitation")
			return errOf(f.c.AcceptInvitation(f.ctx, dave, token(rs.Link)))
		}},
		{"reading an invitation to accept", "", func(f *fixture) error {
			rs := pending(f, "GetTeamInvitation")
			return errOf(f.c.AcceptInvitationByID(f.ctx, dave, rs.Invitation.ID))
		}},
		{"reading the joined team", "", func(f *fixture) error {
			rs := pending(f, "CountTeamMembers")
			return errOf(f.c.AcceptInvitation(f.ctx, dave, token(rs.Link)))
		}},
		{"declining an invitation", "", func(f *fixture) error {
			rs := pending(f, "DeclineTeamInvitation")
			return f.c.DeclineInvitation(f.ctx, dave, rs.Invitation.ID)
		}},
		{"listing a team's activity", "ListTeamActivities", func(f *fixture) error {
			return errOf(f.c.ListActivities(f.ctx, entity.ListTeamActivitiesRequest{UserID: alice, TeamID: f.team}))
		}},
		{"checking membership to list activity", "GetTeamMember", func(f *fixture) error {
			return errOf(f.c.ListActivities(f.ctx, entity.ListTeamActivitiesRequest{UserID: alice, TeamID: f.team}))
		}},
		{"listing a workspace's shares", "ListWorkspaceShares", func(f *fixture) error {
			workspace(f)
			return errOf(f.c.ListShares(f.ctx, alice, 100))
		}},
		{"reading a workspace to list its shares", "GetWorkspace", func(f *fixture) error {
			workspace(f)
			return errOf(f.c.ListShares(f.ctx, alice, 100))
		}},
		{"reading a workspace to share", "GetWorkspace", func(f *fixture) error {
			workspace(f)
			return errOf(f.c.ShareWorkspace(f.ctx, f.ch(alice), 100))
		}},
		{"storing a share", "SetWorkspaceShare", func(f *fixture) error {
			workspace(f)
			return errOf(f.c.ShareWorkspace(f.ctx, f.ch(alice), 100))
		}},
		{"reading a workspace to unshare", "", func(f *fixture) error {
			workspace(f)
			must(f.c.ShareWorkspace(f.ctx, f.ch(alice), 100))
			f.c.(*controller).repo = failing{f.repo, "GetWorkspace"}
			return errOf(f.c.UnshareWorkspace(f.ctx, f.ch(alice), 100))
		}},
		{"deleting a share", "", func(f *fixture) error {
			workspace(f)
			must(f.c.ShareWorkspace(f.ctx, f.ch(alice), 100))
			f.c.(*controller).repo = failing{f.repo, "DeleteWorkspaceShare"}
			return errOf(f.c.UnshareWorkspace(f.ctx, f.ch(alice), 100))
		}},
		{"checking a team read", "CanReadWorkspaceInTeam", func(f *fixture) error {
			return errOf(f.c.CanReadWorkspaceInTeam(f.ctx, alice, f.team, 100))
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
		rs := must(f.c.CreateInvitation(f.ctx, f.ch(alice), "dave@example.com"))
		for name, err := range map[string]error{
			"listing its invitations": errOf(f.c.ListMyInvitations(f.ctx, nobody)),
			"previewing":              errOf(f.c.PreviewInvitation(f.ctx, nobody, token(rs.Link))),
			"accepting by link":       errOf(f.c.AcceptInvitation(f.ctx, nobody, token(rs.Link))),
			"accepting by id":         errOf(f.c.AcceptInvitationByID(f.ctx, nobody, rs.Invitation.ID)),
			"declining":               f.c.DeclineInvitation(f.ctx, nobody, rs.Invitation.ID),
		} {
			expectNotFound(t, name, err)
		}
	})
}

// An inviter whose account is gone is shown without a name.
func TestAnInviterWithoutAnAccountHasNoName(t *testing.T) {
	each(t, func(f *fixture) {
		must(f.c.CreateInvitation(f.ctx, f.ch(bob), "dave@example.com"))
		f.db.Delete(&model.User{}, bob)
		list := must(f.c.ListTeamInvitations(f.ctx, alice, f.team))
		if len(list.Invitations) != 1 || list.Invitations[0].InvitedByName != "" || list.Invitations[0].InvitedBy != bob {
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
