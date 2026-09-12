// Copyright 2026 Contextual, Inc. https://agentrq.com

package api

import (
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

func TestFromEntityAgentCommandsToView(t *testing.T) {
	t.Run("nil stays nil, so the field is omitted entirely", func(t *testing.T) {
		// A client reads the field's presence as "there is a menu to show", so
		// an empty object here would advertise a menu with nothing in it.
		if got := fromEntityAgentCommandsToView(nil); got != nil {
			t.Errorf("got %+v, want nil", got)
		}
	})

	t.Run("carries every field across", func(t *testing.T) {
		got := fromEntityAgentCommandsToView(&entity.AgentCommands{
			Commands: []entity.AgentCommand{
				{Name: "web", Description: "Search the web", Hint: "query"},
				{Name: "compact"},
			},
		})

		if got == nil || len(got.Commands) != 2 {
			t.Fatalf("got %+v", got)
		}
		if got.Commands[0].Name != "web" || got.Commands[0].Description != "Search the web" || got.Commands[0].Hint != "query" {
			t.Errorf("commands[0] = %+v", got.Commands[0])
		}
		if got.Commands[1].Description != "" || got.Commands[1].Hint != "" {
			t.Errorf("optional fields should stay empty: %+v", got.Commands[1])
		}
	})

	t.Run("an empty list is still a list, not nil", func(t *testing.T) {
		got := fromEntityAgentCommandsToView(&entity.AgentCommands{})
		if got == nil || got.Commands == nil || len(got.Commands) != 0 {
			t.Errorf("got %+v, want an empty slice", got)
		}
	})
}

func TestFromEntityAgentClientToView(t *testing.T) {
	t.Run("nil stays nil, so the field is omitted", func(t *testing.T) {
		if got := fromEntityAgentClientToView(nil); got != nil {
			t.Errorf("got %+v, want nil", got)
		}
	})

	t.Run("carries the name and version across", func(t *testing.T) {
		got := fromEntityAgentClientToView(&entity.AgentClient{Name: "acp-gateway", Version: "0.2.13"})
		if got == nil || got.Name != "acp-gateway" || got.Version != "0.2.13" {
			t.Errorf("got %+v", got)
		}
	})
}
