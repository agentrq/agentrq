// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mustafaturan/monoflake"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
)

type fakeTeamMembers struct {
	members []base.TeamMemberWithUser
	err     error
	asked   []int64
}

func (f *fakeTeamMembers) ListTeamMembers(_ context.Context, teamID int64) ([]base.TeamMemberWithUser, error) {
	f.asked = append(f.asked, teamID)
	return f.members, f.err
}

// A change to a team skill reaches every member of the team with their
// stream open, tagged with the team, and nobody else; any other event is
// left to the rest of the forwarding.
func TestPublishTeamSkillChange(t *testing.T) {
	const team, alice, bob, eve = int64(11), int64(1), int64(2), int64(3)
	repo := &fakeTeamMembers{members: []base.TeamMemberWithUser{{TeamMember: model.TeamMember{UserID: alice}}, {TeamMember: model.TeamMember{UserID: bob}}}}
	bus := eventbus.New()
	if !publishTeamSkillChange(context.Background(), repo, bus, entity.CRUDEvent{ResourceType: entity.ResourceTeamSkill, TeamID: team}) || len(repo.asked) != 0 {
		t.Errorf("with no stream open the change was not handled, or the team was read (%v)", repo.asked)
	}
	a := bus.Subscribe(0, monoflake.ID(alice).String())
	e := bus.Subscribe(0, monoflake.ID(eve).String())
	for action, want := range map[entity.Action]string{
		entity.ActionTeamSkillCreate: "team.skill.created", entity.ActionTeamSkillDelete: "team.skill.deleted",
		entity.ActionTeamSkillGive: "team.skill.given", entity.ActionTeamSkillOptIn: "team.skill.optedIn",
		entity.ActionTeamSkillOptOut: "team.skill.optedOut",
	} {
		if !publishTeamSkillChange(context.Background(), repo, bus, entity.CRUDEvent{Action: action, ResourceType: entity.ResourceTeamSkill, ResourceID: 50, TeamID: team}) {
			t.Fatalf("%v was not handled", action)
		}
		var got struct {
			Type    string            `json:"type"`
			TeamID  string            `json:"teamId"`
			Payload map[string]string `json:"payload"`
		}
		line := <-a
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(string(line), "data: "))), &got); err != nil {
			t.Fatal(err)
		}
		if got.Type != want || got.TeamID != monoflake.ID(team).String() || got.Payload["id"] != monoflake.ID(50).String() {
			t.Errorf("%v was sent as %+v, want %s for team %d", action, got, want, team)
		}
	}
	select {
	case line := <-e:
		t.Errorf("someone outside the team was sent %s", line)
	default:
	}
	repo.err = errors.New("db down")
	if !publishTeamSkillChange(context.Background(), repo, bus, entity.CRUDEvent{ResourceType: entity.ResourceTeamSkill, TeamID: team}) {
		t.Error("a failed member list was not handled")
	}
	if publishTeamSkillChange(context.Background(), repo, bus, entity.CRUDEvent{ResourceType: entity.ResourceSkill}) {
		t.Error("an account skill's event was taken for a team skill's")
	}
}
