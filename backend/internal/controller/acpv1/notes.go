// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The notes a turn can end with. The composer matches them word for word
// (frontend/src/composables/useAgentTurn.js, STOP_REASON_NOTE), so these are
// the gateway's texts exactly, "this gateway" included, plus AgentRQ's own
// for /logout.
const (
	noteRefusal         = "⚠️ The agent refused to continue this turn."
	noteMaxTokens       = "⚠️ The agent stopped mid-turn: it ran out of output tokens."
	noteMaxTurnRequests = "⚠️ The agent stopped mid-turn: it reached its limit on model requests for a single turn."
	noteCancelled       = "⚠️ The turn was cancelled before the agent finished."
	noteUnknownPrefix   = "⚠️ The agent stopped for a reason this gateway does not recognise: "
	noteProcessExited   = "⚠️ The agent stopped mid-turn: its process exited."
	noteConnectionLost  = "⚠️ The agent stopped mid-turn: the connection to its machine was lost."
	noteErrorPrefix     = "⚠️ The agent stopped mid-turn: it answered with an error ("
	// noteLoggedOut is AgentRQ's own, for a /logout the agent accepted.
	noteLoggedOut = "⚠️ The agent logged out: its next session asks for a login again."
)

// maxNoteErrorRunes is how much of an agent's error message a note quotes.
const maxNoteErrorRunes = 200

// stopNote is the note a turn's stop reason ends it with, "" for none.
func stopNote(reason string) string {
	switch reason {
	case "end_turn":
		return ""
	case "refusal":
		return noteRefusal
	case "max_tokens":
		return noteMaxTokens
	case "max_turn_requests":
		return noteMaxTurnRequests
	case "cancelled":
		return noteCancelled
	}
	return noteUnknownPrefix + reason + "."
}

func noteAnsweredWithError(message string) string {
	if utf8.RuneCountInString(message) > maxNoteErrorRunes {
		message = string([]rune(message)[:maxNoteErrorRunes])
	}
	return noteErrorPrefix + message + ")."
}

// groupThousands writes n the way the gateway's toLocaleString("en-US") does.
func groupThousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	var b strings.Builder
	for i := range len(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(s[i])
	}
	return sign + b.String()
}

func contextPercent(u usageUpdate) (int64, bool) {
	if u.Size <= 0 {
		return 0, false
	}
	return int64(math.Round(float64(u.Used) / float64(u.Size) * 100)), true
}

// formatCost shows a fraction of a cent to four places, since "0.00 USD" says
// nothing, and anything else to two.
func formatCost(c cost) string {
	if c.Amount != 0 && math.Abs(c.Amount) < 0.01 {
		return fmt.Sprintf("%.4f %s", c.Amount, c.Currency)
	}
	return fmt.Sprintf("%.2f %s", c.Amount, c.Currency)
}

func formatUsage(u usageUpdate) string {
	s := "Context " + groupThousands(u.Used) + " / " + groupThousands(u.Size) + " tokens"
	if p, ok := contextPercent(u); ok {
		s += fmt.Sprintf(" (%d%%)", p)
	}
	if u.Cost != nil {
		s += " · " + formatCost(*u.Cost)
	}
	return s
}

func usageData(u usageUpdate) map[string]any {
	d := map[string]any{"used": u.Used, "size": u.Size}
	if p, ok := contextPercent(u); ok {
		d["percent"] = p
	}
	if u.Cost != nil {
		d["cost"] = map[string]any{"amount": u.Cost.Amount, "currency": u.Cost.Currency}
	}
	return d
}

var planMarkers = map[string]string{"completed": "✅", "in_progress": "🔄", "pending": "⬜"}

func formatPlan(p planContent) string {
	switch p.Type {
	case "file":
		return "Plan: " + p.URI
	case "markdown":
		return p.Content
	}
	lines := make([]string, 0, len(p.Entries))
	for _, e := range p.Entries {
		marker, ok := planMarkers[e.Status]
		if !ok {
			marker = planMarkers["pending"]
		}
		lines = append(lines, "- "+marker+" "+e.Content)
	}
	return strings.Join(lines, "\n")
}

func planData(p planContent) map[string]any {
	d := map[string]any{"planId": p.PlanID, "planType": p.Type}
	switch p.Type {
	case "file":
		d["uri"] = p.URI
	case "markdown":
		d["content"] = p.Content
	default:
		entries := make([]map[string]any, 0, len(p.Entries))
		for _, e := range p.Entries {
			entries = append(entries, map[string]any{"content": e.Content, "priority": e.Priority, "status": e.Status})
		}
		d["entries"] = entries
	}
	return d
}
