// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlink"
)

func TestEffortFrom(t *testing.T) {
	t.Run("the thought_level select is found by category", func(t *testing.T) {
		var resp newSessionResponse
		if err := json.Unmarshal(fixture(t, "session_new.response.json"), &resp); err != nil {
			t.Fatal(err)
		}
		id, current, efforts, ok := effortFrom(resp.ConfigOptions)
		want := []agentlink.Effort{{ID: "low", Name: "Low"}, {ID: "high", Name: "High"}}
		if !ok || id != "effort" || current != "high" || !slices.Equal(efforts, want) {
			t.Fatalf("effortFrom = %q %q %+v %v, want effort high %+v", id, current, efforts, ok, want)
		}
	})

	t.Run("never guessed from names", func(t *testing.T) {
		opts := options(t, `[{"id":"effort","name":"Reasoning effort","type":"select","currentValue":"low","options":[{"value":"low"}]},
			{"id":"thinking","name":"Thinking","category":"model_config","type":"select","options":[{"value":"on"}]}]`)
		if _, _, _, ok := effortFrom(opts); ok {
			t.Fatal("a select without category thought_level was taken for the reasoning level")
		}
	})

	t.Run("a boolean or empty thought_level is skipped, and a nameless level is named by its id", func(t *testing.T) {
		opts := options(t, `[{"id":"think","category":"thought_level","type":"boolean","currentValue":true},
			{"id":"empty","category":"thought_level","type":"select","options":[]},
			{"id":"level","category":"thought_level","type":"select","currentValue":"max","options":[{"value":"max","description":"slowest"}]}]`)
		id, current, efforts, ok := effortFrom(opts)
		if !ok || id != "level" || current != "max" || !slices.Equal(efforts, []agentlink.Effort{{ID: "max", Name: "max", Description: "slowest"}}) {
			t.Fatalf("effortFrom = %q %q %+v %v, want the level select", id, current, efforts, ok)
		}
	})
}

// fixtureWithout is the session/new fixture with the effort option removed.
func fixtureWithout(t *testing.T) string {
	t.Helper()
	raw := string(fixture(t, "session_new.response.json"))
	i := strings.Index(raw, `,{"id":"effort"`)
	return raw[:i] + "]}"
}

// configOptionsOf is the fixture's config options, with replacements applied.
func configOptionsOf(t *testing.T, oldNew ...string) string {
	t.Helper()
	answered := strings.NewReplacer(oldNew...).Replace(string(fixture(t, "session_new.response.json")))
	var opts struct {
		ConfigOptions json.RawMessage `json:"configOptions"`
	}
	if err := json.Unmarshal([]byte(answered), &opts); err != nil {
		t.Fatal(err)
	}
	return string(opts.ConfigOptions)
}

