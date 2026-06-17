package schedule

import (
	"strings"
	"testing"
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
