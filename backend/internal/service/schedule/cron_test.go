// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package schedule

import (
	"strings"
	"testing"
	"time"
)

func TestValidateCronGranularity(t *testing.T) {
	validCases := []string{
		"0 * * * *",
		"30 * * * *",
		"0 9 * * *",
		"0 9 * * 1",
		"0 9 1 * *",
		"59 23 * * *",
		"0 */2 * * *",
		"30 9 * * 1-5",
		"30 14 25 4 *",
		"5 9 1 1 *",
	}

	for _, s := range validCases {
		if err := ValidateCronGranularity(s); err != nil {
			t.Errorf("expected valid for %q, got error: %v", s, err)
		}
	}

	invalidCases := []struct {
		schedule string
		errFrag  string
	}{
		{"* * * * *", "granularity too fine"},
		{"*/5 * * * *", "granularity too fine"},
		{"*/15 * * * *", "granularity too fine"},
		{"0,30 * * * *", "granularity too fine"},
		{"0-5 * * * *", "granularity too fine"},
		{"60 * * * *", "must be a valid integer"},
		{"abc * * * *", "must be a valid integer"},
		{"not-a-cron", "must have exactly 5 fields"},
	}

	for _, tc := range invalidCases {
		if err := ValidateCronGranularity(tc.schedule); err == nil {
			t.Errorf("expected error for %q, got nil", tc.schedule)
		} else if !strings.Contains(err.Error(), tc.errFrag) {
			t.Errorf("expected error containing %q for %q, got: %v", tc.errFrag, tc.schedule, err)
		}
	}
}

func TestValidateCronSyntax_AllowsSubHourlySchedules(t *testing.T) {
	validCases := []string{
		"* * * * *",
		"*/5 * * * *",
		"*/15 * * * *",
		"0,30 * * * *",
		"0-5 * * * *",
	}

	for _, s := range validCases {
		if err := ValidateCronSyntax(s); err != nil {
			t.Errorf("expected syntax-valid cron %q, got error: %v", s, err)
		}
	}
}

func TestTimezonePrefix(t *testing.T) {
	validCases := []string{
		"CRON_TZ=Pacific/Auckland 0 9 1 * *",
		"CRON_TZ=America/New_York 30 6 15 * *",
		"TZ=UTC 0 9 * * *",
		"CRON_TZ=UTC */15 * * * *",
		// The longest the schedule picker writes: a long zone and every day.
		"CRON_TZ=America/North_Dakota/New_Salem 30 12 * * 0,1,2,3,4,5,6",
	}

	for _, s := range validCases {
		if err := ValidateCronSyntax(s); err != nil {
			t.Errorf("expected valid for %q, got error: %v", s, err)
		}
	}

	invalidCases := []struct {
		schedule string
		errFrag  string
	}{
		// A prefix naming no schedule: robfig/cron slices on the space it
		// never finds, so this has to be caught before it reaches the parser.
		{"CRON_TZ=UTC", "no schedule"},
		{"CRON_TZ=", "no schedule"},
		{"TZ=UTC", "no schedule"},
		{"CRON_TZ=UTC ", "no schedule"},
		{"CRON_TZ=Not/AZone 0 9 * * *", "bad location"},
		{"CRON_TZ=Local 0 9 * * *", "server's own zone"},
		// 68 characters: past the column, which Postgres refuses with a 500.
		{"CRON_TZ=America/Argentina/Buenos_Aires 0 8,10,12,14,16 * * 1,2,3,4,5", "at most 64"},
	}

	for _, tc := range invalidCases {
		err := ValidateCronSyntax(tc.schedule)
		if err == nil {
			t.Errorf("expected error for %q, got nil", tc.schedule)
		} else if !strings.Contains(err.Error(), tc.errFrag) {
			t.Errorf("expected error containing %q for %q, got: %v", tc.errFrag, tc.schedule, err)
		}
	}
}

func TestValidateCronGranularity_CountsFieldsWithoutThePrefix(t *testing.T) {
	if err := ValidateCronGranularity("CRON_TZ=Pacific/Auckland 0 9 1 * *"); err != nil {
		t.Errorf("expected valid, got error: %v", err)
	}
	if err := ValidateCronGranularity("CRON_TZ=Pacific/Auckland */5 * * * *"); err == nil {
		t.Error("expected the minute guardrail to apply through the prefix, got nil")
	}
}

func TestFields(t *testing.T) {
	cases := map[string]int{
		"0 9 1 * *":                          5,
		"CRON_TZ=Pacific/Auckland 0 9 1 * *": 5,
		"TZ=UTC 0 9 1 * *":                   5,
		"CRON_TZ=UTC":                        0,
	}
	for schedule, want := range cases {
		if got := len(Fields(schedule)); got != want {
			t.Errorf("Fields(%q) returned %d fields, want %d", schedule, got, want)
		}
	}
}

// A schedule with no zone means UTC. robfig/cron on its own reads it in the
// host's zone, and the task fires hours off on any server not on UTC.
func TestParse_BareScheduleIsUTCWhateverTheHostZone(t *testing.T) {
	original := time.Local
	time.Local = time.FixedZone("Test/Plus5", 5*60*60)
	defer func() { time.Local = original }()

	from := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	sched, err := Parse("0 9 * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if next := sched.Next(from); next.UTC().Hour() != 9 {
		t.Errorf("bare schedule fired at %v, want 09:00 UTC", next.UTC())
	}

	sched, err = Parse("CRON_TZ=Pacific/Auckland 0 9 * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	auckland, _ := time.LoadLocation("Pacific/Auckland")
	if next := sched.Next(from); next.In(auckland).Hour() != 9 {
		t.Errorf("zoned schedule fired at %v, want 09:00 in Auckland", next.In(auckland))
	}
}

// A prefix with nothing after it panics inside robfig/cron.
func TestParse_RefusesAPrefixWithNoSchedule(t *testing.T) {
	if _, err := Parse("CRON_TZ=UTC"); err == nil {
		t.Error("expected an error, got nil")
	}
}
