package schedule

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/robfig/cron/v3"
)

// ValidateCronSyntax validates a standard 5-field cron schedule.
func ValidateCronSyntax(schedule string) error {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if _, err := parser.Parse(schedule); err != nil {
		return fmt.Errorf("invalid cron schedule: %w", err)
	}
	return nil
}

// ValidateCronGranularity validates the stricter agent-created schedule guardrail.
// Agent schedules have hourly-minimum granularity: the minute field must be a
// single fixed integer 0-59, not a wildcard, step, range, or comma-list.
func ValidateCronGranularity(schedule string) error {
	fields := strings.Fields(schedule)
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
