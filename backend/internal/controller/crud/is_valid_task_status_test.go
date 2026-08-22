package crud

import "testing"

func TestIsValidTaskStatus(t *testing.T) {
	for _, s := range []string{"notstarted", "ongoing", "completed", "rejected", "cron", "blocked"} {
		if !IsValidTaskStatus(s) {
			t.Errorf("expected %q to be valid", s)
		}
	}
	for _, s := range []string{"", "garbage", "Ongoing", "done", "complete", "COMPLETED"} {
		if IsValidTaskStatus(s) {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}
