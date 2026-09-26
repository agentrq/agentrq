// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package schedule

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/robfig/cron/v3"
)

// defaultZone is what a schedule with no prefix is read in. robfig/cron would
// otherwise read it in the server's own zone, so the same schedule would fire
// at a different hour on a host that is not on UTC.
const defaultZone = "UTC"

// MaxLength is the width of the cron_schedule columns. Postgres refuses a
// longer value with a bare 500; SQLite stores it anyway.
const MaxLength = 64

var parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// split separates an optional `CRON_TZ=<zone> ` prefix from the five fields.
// A prefix with no schedule after it is rejected here because robfig/cron
// slices on the space it never finds and panics.
func split(schedule string) (string, string, error) {
	for _, prefix := range []string{"CRON_TZ=", "TZ="} {
		if !strings.HasPrefix(schedule, prefix) {
			continue
		}
		zone, spec, found := strings.Cut(schedule[len(prefix):], " ")
		if !found || zone == "" || strings.TrimSpace(spec) == "" {
			return "", "", fmt.Errorf("invalid cron schedule: %q names a timezone but no schedule", schedule)
		}
		// Go resolves "Local" to the host's zone, which is what naming one avoids.
		if zone == "Local" {
			return "", "", fmt.Errorf("invalid cron schedule: %q names the server's own zone; name an IANA zone such as Europe/Berlin", schedule)
		}
		return zone, strings.TrimSpace(spec), nil
	}
	return defaultZone, schedule, nil
}

// Fields returns the five cron fields with any timezone prefix removed, or nil
// if the prefix is malformed.
func Fields(schedule string) []string {
	_, spec, err := split(schedule)
	if err != nil {
		return nil
	}
	return strings.Fields(spec)
}

// Parse reads a schedule in the zone it names, or in UTC when it names none,
// whatever the host's own zone is.
func Parse(schedule string) (cron.Schedule, error) {
	zone, spec, err := split(schedule)
	if err != nil {
		return nil, err
	}
	sched, err := parser.Parse("CRON_TZ=" + zone + " " + spec)
	if err != nil {
		return nil, fmt.Errorf("invalid cron schedule: %w", err)
	}
	return sched, nil
}

// ValidateCronSyntax validates a standard 5-field cron schedule, with or
// without a timezone prefix.
func ValidateCronSyntax(schedule string) error {
	if len(schedule) > MaxLength {
		return fmt.Errorf("cron schedule is %d characters; at most %d can be stored", len(schedule), MaxLength)
	}
	_, err := Parse(schedule)
	return err
}

// ValidateCronGranularity validates the stricter agent-created schedule guardrail.
// Agent schedules have hourly-minimum granularity: the minute field must be a
// single fixed integer 0-59, not a wildcard, step, range, or comma-list.
func ValidateCronGranularity(schedule string) error {
	fields := Fields(schedule)
	if len(fields) != 5 {
		return fmt.Errorf("cron schedule must have exactly 5 fields (minute hour dom month dow)")
	}

	minuteField := fields[0]
	if minuteField == "*" ||
		strings.Contains(minuteField, "/") ||
		strings.Contains(minuteField, "-") ||
		strings.Contains(minuteField, ",") {
		return fmt.Errorf("cron schedule granularity too fine: minute field must be a single fixed value (0-59), not %q - only hourly or coarser schedules are allowed", minuteField)
	}

	minute, err := strconv.Atoi(minuteField)
	if err != nil || minute < 0 || minute > 59 {
		return fmt.Errorf("cron schedule minute field must be a valid integer between 0 and 59, got %q", minuteField)
	}

	return ValidateCronSyntax(schedule)
}