func TestEngineEffort(t *testing.T) {
	t.Run("the level from session/new is reported, settable", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		a.readyAsking(rec)
		rec.wait(t, func() bool { return len(rec.effort) == 1 })
		rec.read(func() {
			e := rec.effort[0]
			if e.ConfigID != "effort" || e.CurrentEffort != "high" || !e.CanSet || len(e.Efforts) != 2 || e.SessionID != "sess-1" || e.TaskID != "" {
				t.Fatalf("effort report was %+v, want high of 2 on sess-1, settable, no task", e)
			}
		})
	})

	t.Run("an agent without a thought_level option reports nothing", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		a.answer(a.expect("initialize"), string(fixture(t, "initialize.response.json")))
		a.answer(a.expect("session/new"), fixtureWithout(t))
		a.answer(a.expect("session/set_mode"), `{}`)
		l := rec.link(t)
		rec.wait(t, func() bool { return len(rec.models) == 1 })
		a.update("sess-1", `{"sessionUpdate":"config_option_update","configOptions":[]}`)
		a.sync(l)
		rec.read(func() {
			if len(rec.effort) != 0 {
				t.Fatalf("effort reports were %+v, want none", rec.effort)
			}
		})
	})

	t.Run("a model without levels withdraws the option, and a model with them brings it back", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.readyAsking(rec)
		rec.wait(t, func() bool { return len(rec.effort) == 1 })
		a.update("sess-1", `{"sessionUpdate":"config_option_update","configOptions":`+`[{"id":"model","category":"model","type":"select","currentValue":"model-small","options":[{"value":"model-small"}]}]`+`}`)
		rec.wait(t, func() bool { return len(rec.effort) == 2 })
		a.update("sess-1", `{"sessionUpdate":"config_option_update","configOptions":[]}`)
		a.sync(l)
		a.update("sess-1", `{"sessionUpdate":"config_option_update","configOptions":`+configOptionsOf(t, `"currentValue":"high"`, `"currentValue":"low"`)+`}`)
		rec.wait(t, func() bool { return len(rec.effort) == 3 })
		rec.read(func() {
			if e := rec.effort[1]; len(e.Efforts) != 0 || e.CanSet {
				t.Fatalf("the withdrawal was %+v, want no levels", e)
			}
			if e := rec.effort[2]; e.CurrentEffort != "low" || len(e.Efforts) != 2 {
				t.Fatalf("the return was %+v, want low of 2", e)
			}
		})
	})

	t.Run("a model switch whose answer has no levels withdraws them", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.readyAsking(rec)
		rec.wait(t, func() bool { return len(rec.effort) == 1 })
		l.SetModel(context.Background(), "sess-1", "model", "model-small")
		a.answer(a.expect("session/set_config_option"), `{"configOptions":[{"id":"model","category":"model","type":"select","currentValue":"model-small","options":[{"value":"model-small"}]}]}`)
		rec.wait(t, func() bool { return len(rec.effort) == 2 })
		rec.read(func() {
			if e := rec.effort[1]; len(e.Efforts) != 0 {
				t.Fatalf("effort report was %+v, want withdrawn", e)
			}
		})
	})

	t.Run("an effort switch whose answer updates models passes the model change on", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.readyAsking(rec)
		rec.wait(t, func() bool { return len(rec.models) == 1 && len(rec.effort) == 1 })
		l.SetEffort(context.Background(), "sess-1", "effort", "low")
		a.answer(a.expectParams("session/set_config_option", `{"sessionId":"sess-1","configId":"effort","value":"low"}`),
			`{"configOptions":`+configOptionsOf(t, `"currentValue":"model-large"`, `"currentValue":"model-small"`, `"currentValue":"high"`, `"currentValue":"low"`)+`}`)
		rec.wait(t, func() bool { return len(rec.models) == 2 && len(rec.effort) == 2 })
		rec.read(func() {
			if m := rec.models[1]; m.CurrentModel != "model-small" {
				t.Fatalf("models report was %+v, want model-small after the effort switch", m)
			}
			if e := rec.effort[1]; e.CurrentEffort != "low" {
				t.Fatalf("effort report was %+v, want low after the effort switch", e)
			}
		})
	})

	t.Run("SetEffort switches the session and becomes the default for new ones", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.readyAsking(rec)
		rec.wait(t, func() bool { return len(rec.effort) == 1 })
		if !l.SetEffort(context.Background(), "sess-1", "effort", "low") {
			t.Fatal("SetEffort was refused")
		}
		a.answer(a.expectParams("session/set_config_option", `{"sessionId":"sess-1","configId":"effort","value":"low"}`),
			`{"configOptions":`+configOptionsOf(t, `"currentValue":"high"`, `"currentValue":"low"`)+`}`)
		rec.wait(t, func() bool { return len(rec.effort) == 2 })
		rec.read(func() {
			if e := rec.effort[1]; e.CurrentEffort != "low" {
				t.Fatalf("effort report was %+v, want low after the switch", e)
			}
		})
		a.turn(rec, l, 42, "sess-1", "end_turn")
		l.DeliverTask(context.Background(), 43, "next")
		a.answer(a.expect("session/new"), strings.Replace(string(fixture(t, "session_new.response.json")), "sess-1", "sess-2", 1))
		a.answer(a.expectParams("session/set_config_option", `{"sessionId":"sess-2","configId":"effort","value":"low"}`), `{}`)
		a.answer(a.expect("session/set_mode"), `{}`)
		a.expect("session/prompt")
		rec.wait(t, func() bool { return len(rec.effort) >= 4 })
		rec.read(func() {
			if e := rec.effort[len(rec.effort)-1]; e.SessionID != "sess-2" || e.CurrentEffort != "low" || e.TaskID != b62(43) {
				t.Fatalf("the new session's report was %+v, want low on sess-2 for task 43", e)
			}
		})
	})

	t.Run("the bridge's effort is applied to every new session that offers it", func(t *testing.T) {
		rec := &recorder{}
		a := startAgentWith(t, rec, agentOpts{header: map[string]string{"X-AgentRQ-Agent-Effort": "low"}})
		a.answer(a.expect("initialize"), string(fixture(t, "initialize.response.json")))
		a.answer(a.expect("session/new"), string(fixture(t, "session_new.response.json")))
		a.answer(a.expectParams("session/set_config_option", `{"sessionId":"sess-1","configId":"effort","value":"low"}`), `{}`)
		a.answer(a.expect("session/set_mode"), `{}`)
		rec.link(t)
		rec.wait(t, func() bool { return len(rec.effort) == 1 })
		rec.read(func() {
			if e := rec.effort[0]; e.CurrentEffort != "low" {
				t.Fatalf("effort report was %+v, want low", e)
			}
		})
	})

	t.Run("a bridge effort the model does not offer is skipped without error", func(t *testing.T) {
		rec := &recorder{}
		a := startAgentWith(t, rec, agentOpts{header: map[string]string{"X-AgentRQ-Agent-Effort": "max"}})
		l := a.readyAsking(rec)
		rec.wait(t, func() bool { return len(rec.effort) == 1 })
		a.sync(l)
		rec.read(func() {
			if e := rec.effort[0]; e.CurrentEffort != "high" {
				t.Fatalf("effort report was %+v, want the agent's own high", e)
			}
		})
	})

	t.Run("a failed switch, or a level nobody offers, re-reports what is true; an unknown session is left alone", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.readyAsking(rec)
		rec.wait(t, func() bool { return len(rec.effort) == 1 })
		l.SetEffort(context.Background(), "sess-1", "effort", "low")
		a.fail(a.expect("session/set_config_option"), codeInternalError, "no")
		rec.wait(t, func() bool { return len(rec.effort) == 2 })
		l.SetEffort(context.Background(), "sess-1", "effort", "max")
		rec.wait(t, func() bool { return len(rec.effort) == 3 })
		l.SetEffort(context.Background(), "nope", "effort", "low")
		a.quiet()
		rec.read(func() {
			for _, e := range rec.effort {
				if e.CurrentEffort != "high" {
					t.Fatalf("effort report was %+v, want high still current", e)
				}
			}
		})
	})

	t.Run("the first task adopting the idle session re-reports its level for the task", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.readyAsking(rec)
		rec.wait(t, func() bool { return len(rec.effort) == 1 })
		a.turn(rec, l, 42, "sess-1", "end_turn")
		rec.wait(t, func() bool { return len(rec.effort) == 2 })
		rec.read(func() {
			if e := rec.effort[1]; e.TaskID != b62(42) {
				t.Fatalf("the adopted session's level was reported for %q, want %q", e.TaskID, b62(42))
			}
		})
	})
}
