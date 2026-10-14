// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"encoding/json"
	"testing"

	mcpctrl "github.com/agentrq/agentrq/backend/internal/controller/mcp"
)

func options(t *testing.T, raw string) []configOption {
	t.Helper()
	var opts []configOption
	if err := json.Unmarshal([]byte(raw), &opts); err != nil {
		t.Fatalf("config options %s did not decode: %v", raw, err)
	}
	return opts
}

func TestModelsFrom(t *testing.T) {
	t.Run("the model option is found by category", func(t *testing.T) {
		var resp newSessionResponse
		if err := json.Unmarshal(fixture(t, "session_new.response.json"), &resp); err != nil {
			t.Fatal(err)
		}
		id, current, models, ok := modelsFrom(resp.ConfigOptions)
		want := []mcpctrl.AgentModel{{ID: "model-large", Name: "Large", Current: true}, {ID: "model-small", Name: "Small", Description: "faster"}}
		if !ok || id != "model" || current != "model-large" || len(models) != 2 || models[0] != want[0] || models[1] != want[1] {
			t.Fatalf("modelsFrom was (%q, %q, %+v, %v), want (model, model-large, %+v, true)", id, current, models, ok, want)
		}
	})
	t.Run("an option named like a model is found when none says it is one", func(t *testing.T) {
		id, _, models, ok := modelsFrom(options(t, `[{"id":"llm","name":"LLM","type":"select","currentValue":"a","options":[{"value":"a","name":"A"}]}]`))
		if !ok || id != "llm" || len(models) != 1 {
			t.Fatalf("modelsFrom was (%q, %+v, %v), want the llm option", id, models, ok)
		}
	})
	t.Run("a reasoning-effort select is never taken for the model list", func(t *testing.T) {
		for _, raw := range []string{
			`[{"id":"model_effort","name":"Effort","type":"select","options":[{"value":"low","name":"Low"}]}]`,
			`[{"id":"x","name":"Model thinking","type":"select","options":[{"value":"on","name":"On"}]}]`,
			`[{"id":"model_x","name":"Model","category":"model_config","type":"select","options":[{"value":"a","name":"A"}]}]`,
		} {
			if _, _, _, ok := modelsFrom(options(t, raw)); ok {
				t.Errorf("modelsFrom(%s) found models, want none", raw)
			}
		}
	})
	t.Run("grouped models keep their group", func(t *testing.T) {
		_, _, models, _ := modelsFrom(options(t, `[{"id":"model","name":"Model","type":"select","currentValue":"b","options":[{"group":"fast","name":"Fast","options":[{"value":"a","name":"A"}]},{"group":"big","options":[{"value":"b"}]}]}]`))
		want := []mcpctrl.AgentModel{{ID: "a", Name: "A", Group: "Fast"}, {ID: "b", Name: "b", Group: "big", Current: true}}
		if len(models) != 2 || models[0] != want[0] || models[1] != want[1] {
			t.Fatalf("models were %+v, want %+v (a nameless group reads as its id, a nameless model as its value)", models, want)
		}
	})
	t.Run("a numeric current value is read as text", func(t *testing.T) {
		_, current, models, _ := modelsFrom(options(t, `[{"id":"model","name":"Model","type":"select","currentValue":2,"options":[{"value":1,"name":"One"},{"value":2,"name":"Two"}]}]`))
		if current != "2" || !models[1].Current || models[0].ID != "1" {
			t.Fatalf("current was %q and models %+v, want 2 current", current, models)
		}
	})
	t.Run("no select means no models", func(t *testing.T) {
		if _, _, _, ok := modelsFrom(options(t, `[{"id":"model","name":"Model","type":"boolean","currentValue":true}]`)); ok {
			t.Fatal("a boolean option was taken for a model list")
		}
		if _, _, _, ok := modelsFrom(nil); ok {
			t.Fatal("no options at all gave a model list")
		}
	})
	t.Run("a select that says it is the model is one, whatever its name", func(t *testing.T) {
		if !isModelOption(configOption{ID: "x", Name: "Effort", Type: "select", Category: "model"}) {
			t.Fatal("a select with category model was not taken for the model")
		}
	})
	t.Run("a model select with no choices is passed over", func(t *testing.T) {
		id, _, _, ok := modelsFrom(options(t, `[{"id":"model","type":"select","options":[]},{"id":"model_name","type":"select","options":[{"value":"a"}]}]`))
		if !ok || id != "model_name" {
			t.Fatalf("modelsFrom picked %q (%v), want model_name", id, ok)
		}
	})
}

func TestResolveModel(t *testing.T) {
	models := []mcpctrl.AgentModel{{ID: "model-large", Name: "Large"}, {ID: "model-small", Name: "Small"}}
	for requested, want := range map[string]string{"model-small": "model-small", "large": "model-large", "nope": ""} {
		if got, _ := resolveModel(models, requested); got != want {
			t.Errorf("resolveModel(%q) was %q, want %q", requested, got, want)
		}
	}
	marked := withCurrent(models, "model-small")
	if marked[0].Current || !marked[1].Current || models[1].Current {
		t.Errorf("withCurrent gave %+v and left %+v, want only a copy with model-small current", marked, models)
	}
}

func TestApprovalModes(t *testing.T) {
	var resp newSessionResponse
	if err := json.Unmarshal(fixture(t, "session_new.response.json"), &resp); err != nil {
		t.Fatal(err)
	}
	t.Run("an auto-reviewing agent mode is passed over for read-only", func(t *testing.T) {
		if got := humanMode(resp.Modes); got != "read-only" {
			t.Fatalf("humanMode was %q, want read-only", got)
		}
	})
	t.Run("with no mode that says it asks, the first that does not approve is taken", func(t *testing.T) {
		got := humanMode(&sessionModeState{AvailableModes: []sessionMode{{ID: "yolo", Name: "YOLO"}, {ID: "plain", Name: "Plain"}}})
		if got != "plain" {
			t.Fatalf("humanMode was %q, want plain", got)
		}
	})
	t.Run("no asking mode at all yields nothing", func(t *testing.T) {
		if got := humanMode(&sessionModeState{AvailableModes: []sessionMode{{ID: "bypass", Name: "Bypass"}}}); got != "" {
			t.Fatalf("humanMode was %q, want none", got)
		}
		if got := humanMode(nil); got != "" {
			t.Fatalf("humanMode(nil) was %q, want none", got)
		}
	})
	t.Run("a mode the agent never advertised counts as auto-approving", func(t *testing.T) {
		if modeAsks(resp.Modes, "invented") {
			t.Fatal("an unadvertised mode was taken for one that asks")
		}
		if modeAsks(resp.Modes, "agent-full-access") {
			t.Fatal("a full-access mode was taken for one that asks")
		}
		if !modeAsks(resp.Modes, "read-only") {
			t.Fatal("read-only was not taken for a mode that asks")
		}
	})
}
