// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package team

import (
	"errors"
	"slices"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
)

// stale is a plan for a team skill nobody is opted into: the change finds
// the skills changed under it.
func stale() (base.TeamSkillCopies, []string, error) {
	return base.TeamSkillCopies{Kept: map[base.KeptTeamSkill]string{{WorkspaceID: 1, SkillID: 1}: ""}}, []string{"written"}, nil
}

// Every change that ends shares asks for the copies of what they end: the
// team's, the member's, the workspace's. A change that commits purges what
// it returns; one that fails drops the copies' blobs, and one whose plan
// went stale plans again, giving up after keepTries.
func TestEndingSharesKeepsTeamSkills(t *testing.T) {
	each(t, func(f *fixture) {
		f.db.Create(&model.Workspace{ID: 100, UserID: alice, Name: "w"})
		must(f.c.ShareWorkspace(f.ctx, f.ch(alice), 100, entity.SharedParts{Tasks: true, Memory: true, AutoOptInSkills: true}))

		f.keeper.plans = append(f.keeper.plans, stale)
		_, err := f.c.UnshareWorkspace(f.ctx, f.ch(alice), 100)
		expectOK(t, "unsharing on the second plan", err)
		want := base.TeamSkillsLeaving{TeamID: f.team, WorkspaceID: 100}
		if !slices.Equal(f.keeper.asked, []base.TeamSkillsLeaving{want, want}) {
			t.Errorf("unsharing asked for %+v", f.keeper.asked)
		}
		if len(f.keeper.purged) != 2 || !slices.Equal(f.keeper.purged[0], []string{"written"}) || f.keeper.purged[1] != nil {
			t.Errorf("unsharing purged %q, want the stale plan's blobs, then nothing", f.keeper.purged)
		}

		f.keeper.asked, f.keeper.purged = nil, nil
		f.keeper.plans = []func() (base.TeamSkillCopies, []string, error){stale, stale, stale, stale}
		_, err = f.c.RemoveMember(f.ctx, f.ch(alice), carol)
		expectKind(t, "removing Carol on plans that keep going stale", err, entity.TeamConflict)
		if len(f.keeper.asked) != keepTries || f.keeper.asked[0] != (base.TeamSkillsLeaving{TeamID: f.team, UserID: carol}) || len(f.keeper.purged) != keepTries {
			t.Errorf("removing asked %+v and purged %q, want %d of each", f.keeper.asked, f.keeper.purged, keepTries)
		}

		f.keeper.asked, f.keeper.plans = nil, []func() (base.TeamSkillCopies, []string, error){func() (base.TeamSkillCopies, []string, error) {
			return base.TeamSkillCopies{}, nil, errors.New("disk full")
		}}
		if err := f.c.LeaveTeam(f.ctx, f.ch(bob)); err == nil || err.Error() != "disk full" {
			t.Errorf("leaving with the copies failing: %v", err)
		}
		f.keeper.plans = nil
		var ve *entity.TeamVersionError
		if err := f.c.DeleteTeam(f.ctx, f.stale(alice)); !errors.As(err, &ve) {
			t.Errorf("deleting from the version before: %v, want a version conflict", err)
		}
		f.keeper.asked = nil
		expectOK(t, "deleting the team", f.c.DeleteTeam(f.ctx, f.ch(alice)))
		if !slices.Equal(f.keeper.asked, []base.TeamSkillsLeaving{{TeamID: f.team}}) {
			t.Errorf("deleting asked for %+v", f.keeper.asked)
		}
	})
}
