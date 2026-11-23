// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package tasklatency

import (
	"testing"
	"time"
)

func statsOf(values ...int64) Stats {
	var s Stats
	for _, v := range values {
		s.Add(v)
	}
	return s
}

func TestStats_AddAndAggregates(t *testing.T) {
	s := statsOf(600, 30, 7200, -5)
	if s.Count != 4 || s.Sum != 7830 || s.Min != 0 || s.Max != 7200 {
		t.Fatalf("stats = %+v", s)
	}
	if v, _ := s.Value(AggregateMin); v != 0 {
		t.Errorf("min = %d", v)
	}
	if v, _ := s.Value(AggregateMax); v != 7200 {
		t.Errorf("max = %d", v)
	}
	// A negative duration (clock skew) counts as zero, in the first bucket.
	if s.Hist[0] != 2 || s.Hist[bucketOf(600)] != 1 || s.Hist[bucketOf(7200)] != 1 {
		t.Errorf("hist = %v", s.Hist)
	}
}

func TestStats_EmptyIsAGap(t *testing.T) {
	for _, a := range []string{AggregateP50, AggregateMin, AggregateMax} {
		if _, ok := (Stats{}).Value(a); ok {
			t.Errorf("%s of nothing must be a gap", a)
		}
	}
}

func TestStats_P50(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []int64
		want   int64
	}{
		// One task: clamped to [min, max], so exact.
		{"single", []int64{437}, 437},
		// Both in [300, 600): the median rank is half way through the bucket.
		{"interpolated", []int64{310, 590}, 450},
		// The median falls in the open bucket past 30 days, which max bounds:
		// half way from 30 to 50 days.
		{"open bucket", []int64{40 * 86400, 50 * 86400}, 40 * 86400},
		// Clamped up to min when the bucket's lower edge is below it.
		{"clamped to min", []int64{3000, 3000, 3000}, 3000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if v, ok := statsOf(tc.values...).Value(AggregateP50); !ok || v != tc.want {
				t.Errorf("p50 = %d, %v; want %d", v, ok, tc.want)
			}
		})
	}
}

// A histogram that says less than Count (a corrupted row) still answers.
func TestStats_P50InconsistentHistogram(t *testing.T) {
	s := Stats{Count: 4, Min: 1, Max: 99, Hist: make([]int64, Buckets)}
	if v, _ := s.Value(AggregateP50); v != 99 {
		t.Errorf("p50 = %d, want max", v)
	}
}

func TestStats_MergeMatchesAddingEverything(t *testing.T) {
	a, b := statsOf(10, 900), statsOf(5, 86400*3)
	var merged Stats
	merged.Merge(a)
	merged.Merge(Stats{}) // an empty one changes nothing
	merged.Merge(b)
	all := statsOf(10, 900, 5, 86400*3)
	if merged.Count != all.Count || merged.Sum != all.Sum || merged.Min != all.Min || merged.Max != all.Max {
		t.Fatalf("merged = %+v, want %+v", merged, all)
	}
	for i := range all.Hist {
		if merged.Hist[i] != all.Hist[i] {
			t.Fatalf("hist = %v, want %v", merged.Hist, all.Hist)
		}
	}
	// Merging a lower max and higher min leaves them alone.
	merged.Merge(statsOf(700))
	if merged.Min != 5 || merged.Max != 86400*3 {
		t.Errorf("min, max = %d, %d", merged.Min, merged.Max)
	}
}

func TestHistEncoding(t *testing.T) {
	s := statsOf(1, 61, 61, 99*86400)
	enc := EncodeHist(s.Hist)
	if enc != "1,2,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,1" {
		t.Fatalf("encoded = %q", enc)
	}
	got := Decode(4, 5, 1, 99*86400, enc)
	if got.Count != 4 || got.Sum != 5 || got.Hist[1] != 2 || got.Hist[Buckets-1] != 1 {
		t.Fatalf("decoded = %+v", got)
	}
	// Short, long and malformed input all read without failing.
	if h := EncodeHist([]int64{3}); h != "3,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0" {
		t.Errorf("short = %q", h)
	}
	long := DecodeHist(enc + ",7,8")
	if len(long) != Buckets {
		t.Errorf("long = %v", long)
	}
	if bad := DecodeHist("x,2"); bad[0] != 0 || bad[1] != 2 {
		t.Errorf("malformed = %v", bad)
	}
}

func TestValidAggregate(t *testing.T) {
	for _, a := range []string{"p50", "min", "max"} {
		if !ValidAggregate(a) {
			t.Errorf("%s should be valid", a)
		}
	}
	for _, a := range []string{"", "avg", "p99"} {
		if ValidAggregate(a) {
			t.Errorf("%q should be refused", a)
		}
	}
}

func TestGranularityFor(t *testing.T) {
	for _, tc := range []struct {
		days int64
		want Granularity
	}{{1, Hour}, {2, Hour}, {7, Day}, {92, Day}, {93, Month}, {365, Month}} {
		if g := GranularityFor(0, tc.days*86400); g != tc.want {
			t.Errorf("%d days: %s, want %s", tc.days, g, tc.want)
		}
	}
}

func TestFloorNextPeriods(t *testing.T) {
	at := time.Date(2026, 1, 31, 13, 45, 10, 0, time.UTC).Unix()
	unix := func(y int, m time.Month, d, h int) int64 { return time.Date(y, m, d, h, 0, 0, 0, time.UTC).Unix() }

	if got := Floor(at, Hour); got != unix(2026, 1, 31, 13) {
		t.Errorf("hour floor = %d", got)
	}
	if got := Floor(at, Day); got != unix(2026, 1, 31, 0) {
		t.Errorf("day floor = %d", got)
	}
	if got := Floor(at, Month); got != unix(2026, 1, 1, 0) {
		t.Errorf("month floor = %d", got)
	}
	if got := Next(unix(2026, 1, 31, 23), Hour); got != unix(2026, 2, 1, 0) {
		t.Errorf("next hour = %d", got)
	}
	if got := Next(unix(2026, 1, 31, 0), Day); got != unix(2026, 2, 1, 0) {
		t.Errorf("next day = %d", got)
	}
	if got := Next(unix(2026, 1, 1, 0), Month); got != unix(2026, 2, 1, 0) {
		t.Errorf("next month = %d", got)
	}

	ps := Periods(at, unix(2026, 2, 2, 5), Day)
	if len(ps) != 3 || ps[0] != unix(2026, 1, 31, 0) || ps[2] != unix(2026, 2, 2, 0) {
		t.Errorf("periods = %v", ps)
	}
}

func TestClaimTypeAndTailStart(t *testing.T) {
	if ClaimType(Hour) != ClaimHourly || ClaimType(Day) != ClaimDaily || ClaimType(Month) != ClaimMonthly {
		t.Fatal("claim types")
	}
	unix := func(y int, m time.Month, d, h int) int64 { return time.Date(y, m, d, h, 0, 0, 0, time.UTC).Unix() }
	for _, tc := range []struct {
		g    Granularity
		key  string
		want int64
	}{
		{Hour, "2026-09-27T14", unix(2026, 9, 27, 15)},
		{Day, "2026-09-26", unix(2026, 9, 27, 0)},
		{Month, "2026-09-27", unix(2026, 9, 27, 0)},
		{Hour, "junk", 0},
		{Day, "junk", 0},
		{Month, "junk", 0},
	} {
		if got := TailStart(tc.g, tc.key); got != tc.want {
			t.Errorf("TailStart(%s, %q) = %d, want %d", tc.g, tc.key, got, tc.want)
		}
	}
}
