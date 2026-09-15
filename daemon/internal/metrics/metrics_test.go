// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package metrics

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// fake builds a collector reading nothing real.
func fake() *Collector {
	return &Collector{
		Interval:   time.Millisecond,
		Memory:     func() (int64, int64, error) { return 16 << 30, 4 << 30, nil },
		CPU:        func(time.Duration) (float64, error) { return 37.4, nil },
		Load:       func() ([]float64, error) { return []float64{1.2, 0.9, 0.7}, nil },
		Uptime:     func() (int64, error) { return 918273, nil },
		Partitions: func() ([]Partition, error) { return nil, nil },
		Usage:      func(string) (int64, int64, error) { return 0, 0, errors.New("no such mount") },
	}
}

func measureOnce(t *testing.T, c *Collector) {
	t.Helper()
	c.measure(context.Background(), time.Millisecond)
	if _, ok := c.Snapshot(); !ok {
		t.Fatal("no measurement was recorded")
	}
}

func TestItReportsWhatTheMachineHasLeft(t *testing.T) {
	c := fake()
	measureOnce(t, c)

	hb, _ := c.Snapshot()
	if hb.MemTotal != 16<<30 || hb.MemAvailable != 4<<30 {
		t.Errorf("memory = %d/%d", hb.MemAvailable, hb.MemTotal)
	}
	if hb.CPUPercent != 37.4 {
		t.Errorf("cpu = %v", hb.CPUPercent)
	}
	if hb.UptimeSec != 918273 {
		t.Errorf("uptime = %d", hb.UptimeSec)
	}
	if len(hb.LoadAvg) != 3 {
		t.Errorf("loadAvg = %v", hb.LoadAvg)
	}
}

// Three zeroes render as a perfectly idle machine, which is the most
// misleading thing this payload could say about a box that has no load average
// at all.
func TestAPlatformWithNoLoadAverageReportsNone(t *testing.T) {
	c := fake()
	c.Load = func() ([]float64, error) { return nil, errors.New("not on this platform") }
	measureOnce(t, c)

	if hb, _ := c.Snapshot(); hb.LoadAvg != nil {
		t.Errorf("loadAvg = %v, want nothing at all", hb.LoadAvg)
	}
}

// One source failing must not cost the rest of the measurement: a machine
// whose disks cannot be read still has memory worth reporting.
func TestOneBrokenSourceDoesNotLoseTheOthers(t *testing.T) {
	c := fake()
	c.Memory = func() (int64, int64, error) { return 0, 0, errors.New("no") }
	c.Partitions = func() ([]Partition, error) { return nil, errors.New("no") }
	measureOnce(t, c)

	hb, _ := c.Snapshot()
	if hb.CPUPercent != 37.4 || hb.UptimeSec != 918273 {
		t.Errorf("a broken source took the rest with it: %+v", hb)
	}
}

func TestNothingIsReportedBeforeTheFirstMeasurement(t *testing.T) {
	if _, ok := fake().Snapshot(); ok {
		t.Error("a collector that has measured nothing claimed a measurement")
	}
}

// A single free-space figure is a lie on any machine with more than one
// filesystem, so the pseudo-filesystems are filtered and the real ones are not.
func TestOnlyRealFilesystemsAreReported(t *testing.T) {
	c := fake()
	c.Partitions = func() ([]Partition, error) {
		return []Partition{
			{Mount: "/", FSType: "ext4"},
			{Mount: "/srv", FSType: "xfs"},
			{Mount: "/run", FSType: "tmpfs"},
			{Mount: "/dev", FSType: "devtmpfs"},
			{Mount: "/snap/core/1", FSType: "squashfs"},
			{Mount: "/var/lib/docker/overlay2/x/merged", FSType: "overlay"},
		}, nil
	}
	c.Usage = func(string) (int64, int64, error) { return 100, 40, nil }
	measureOnce(t, c)

	hb, _ := c.Snapshot()
	// Sorted, because the map they come from is not and an unsorted list
	// reshuffles the UI on every heartbeat.
	if len(hb.Disks) != 2 || hb.Disks[0].Mount != "/" || hb.Disks[1].Mount != "/srv" {
		t.Fatalf("disks = %+v", hb.Disks)
	}
	if hb.Disks[0].Total != 100 || hb.Disks[0].Free != 40 {
		t.Errorf("usage = %+v", hb.Disks[0])
	}
}

// The mount a workspace sits on is the one that fills up, and it is reported
// whatever kind of filesystem it turns out to be — the person whose checkout
// failed does not care that it is an overlay.
func TestAWorkspaceMountIsReportedEvenWhenItsKindIsFiltered(t *testing.T) {
	c := fake()
	c.Paths = func() []string { return []string{"/var/lib/docker/overlay2/x/merged/app"} }
	c.Partitions = func() ([]Partition, error) {
		return []Partition{
			{Mount: "/", FSType: "ext4"},
			{Mount: "/var/lib/docker/overlay2/x/merged", FSType: "overlay"},
		}, nil
	}
	c.Usage = func(string) (int64, int64, error) { return 100, 40, nil }
	measureOnce(t, c)

	hb, _ := c.Snapshot()
	if len(hb.Disks) != 2 {
		t.Fatalf("disks = %+v", hb.Disks)
	}
}

