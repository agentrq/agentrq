// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// Package tasklatency is the arithmetic behind the task latency statistics:
// the summary a rollup row keeps for one metric, how two of them merge, how
// p50 is read back out, and the time buckets a period is split into.
//
// A rollup cannot keep every value, so p50 comes from a fixed histogram that
// merges by adding counts. min and max merge exactly. The p50 is therefore an
// estimate, interpolated inside the bucket the median falls in and clamped to
// [min, max] — which makes it exact for a bucket holding a single task.
package tasklatency

import (
	"strconv"
	"strings"
	"time"
)

// Bounds are the histogram's upper edges in seconds, log-spaced minutes:
// <1m, 1–2m, 2–5m, … 14–30d, and one open bucket past 30 days. They are
// stored, as the counts in each position, so they are never reordered or
// changed; a finer histogram would be a new column.
var Bounds = []int64{
	60, 120, 300, 600, 900, 1800, // minutes
	3600, 7200, 14400, 28800, 43200, // hours
	86400, 172800, 345600, 604800, 1209600, 2592000, // days
}

// Buckets is how many counts a histogram holds: one per bound and one past
// the last.
var Buckets = len(Bounds) + 1

// Stats summarises the values of one metric in one period.
type Stats struct {
	Count int64
	Sum   int64
	Min   int64
	Max   int64
	Hist  []int64
}

// Add counts one value in.
func (s *Stats) Add(v int64) {
	if v < 0 {
		v = 0
	}
	s.ensure()
	if s.Count == 0 || v < s.Min {
		s.Min = v
	}
	if s.Count == 0 || v > s.Max {
		s.Max = v
	}
	s.Count++
	s.Sum += v
	s.Hist[bucketOf(v)]++
}

// Merge folds o into s, as if every value of o had been added to s.
func (s *Stats) Merge(o Stats) {
	if o.Count == 0 {
		return
	}
	s.ensure()
	if s.Count == 0 || o.Min < s.Min {
		s.Min = o.Min
	}
	if s.Count == 0 || o.Max > s.Max {
		s.Max = o.Max
	}
	s.Count += o.Count
	s.Sum += o.Sum
	for i := 0; i < len(o.Hist) && i < Buckets; i++ {
		s.Hist[i] += o.Hist[i]
	}
}

func (s *Stats) ensure() {
	if len(s.Hist) != Buckets {
		h := make([]int64, Buckets)
		copy(h, s.Hist)
		s.Hist = h
	}
}

func bucketOf(v int64) int {
	for i, b := range Bounds {
		if v < b {
			return i
		}
	}
	return len(Bounds)
}

// Aggregates the API offers. There is no average and no p99 by design.
const (
	AggregateP50 = "p50"
	AggregateMin = "min"
	AggregateMax = "max"
)

// ValidAggregate reports whether a is one of the aggregates above.
func ValidAggregate(a string) bool {
	return a == AggregateP50 || a == AggregateMin || a == AggregateMax
}

// Value reads the aggregate out, in whole seconds. ok is false for an empty
// Stats: a period with nothing in it is a gap, not a zero.
func (s Stats) Value(aggregate string) (v int64, ok bool) {
	if s.Count == 0 {
		return 0, false
	}
	switch aggregate {
	case AggregateMin:
		return s.Min, true
	case AggregateMax:
		return s.Max, true
	}
	return s.p50(), true
}

func (s Stats) p50() int64 {
	target := float64(s.Count) / 2
	var before int64
	for i, n := range s.Hist {
		if n == 0 || float64(before+n) < target {
			before += n
			continue
		}
		lower, upper := float64(0), float64(s.Max)
		if i > 0 {
			lower = float64(Bounds[i-1])
		}
		if i < len(Bounds) {
			upper = float64(Bounds[i])
		}
		v := int64(lower + (target-float64(before))/float64(n)*(upper-lower))
		return min(max(v, s.Min), s.Max)
	}
	// Only reachable with a histogram that disagrees with Count.
	return s.Max
}

