// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
)

// withSkillEvents gives a skill test environment a pubsub that collects what
// it publishes.
func withSkillEvents(t *testing.T, e *skillEnv) *[]entity.CRUDEvent {
	t.Helper()
	ps := mock_pubsub.NewMockService(gomock.NewController(t))
	events := &[]entity.CRUDEvent{}
	ps.EXPECT().Publish(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, req pubsub.PublishRequest) (*pubsub.PublishResponse, error) {
			if ev, ok := req.Event.(entity.CRUDEvent); ok && req.PubSubID == entity.PubSubTopicCRUD {
				*events = append(*events, ev)
			}
			return &pubsub.PublishResponse{}, nil
		}).AnyTimes()
	e.c.pubsub = ps
	return events
}

func wantSkillEvents(t *testing.T, events *[]entity.CRUDEvent, want ...entity.CRUDEvent) {
	t.Helper()
	if len(*events) != len(want) {
		t.Fatalf("published %d events, want %d: %+v", len(*events), len(want), *events)
	}
	for i, w := range want {
		w.ResourceType, w.Actor, w.UserID, w.Origin = entity.ResourceSkill, entity.ActorHuman, skUser, entity.OriginAPI
		if (*events)[i] != w {
			t.Errorf("event %d = %+v, want %+v", i, (*events)[i], w)
		}
	}
	*events = (*events)[:0]
}

func TestSkillTelemetry_Search(t *testing.T) {
	e := newSkillEnv(t)
	e.save(t, skWS, "tdd", "SKILL.md", md("tdd", "Test first."))
	events := withSkillEvents(t, e)

	if _, err := e.c.SearchSkills(e.ctx, entity.SearchSkillsRequest{WorkspaceID: skWS, UserID: skUserStr, Query: "test"}); err != nil {
		t.Fatal(err)
	}
	wantSkillEvents(t, events, entity.CRUDEvent{Action: entity.ActionSkillSearch, WorkspaceID: skWS})

	// A refused search did not happen, and an agent's is its tool call.
	if _, err := e.c.SearchSkills(e.ctx, entity.SearchSkillsRequest{WorkspaceID: skWS, UserID: skUserStr, Query: "t"}); err == nil {
		t.Fatal("a one-character query must be refused")
	}
	mcpCtx := entity.WithOrigin(e.ctx, entity.OriginMCP)
	if _, err := e.c.SearchSkills(mcpCtx, entity.SearchSkillsRequest{WorkspaceID: skWS, UserID: skUserStr}); err != nil {
		t.Fatal(err)
	}
	wantSkillEvents(t, events)
}

func TestSkillTelemetry_View(t *testing.T) {
	e := newSkillEnv(t)
	saved := e.save(t, skWS, "tdd", "SKILL.md", md("tdd", "Test first."))
	events := withSkillEvents(t, e)
	req := entity.GetSkillFileRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "tdd", Path: "SKILL.md"}

	if _, err := e.c.GetSkillFile(e.ctx, req); err != nil {
		t.Fatal(err)
	}
	wantSkillEvents(t, events, entity.CRUDEvent{Action: entity.ActionSkillView, WorkspaceID: skWS, ResourceID: saved.Skill.ID})

	missing := req
	missing.Path = "nope.md"
	if _, err := e.c.GetSkillFile(e.ctx, missing); err == nil {
		t.Fatal("a missing file must be an error")
	}
	if _, err := e.c.GetSkillFile(entity.WithOrigin(e.ctx, entity.OriginMCP), req); err != nil {
		t.Fatal(err)
	}
	wantSkillEvents(t, events)
}

func TestSkillTelemetry_Import(t *testing.T) {
	e := newSkillEnv(t)
	e.save(t, skWS, "tdd", "SKILL.md", md("tdd", "Our own."))
	events := withSkillEvents(t, e)
	e.importer.res = githubResult(importedSkill("debugging", "Debug."), importedSkill("tdd", "Theirs."))

	// One per skill imported: tdd is skipped, and so is not counted.
	rs, err := e.c.ImportSkills(e.ctx, entity.ImportSkillsRequest{UserID: skUserStr, URL: "u"})
	if err != nil || len(rs.Imported) != 1 {
		t.Fatalf("import: %+v, %v", rs, err)
	}
	wantSkillEvents(t, events, entity.CRUDEvent{Action: entity.ActionSkillImport, ResourceID: rs.Imported[0].ID})

	rs, err = e.c.ImportSkills(e.ctx, entity.ImportSkillsRequest{UserID: skUserStr, URL: "u", Overwrite: true})
	if err != nil || len(rs.Imported) != 2 {
		t.Fatalf("overwrite: %+v, %v", rs, err)
	}
	if len(*events) != 2 || (*events)[0].Action != entity.ActionSkillImport || (*events)[1].Action != entity.ActionSkillImport {
		t.Errorf("overwrite published %+v, want two imports", *events)
	}
}

func TestSkillTelemetry_TurnOffAndOn(t *testing.T) {
	e := newSkillEnv(t)
	saved := e.save(t, skWS, "tdd", "SKILL.md", md("tdd", "Test first."))
	events := withSkillEvents(t, e)
	set := func(enabled bool) {
		t.Helper()
		if _, err := e.c.SetSkillEnabled(e.ctx, entity.SetSkillEnabledRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "tdd", Enabled: enabled}); err != nil {
			t.Fatal(err)
		}
	}

	set(false)
	wantSkillEvents(t, events, entity.CRUDEvent{Action: entity.ActionSkillDisable, WorkspaceID: skWS, ResourceID: saved.Skill.ID})
	// Asking for the state it is already in changes nothing, and counts nothing.
	set(false)
	wantSkillEvents(t, events)
	set(true)
	wantSkillEvents(t, events, entity.CRUDEvent{Action: entity.ActionSkillEnable, WorkspaceID: skWS, ResourceID: saved.Skill.ID})
}

func TestResourceSkillString(t *testing.T) {
	if got := entity.ResourceSkill.String(); got != "skill" {
		t.Errorf("ResourceSkill.String() = %q", got)
	}
}