// A mount that cannot be measured is left out rather than reported as zero
// bytes, which reads as a disk that is completely full.
func TestAnUnmeasurableMountIsOmittedRatherThanReportedEmpty(t *testing.T) {
	c := fake()
	c.Partitions = func() ([]Partition, error) {
		return []Partition{{Mount: "/", FSType: "ext4"}, {Mount: "/mnt/gone", FSType: "nfs"}}, nil
	}
	c.Usage = func(mount string) (int64, int64, error) {
		if mount == "/mnt/gone" {
			return 0, 0, errors.New("stale file handle")
		}
		return 100, 40, nil
	}
	measureOnce(t, c)

	hb, _ := c.Snapshot()
	if len(hb.Disks) != 1 || hb.Disks[0].Mount != "/" {
		t.Errorf("disks = %+v", hb.Disks)
	}
}

// "/" contains everything, so the longest match is the only correct one.
//
// Written through filepath.FromSlash because this is about path *separators*,
// and hard-coding "/" tests nothing on the one platform where the answer could
// differ. The first version of this test failed on Windows for exactly that
// reason — the function was right and the expectations were Unix.
func TestTheDeepestMountWins(t *testing.T) {
	sep := filepath.FromSlash
	parts := []Partition{
		{Mount: sep("/")},
		{Mount: sep("/srv")},
		{Mount: sep("/srv/data")},
		{Mount: sep("/srvfoo")},
	}
	for path, want := range map[string]string{
		"/srv/data/app":  "/srv/data",
		"/srv/app":       "/srv",
		"/srvfoo/app":    "/srvfoo", // not /srv: the separator matters
		"/home/rpi/code": "/",
		"/srv":           "/srv",
	} {
		if got := mountFor(sep(path), parts); got != sep(want) {
			t.Errorf("mountFor(%q) = %q, want %q", sep(path), got, sep(want))
		}
	}
}

// Windows mounts are drive letters, which is what gopsutil actually reports
// there — so that is what the matching has to work on, not a Unix path with
// the separators swapped.
//
// This found a real bug rather than a test one: a drive root already ends in a
// separator, and the matching required another one after the mount. Every
// Windows machine would have reported no disks at all.
func TestDriveLettersAreMounts(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("drive letters are a Windows shape")
	}
	parts := []Partition{{Mount: `C:\`}, {Mount: `D:\`}, {Mount: `C:\data`}}
	for path, want := range map[string]string{
		`C:\Users\rpi\src`: `C:\`,
		`C:\data\app`:      `C:\data`,
		`D:\build`:         `D:\`,
	} {
		if got := mountFor(path, parts); got != filepath.Clean(want) {
			t.Errorf("mountFor(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestMountForWithNothingMounted(t *testing.T) {
	if got := mountFor(filepath.FromSlash("/srv/app"), nil); got != "" {
		t.Errorf("mountFor with no partitions = %q", got)
	}
}

// The loop measures immediately rather than sleeping first, so there is an
// answer before the first heartbeat rather than a blank one.
func TestRunMeasuresBeforeItWaits(t *testing.T) {
	c := fake()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := c.Snapshot(); ok {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the loop never produced a measurement")
}

// A cancelled context stops the loop rather than leaving a goroutine measuring
// a machine nobody is reporting on.
func TestRunStops(t *testing.T) {
	c := fake()
	c.CPU = func(d time.Duration) (float64, error) { time.Sleep(d); return 1, nil }
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the loop kept running after its context ended")
	}
}

// The real sources are exercised once, because a wrapper that compiles and
// returns nothing useful is the easiest thing in this package to get wrong.
func TestTheRealMachineAnswers(t *testing.T) {
	total, available, err := realMemory()
	if err != nil {
		t.Skipf("no memory readings on this platform: %v", err)
	}
	if total <= 0 || available <= 0 || available > total {
		t.Errorf("memory = %d available of %d", available, total)
	}

	if up, err := realUptime(); err == nil && up <= 0 {
		t.Errorf("uptime = %d", up)
	}
	if _, err := realCPU(10 * time.Millisecond); err != nil {
		t.Errorf("cpu: %v", err)
	}

	parts, err := realPartitions()
	if err != nil {
		t.Skipf("no partition list on this platform: %v", err)
	}
	if len(parts) == 0 {
		t.Error("this machine reported no filesystems at all")
	}
	if _, _, err := realUsage(parts[0].Mount); err != nil {
		t.Errorf("usage of %q: %v", parts[0].Mount, err)
	}

	avg, err := realLoad()
	if err != nil {
		if !errors.Is(err, errLoadUnsupported) {
			t.Errorf("load: %v", err)
		}
	} else if len(avg) != 3 {
		t.Errorf("loadAvg = %v", avg)
	}
}
