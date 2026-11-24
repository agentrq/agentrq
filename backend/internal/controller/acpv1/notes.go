// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package acpv1

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The notes a turn can end with, for the human: why the agent stopped before
// it was done. They do not end the turn; the usage footer does (markTurnEnd),
// so the wording is free to change. A Stop the human pressed gets no note.
const (
	noteRefusal         = "⚠️ The agent declined to continue."
	noteMaxTokens       = "⚠️ The agent stopped: its answer reached the model's output limit."
	noteMaxTurnRequests = "⚠️ The agent stopped: it reached its limit of model requests for one turn."
	noteUnknownPrefix   = "⚠️ The agent stopped early: "
	noteProcessExited   = "⚠️ The agent stopped: its process exited."
	noteConnectionLost  = "⚠️ The agent stopped: the connection to its machine was lost."
	noteGatewayShutDown = "⚠️ The agent stopped: its gateway was shut down."
	noteErrorPrefix     = "⚠️ The agent hit an error: "
	noteLoggedOut       = "⚠️ The agent logged out: its next session asks for a login again."
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
		return "" // the human's own Stop: the toast already said so
	}
	return noteUnknownPrefix + reason + "."
}

func noteAnsweredWithError(message string) string {
	if utf8.RuneCountInString(message) > maxNoteErrorRunes {
		message = string([]rune(message)[:maxNoteErrorRunes])
	}
	return noteErrorPrefix + message
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

// formatPlan is a plan as the markdown a task keeps it in: entries as a task
// list, ticked once completed, and a file plan as a link to it.
func formatPlan(p planContent) string {
	switch p.Type {
	case "file":
		return "[Plan](" + p.URI + ")"
	case "markdown":
		return p.Content
	}
	lines := make([]string, 0, len(p.Entries))
	for _, e := range p.Entries {
		box := "[ ]"
		if e.Status == "completed" {
			box = "[x]"
		}
		lines = append(lines, "- "+box+" "+e.Content)
	}
	return strings.Join(lines, "\n")
}