// EncodeHist is the histogram as stored: its counts, comma separated.
func EncodeHist(h []int64) string {
	parts := make([]string, Buckets)
	for i := range parts {
		var n int64
		if i < len(h) {
			n = h[i]
		}
		parts[i] = strconv.FormatInt(n, 10)
	}
	return strings.Join(parts, ",")
}

// Decode rebuilds the Stats a stored rollup row describes.
func Decode(count, sum, minimum, maximum int64, hist string) Stats {
	return Stats{Count: count, Sum: sum, Min: minimum, Max: maximum, Hist: DecodeHist(hist)}
}

// DecodeHist reads a stored histogram back. A malformed count reads as zero
// rather than failing the whole statistics request.
func DecodeHist(s string) []int64 {
	h := make([]int64, Buckets)
	for i, p := range strings.Split(s, ",") {
		if i >= Buckets {
			break
		}
		h[i], _ = strconv.ParseInt(p, 10, 64)
	}
	return h
}

// Granularity is the width of one bucket of a statistics series.
type Granularity string

const (
	Hour  Granularity = "hour"
	Day   Granularity = "day"
	Month Granularity = "month"
)

// GranularityFor picks the bucket width for a period: hours up to two days,
// days up to about a quarter, months beyond that, so a series stays between a
// couple of dozen and about a hundred points.
func GranularityFor(start, end int64) Granularity {
	span := end - start
	switch {
	case span <= 2*86400:
		return Hour
	case span <= 92*86400:
		return Day
	}
	return Month
}

// Floor is the start of the bucket t falls in, in UTC — the rollups are keyed
// in UTC.
func Floor(t int64, g Granularity) int64 {
	u := time.Unix(t, 0).UTC()
	switch g {
	case Hour:
		return u.Truncate(time.Hour).Unix()
	case Day:
		return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC).Unix()
	}
	return time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC).Unix()
}

// Next is the start of the bucket after the one starting at t.
func Next(t int64, g Granularity) int64 {
	u := time.Unix(t, 0).UTC()
	switch g {
	case Hour:
		return u.Add(time.Hour).Unix()
	case Day:
		return u.AddDate(0, 0, 1).Unix()
	}
	return u.AddDate(0, 1, 0).Unix()
}

// Periods lists the start of every bucket that overlaps [start, end].
func Periods(start, end int64, g Granularity) []int64 {
	var out []int64
	for p := Floor(start, g); p <= end; p = Next(p, g) {
		out = append(out, p)
	}
	return out
}

// Claim types for the latency rollups in telemetry_aggregations, and the
// period key formats they are claimed under — the telemetry ones', so the two
// read alike.
const (
	ClaimHourly  = "latency_hourly"
	ClaimDaily   = "latency_daily"
	ClaimMonthly = "latency_monthly"
	// ClaimBackfill is claimed once per deployment, under BackfillKey, by the
	// instance that backfills the tasks closed before latency was recorded.
	ClaimBackfill = "latency_backfill"
	BackfillKey   = "v1"

	HourKeyFormat = "2006-01-02T15"
	DayKeyFormat  = "2006-01-02"
)

// ClaimType is the claim type of the rollup holding buckets of g.
func ClaimType(g Granularity) string {
	switch g {
	case Hour:
		return ClaimHourly
	case Day:
		return ClaimDaily
	}
	return ClaimMonthly
}

// TailStart is where the rollups of g stop, given the newest claim of its
// type: everything from there on has to be read from the task rows. An hourly
// or daily claim names the period it rolled up; a monthly one names the day
// it ran, whose midnight its sum stops at. 0 means no rollup can be trusted.
func TailStart(g Granularity, latestKey string) int64 {
	switch g {
	case Hour:
		t, err := time.Parse(HourKeyFormat, latestKey)
		if err != nil {
			return 0
		}
		return t.Add(time.Hour).Unix()
	case Day:
		t, err := time.Parse(DayKeyFormat, latestKey)
		if err != nil {
			return 0
		}
		return t.AddDate(0, 0, 1).Unix()
	}
	t, err := time.Parse(DayKeyFormat, latestKey)
	if err != nil {
		return 0
	}
	return t.Unix()
}
