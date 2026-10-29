// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package stream

import (
	"testing"
	"time"
	_ "time/tzdata" // the zones a notice names, on a machine without them
)

func TestResetTime(t *testing.T) {
	at := func(s string) *time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return &v
	}
	cases := []struct {
		name, notice string
		want         *time.Time
	}{
		{"later today", "5-hour limit reached ∙ resets 5pm", at("2026-09-15T17:00:00Z")},
		{"already passed today", "You've hit your limit · resets 5am", at("2026-09-16T05:00:00Z")},
		{"noon is not midnight", "limit reached · resets 12pm", at("2026-09-16T12:00:00Z")},
		{"midnight", "limit reached · resets 12am", at("2026-09-16T00:00:00Z")},
		{"minutes, in a named zone", "You've hit your session limit · resets 2:30pm (Europe/Istanbul)", at("2026-09-16T11:30:00Z")},
		{"an unknown zone reads as the machine's", "Claude usage limit reached. Your limit will reset at 5pm (Not/AZone).", at("2026-09-15T17:00:00Z")},
		{"a date", "Weekly limit reached ∙ resets Oct 31, 5pm", at("2026-10-31T17:00:00Z")},
		{"a date already gone this year", "Weekly limit reached ∙ resets Sep 1 at 5pm", at("2027-09-01T17:00:00Z")},
		{"not a month", "Weekly limit reached ∙ resets Foo 1, 5pm", nil},
		{"no time", "limit reached", nil},
		{"no such hour", "limit reached ∙ resets 13pm", nil},
		{"no such minute", "limit reached ∙ resets 5:75pm", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResetTime(c.notice, t0)
			switch {
			case c.want == nil && got != nil:
				t.Fatalf("ResetTime() = %v, want nil", got)
			case c.want != nil && (got == nil || !got.Equal(*c.want)):
				t.Fatalf("ResetTime() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestUsageLimitNotice(t *testing.T) {
	cases := []struct {
		name, out, want string
	}{
		{"nothing printed", "", ""},
		{"a warning is not a pause", "Approaching usage limit · resets 5pm", ""},
		{"the notice", "working\r\nYou've hit your limit · resets 5pm\r\n> ", "You've hit your limit · resets 5pm"},
		{"wrapped over rows", "5-hour limit reached ∙ resets 5pm", "5-hour limit reached ∙ resets 5pm"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := NewScreen(20, 6)
			if _, err := s.Write([]byte(c.out)); err != nil {
				t.Fatal(err)
			}
			if got := s.UsageLimitNotice(); got != c.want {
				t.Fatalf("UsageLimitNotice() = %q, want %q", got, c.want)
			}
		})
	}
}

// A notice still on screen after its reset time has passed must keep that
// time, not move on to the same hour tomorrow.
func TestActivityReadsTheResetFromWhenTheNoticeAppeared(t *testing.T) {
	p, _, now := newTestPump(t)
	p.Screen = NewScreen(60, 6)
	if err := p.Feed([]byte("working")); err != nil {
		t.Fatal(err)
	}
	*now = t0.Add(90 * time.Second)
	a := p.Activity()
	if a.SessionID != 7 || a.IdleSec != 90 || a.UsageLimited || a.ResetsAt != nil {
		t.Fatalf("Activity() = %+v, want 90s idle and no limit", a)
	}

	if err := p.Feed([]byte("\r\nYou've hit your limit · resets 5pm")); err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 9, 15, 17, 0, 0, 0, time.UTC)
	if a := p.Activity(); !a.UsageLimited || a.IdleSec != 0 || a.ResetsAt == nil || !a.ResetsAt.Equal(want) {
		t.Fatalf("Activity() = %+v, want limited until %v", a, want)
	}
	*now = t0.Add(6 * time.Hour)
	if a := p.Activity(); a.ResetsAt == nil || !a.ResetsAt.Equal(want) {
		t.Fatalf("Activity() at 6pm = %+v, want the reset still %v", a, want)
	}

	if err := p.Feed([]byte("\x1b[2J")); err != nil {
		t.Fatal(err)
	}
	if a := p.Activity(); a.UsageLimited || a.ResetsAt != nil {
		t.Fatalf("Activity() after a clear = %+v, want no limit", a)
	}
}
