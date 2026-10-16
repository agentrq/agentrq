// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package agentlink

import "testing"

func intPtr(v int) *int    { return &v }
func boolPtr(v bool) *bool { return &v }

func TestConcurrencyReport(t *testing.T) {
	if got := (ConcurrencyReport{}).Limit(); got != 0 {
		t.Errorf("Limit() with none reported = %d, want 0", got)
	}
	if got := (ConcurrencyReport{MaxConcurrency: intPtr(4)}).Limit(); got != 4 {
		t.Errorf("Limit() = %d, want 4", got)
	}
	for _, tc := range []struct {
		canSet *bool
		want   bool
	}{{nil, false}, {boolPtr(false), false}, {boolPtr(true), true}} {
		if got := (ConcurrencyReport{CanSet: tc.canSet}).OffersSet(); got != tc.want {
			t.Errorf("OffersSet() with CanSet %v = %v, want %v", tc.canSet, got, tc.want)
		}
	}
}

func TestConcurrencySnapshot(t *testing.T) {
	for _, tc := range []struct {
		name               string
		s                  ConcurrencySnapshot
		reported, settable bool
	}{
		{"nothing reported", ConcurrencySnapshot{CanSet: true}, false, false},
		{"read only", ConcurrencySnapshot{MaxConcurrency: 2}, true, false},
		{"settable", ConcurrencySnapshot{MaxConcurrency: 2, CanSet: true}, true, true},
	} {
		if got := tc.s.Reported(); got != tc.reported {
			t.Errorf("%s: Reported() = %v, want %v", tc.name, got, tc.reported)
		}
		if got := tc.s.Settable(); got != tc.settable {
			t.Errorf("%s: Settable() = %v, want %v", tc.name, got, tc.settable)
		}
	}
}

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
	p := TelemetryReport{Data: map[string]any{"planId": "p1", "removed": true}}
	if p.PlanID() != "p1" || !p.Removed() {
		t.Errorf("PlanID() = %q and Removed() = %v, want p1 and true", p.PlanID(), p.Removed())
	}
	var empty TelemetryReport
	if empty.PlanID() != "" || empty.Removed() {
		t.Errorf("PlanID() = %q and Removed() = %v with no data, want empty and false", empty.PlanID(), empty.Removed())
	}
}
