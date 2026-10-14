// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNotes(t *testing.T) {
	cases := []struct{ reason, want string }{
		{"refusal", "⚠️ The agent refused to continue this turn."},
		{"max_tokens", "⚠️ The agent stopped mid-turn: it ran out of output tokens."},
		{"max_turn_requests", "⚠️ The agent stopped mid-turn: it reached its limit on model requests for a single turn."},
		{"cancelled", "⚠️ The turn was cancelled before the agent finished."},
		{"tea_break", "⚠️ The agent stopped for a reason this gateway does not recognise: tea_break."},
	}
	for _, c := range cases {
		t.Run("the "+c.reason+" note is the gateway's own text", func(t *testing.T) {
			if got := stopNote(c.reason); got != c.want {
				t.Errorf("stopNote(%q) was %q, want %q", c.reason, got, c.want)
			}
		})
	}
	t.Run("end_turn has no note", func(t *testing.T) {
		if got := stopNote("end_turn"); got != "" {
			t.Errorf("stopNote(end_turn) was %q, want none", got)
		}
	})
	t.Run("the process-exited and connection-lost notes start the way the composer expects", func(t *testing.T) {
		for _, n := range []string{noteProcessExited, noteConnectionLost} {
			if !strings.HasPrefix(n, "⚠️ The agent stopped mid-turn: ") {
				t.Errorf("note %q does not start with the mid-turn prefix the composer matches", n)
			}
		}
	})
	t.Run("an error note quotes at most 200 characters of the agent's message", func(t *testing.T) {
		long := "boom" + strings.Repeat("é", 296)
		got := noteAnsweredWithError(long)
		quoted := strings.TrimSuffix(strings.TrimPrefix(got, noteErrorPrefix), ").")
		if n := utf8.RuneCountInString(quoted); n != 200 {
			t.Errorf("the note quoted %d characters, want 200", n)
		}
		if got := noteAnsweredWithError("model overloaded"); got != "⚠️ The agent stopped mid-turn: it answered with an error (model overloaded)." {
			t.Errorf("a short message read %q, want it quoted whole", got)
		}
	})
	t.Run("usage reads like the gateway's footer", func(t *testing.T) {
		got := formatUsage(usageUpdate{Used: 53000, Size: 200000, Cost: &cost{Amount: 0.0042, Currency: "USD"}})
		if want := "Context 53,000 / 200,000 tokens (27%) · 0.0042 USD"; got != want {
			t.Errorf("formatUsage was %q, want %q", got, want)
		}
	})
	t.Run("a window of unknown size has no percentage", func(t *testing.T) {
		if got := formatUsage(usageUpdate{Used: 1200, Size: 0}); got != "Context 1,200 / 0 tokens" {
			t.Errorf("formatUsage was %q, want no percentage", got)
		}
	})
	t.Run("thousands are grouped as en-US writes them, sign included", func(t *testing.T) {
		for n, want := range map[int64]string{0: "0", 999: "999", 1000: "1,000", 1234567: "1,234,567", -45000: "-45,000"} {
			if got := groupThousands(n); got != want {
				t.Errorf("groupThousands(%d) was %q, want %q", n, got, want)
			}
		}
	})
	t.Run("a cost of a cent or more shows two decimals", func(t *testing.T) {
		if got := formatCost(cost{Amount: 1.5, Currency: "USD"}); got != "1.50 USD" {
			t.Errorf("formatCost(1.5) was %q, want 1.50 USD", got)
		}
	})
	t.Run("a zero cost shows two decimals", func(t *testing.T) {
		if got := formatCost(cost{Amount: 0, Currency: "USD"}); got != "0.00 USD" {
			t.Errorf("formatCost(0) was %q, want 0.00 USD", got)
		}
	})
	t.Run("usage data carries used, size, percent and cost", func(t *testing.T) {
		got, _ := json.Marshal(usageData(usageUpdate{Used: 53000, Size: 200000, Cost: &cost{Amount: 0.0042, Currency: "USD"}}))
		if want := `{"cost":{"amount":0.0042,"currency":"USD"},"percent":27,"size":200000,"used":53000}`; string(got) != want {
			t.Errorf("usageData was %s, want %s", got, want)
		}
	})
	t.Run("usage data of a window of unknown size has no percent and no cost", func(t *testing.T) {
		got, _ := json.Marshal(usageData(usageUpdate{Used: 1, Size: 0}))
		if want := `{"size":0,"used":1}`; string(got) != want {
			t.Errorf("usageData was %s, want %s", got, want)
		}
	})
	t.Run("a plan reads as ticked checklist lines", func(t *testing.T) {
		got := formatPlan(planContent{Type: "items", PlanID: "p", Entries: []planEntry{
			{Content: "Read", Status: "completed"}, {Content: "Fix", Status: "in_progress"}, {Content: "Ship", Status: "weird"}}})
		if want := "- ✅ Read\n- 🔄 Fix\n- ⬜ Ship"; got != want {
			t.Errorf("formatPlan was %q, want %q", got, want)
		}
	})
	t.Run("a file plan reads as its uri", func(t *testing.T) {
		if got := formatPlan(planContent{Type: "file", URI: "file:///plan.md"}); got != "Plan: file:///plan.md" {
			t.Errorf("formatPlan was %q, want Plan: file:///plan.md", got)
		}
	})
	t.Run("a markdown plan reads as itself", func(t *testing.T) {
		if got := formatPlan(planContent{Type: "markdown", Content: "# Plan"}); got != "# Plan" {
			t.Errorf("formatPlan was %q, want # Plan", got)
		}
	})
	t.Run("plan data drops nothing the card needs", func(t *testing.T) {
		got, _ := json.Marshal(planData(planContent{Type: "items", PlanID: "p1", Entries: []planEntry{{Content: "Read", Priority: "high", Status: "completed"}}}))
		if want := `{"entries":[{"content":"Read","priority":"high","status":"completed"}],"planId":"p1","planType":"items"}`; string(got) != want {
			t.Errorf("planData was %s, want %s", got, want)
		}
		got, _ = json.Marshal(planData(planContent{Type: "file", PlanID: "p2", URI: "file:///p"}))
		if want := `{"planId":"p2","planType":"file","uri":"file:///p"}`; string(got) != want {
			t.Errorf("planData of a file plan was %s, want %s", got, want)
		}
		got, _ = json.Marshal(planData(planContent{Type: "markdown", PlanID: "p3", Content: "x"}))
		if want := `{"content":"x","planId":"p3","planType":"markdown"}`; string(got) != want {
			t.Errorf("planData of a markdown plan was %s, want %s", got, want)
		}
	})
}
