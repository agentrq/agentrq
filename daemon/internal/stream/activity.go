// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package stream

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/agentrq/agentrq/daemon/wire"
)

// What the heartbeat says about a session's agent: how long its terminal has
// been quiet, and whether a usage-limit notice is on screen. A paused agent
// calls nothing and prints nothing more, so its screen is the only place the
// pause shows.

var (
	// limitNotice matches Claude Code's notices: "5-hour limit reached ∙
	// resets 5pm", "You've hit your limit · resets 5am (Europe/Istanbul)",
	// "Claude usage limit reached. Your limit will reset at 5pm (…)". Not
	// "Approaching usage limit", which is a warning, not a pause.
	limitNotice = regexp.MustCompile(`(?i)(limit reached|hit your (?:[a-z-]+ )?limit)`)
	// resetAt is the time in one: "resets 5pm", "reset at 2:30pm (Zone/City)",
	// "resets Oct 31, 5pm".
	resetAt = regexp.MustCompile(`(?i)\bresets?(?:\s+at)?\s+(?:([a-z]{3})[a-z]*\s+(\d{1,2}),?\s+(?:at\s+)?)?(\d{1,2})(?::(\d{2}))?\s*([ap]m)(?:\s*\(([^)\s]+)\))?`)
)

// Activity is the session's line in the heartbeat.
//
// The reset time is read against when the notice first appeared, not now:
// "resets 5pm" still on screen at 6pm means today's 5pm, which has passed, and
// reading it at 6pm would move it to tomorrow.
func (p *Pump) Activity() wire.SessionActivity {
	line := p.Screen.UsageLimitNotice()
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.Now()
	if line != p.limitLine {
		p.limitLine, p.limitSeen = line, now
	}
	a := wire.SessionActivity{SessionID: p.SessionID, IdleSec: int64(now.Sub(p.lastOutput) / time.Second)}
	if line != "" {
		a.UsageLimited = true
		a.ResetsAt = ResetTime(line, p.limitSeen)
	}
	return a
}

// UsageLimitNotice is the last line on screen saying the agent hit its usage
// limit, with the rows the terminal wrapped it across joined, or "".
func (s *Screen) UsageLimitNotice() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	found := ""
	var line strings.Builder
	for y := 0; y < s.rows; y++ {
		if s.term.Cell(s.cols-1, y).Mode&wrapped != 0 {
			line.WriteString(s.rowLocked(y))
			continue
		}
		line.WriteString(s.rowTextLocked(y))
		if text := strings.TrimSpace(line.String()); limitNotice.MatchString(text) {
			found = text
		}
		line.Reset()
	}
	return found
}

// ResetTime is when a usage-limit notice says the limit lifts, read as the
// first such time after seen, in the zone it names or else seen's own; nil
// when it gives none.
func ResetTime(notice string, seen time.Time) *time.Time {
	m := resetAt.FindStringSubmatch(notice)
	if m == nil {
		return nil
	}
	loc := seen.Location()
	if m[6] != "" {
		if l, err := time.LoadLocation(m[6]); err == nil {
			loc = l
		}
	}
	hour, _ := strconv.Atoi(m[3])
	minute, _ := strconv.Atoi(m[4]) // "" reads as 0
	if hour < 1 || hour > 12 || minute > 59 {
		return nil
	}
	hour %= 12
	if strings.EqualFold(m[5], "pm") {
		hour += 12
	}
	local := seen.In(loc)
	if m[1] == "" {
		t := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, loc)
		if !t.After(local) {
			t = t.AddDate(0, 0, 1)
		}
		t = t.UTC()
		return &t
	}
	month, err := time.Parse("Jan", strings.ToUpper(m[1][:1])+strings.ToLower(m[1][1:]))
	if err != nil {
		return nil
	}
	day, _ := strconv.Atoi(m[2])
	t := time.Date(local.Year(), month.Month(), day, hour, minute, 0, 0, loc)
	if !t.After(local) {
		t = t.AddDate(1, 0, 0)
	}
	t = t.UTC()
	return &t
}
