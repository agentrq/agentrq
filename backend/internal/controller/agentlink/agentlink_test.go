// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package agentlink

import "testing"

func TestSelectable(t *testing.T) {
	if (ModelsSnapshot{CanSet: true}).Selectable() || (EffortSnapshot{CanSet: true}).Selectable() {
		t.Error("Selectable() = true with no config option, want false")
	}
	if (ModelsSnapshot{ConfigID: "model"}).Selectable() || (EffortSnapshot{ConfigID: "effort"}).Selectable() {
		t.Error("Selectable() = true when the agent never said it can set one, want false")
	}
	if !(ModelsSnapshot{ConfigID: "model", CanSet: true}).Selectable() || !(EffortSnapshot{ConfigID: "effort", CanSet: true}).Selectable() {
		t.Error("Selectable() = false for a willing agent with a config option, want true")
	}
}

func TestCurrentModelName(t *testing.T) {
	models := []Model{{ID: "a", Name: " Alpha "}, {ID: "b", Name: " "}}
	for _, tc := range []struct{ current, want string }{{"", ""}, {"a", "Alpha"}, {"b", "b"}, {"gone", "gone"}} {
		if got := (ModelsSnapshot{CurrentModel: tc.current, Models: models}).CurrentModelName(); got != tc.want {
			t.Errorf("CurrentModelName() for %q = %q, want %q", tc.current, got, tc.want)
		}
	}
}

func TestCurrentModelID(t *testing.T) {
	flagged := []Model{{ID: "a"}, {ID: "b", Current: true}}
	for _, tc := range []struct {
		name string
		p    ModelsReport
		want string
	}{
		{"the top-level field wins", ModelsReport{CurrentModel: "a", Models: flagged}, "a"},
		{"else the flagged entry", ModelsReport{Models: flagged}, "b"},
		{"else nothing", ModelsReport{Models: []Model{{ID: "a"}}}, ""},
	} {
		if got := tc.p.CurrentModelID(); got != tc.want {
			t.Errorf("%s: CurrentModelID() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestTelemetryReport(t *testing.T) {
	p := TelemetryReport{Data: map[string]any{"removed": true}}
	if !p.Removed() {
		t.Error("Removed() = false, want true")
	}
	var empty TelemetryReport
	if empty.Removed() {
		t.Error("Removed() = true with no data, want false")
	}
}
