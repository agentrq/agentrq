// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

// Package metrics reports what the machine has left.
//
// Enough to answer "can this box take another agent?" without opening a
// terminal on it. Everything here is read through function fields rather than
// called directly, so the rules — which mounts count, what an absent load
// average means — can be tested without a particular machine under them.
package metrics

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"

	"github.com/agentrq/agentrq/daemon/wire"
)

// Interval is how often the machine is measured.
//
// The CPU figure is a rate and needs two samples separated by real time, so
// this is also the window it is measured over. Long enough that the
// measurement itself is not the load, short enough that the number means
// something by the time it is read.
const Interval = 15 * time.Second

// Partition is a mounted filesystem, as the collector sees it.
type Partition struct {
	Mount  string
	FSType string
	Device string
}

// skipFS are the filesystems that are not storage anyone can fill.
//
// Kept as a list of what to exclude rather than a list of what to include:
// a new real filesystem should show up by default, and a new pseudo one
// showing up is a cosmetic problem rather than a missing disk.
var skipFS = map[string]bool{
	"autofs": true, "binfmt_misc": true, "bpf": true, "cgroup": true,
	"cgroup2": true, "configfs": true, "debugfs": true, "devpts": true,
	"devtmpfs": true, "efivarfs": true, "fuse.gvfsd-fuse": true,
	"fuse.portal": true, "fusectl": true, "hugetlbfs": true, "mqueue": true,
	"nsfs": true, "overlay": true, "proc": true, "pstore": true,
	"ramfs": true, "securityfs": true, "squashfs": true, "sysfs": true,
	"tmpfs": true, "tracefs": true,
}

// Collector measures the machine on a timer and remembers the last answer.
//
// The heartbeat reads that remembered answer and never waits for a
// measurement. A heartbeat that blocked on a disk that had gone away would
// stop being a heartbeat at exactly the moment it was most informative.
type Collector struct {
	// Paths names directories whose filesystems matter — the workspace
	// folders agents are running in. The mount holding a workspace is the one
	// that fills up, and it is not always the root filesystem.
	Paths func() []string

	// The sources. Replaced wholesale in tests; defaulted to gopsutil.
	Memory     func() (total, available int64, err error)
	CPU        func(time.Duration) (float64, error)
	Load       func() ([]float64, error)
	Uptime     func() (int64, error)
	Partitions func() ([]Partition, error)
	Usage      func(mount string) (total, free int64, err error)

	// Now and Interval exist so the loop can be driven in a test.
	Interval time.Duration

	mu      sync.Mutex
	last    wire.Heartbeat
	haveOne bool
}

// errLoadUnsupported is what a platform with no load average reports.
var errLoadUnsupported = errors.New("metrics: this platform has no load average")

// New builds a collector reading the real machine.
func New(paths func() []string) *Collector {
	return &Collector{
		Paths:      paths,
		Memory:     realMemory,
		CPU:        realCPU,
		Load:       realLoad,
		Uptime:     realUptime,
		Partitions: realPartitions,
		Usage:      realUsage,
		Interval:   Interval,
	}
}

// FirstInterval is the window the first measurement is taken over.
//
// Short, because nothing is reported until there has been one: a machine that
// has just connected says something useful within a second or two rather than
// showing nothing for a quarter of a minute. The figure is noisier over a
// second than over fifteen, and a slightly noisy number now is worth more than
// an accurate one later to somebody deciding whether to launch an agent.
const FirstInterval = time.Second

// Run measures until the context ends.
func (c *Collector) Run(ctx context.Context) {
	window := c.first()
	for {
		// The measurement is the loop: the CPU call blocks for the window, so
		// there is nothing to sleep for afterwards.
		c.measure(ctx, window)
		if ctx.Err() != nil {
			return
		}
		window = c.Interval
	}
}

func (c *Collector) first() time.Duration {
	if c.Interval < FirstInterval {
		return c.Interval
	}
	return FirstInterval
}

// Snapshot is the last measurement, and whether there has been one.
func (c *Collector) Snapshot() (wire.Heartbeat, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last, c.haveOne
}

func (c *Collector) measure(ctx context.Context, window time.Duration) {
	var hb wire.Heartbeat

	// The CPU call blocks for the window: that window *is* the measurement.
	// Everything else is a cheap read taken at the end of it.
	if pct, err := c.CPU(window); err == nil {
		hb.CPUPercent = pct
	}
	if ctx.Err() != nil {
		return
	}

	if total, available, err := c.Memory(); err == nil {
		hb.MemTotal, hb.MemAvailable = total, available
	}
	// An error here is Windows, where there is no load average. Left nil, not
	// zeroed: three zeroes render as a perfectly idle machine.
	if avg, err := c.Load(); err == nil {
		hb.LoadAvg = avg
	}
	if up, err := c.Uptime(); err == nil {
		hb.UptimeSec = up
	}
	hb.Disks = c.disks()

	c.mu.Lock()
	c.last = hb
	c.haveOne = true
	c.mu.Unlock()
}

// disks reports the filesystems worth knowing about.
func (c *Collector) disks() []wire.Disk {
	parts, err := c.Partitions()
	if err != nil {
		return nil
	}

	wanted := map[string]bool{}
	for _, p := range parts {
		if skipFS[p.FSType] {
			continue
		}
		wanted[p.Mount] = true
	}

	// The mount each workspace sits on, whether or not it survived the filter.
	// A workspace on an overlay or a network mount still fills up, and the
	// person whose checkout failed does not care what kind of filesystem it is.
	if c.Paths != nil {
		for _, path := range c.Paths() {
			if m := mountFor(path, parts); m != "" {
				wanted[m] = true
			}
		}
	}

	out := make([]wire.Disk, 0, len(wanted))
	for mount := range wanted {
		total, free, err := c.Usage(mount)
		if err != nil || total == 0 {
			// A mount that cannot be measured is left out rather than
			// reported as zero bytes, which reads as a full disk.
			continue
		}
		out = append(out, wire.Disk{Mount: mount, Total: total, Free: free})
	}
	sortDisks(out)
	return out
}

// mountFor finds the longest mount point that contains a path.
//
// Longest, because "/" contains everything: the answer for /srv/app on a
// machine with /srv mounted separately is /srv, and picking the first match
// would report the wrong filesystem's free space.
func mountFor(path string, parts []Partition) string {
	path = filepath.Clean(path)
	best := ""
	for _, p := range parts {
		m := filepath.Clean(p.Mount)
		if !under(path, m) {
			continue
		}
		if len(m) > len(best) {
			best = m
		}
	}
	return best
}

func under(path, mount string) bool {
	if path == mount {
		return true
	}
	if mount == string(filepath.Separator) {
		return true
	}
	sep := string(filepath.Separator)
	// The separator matters: /srv must not match /srvfoo.
	return len(path) > len(mount) && path[:len(mount)] == mount && path[len(mount):len(mount)+1] == sep
}

func sortDisks(d []wire.Disk) {
	// Insertion sort: the list is a handful of entries, and a stable, obvious
	// order is all this needs. Sorted at all because the map above is not, and
	// an unsorted list reshuffles the UI on every heartbeat.
	for i := 1; i < len(d); i++ {
		for j := i; j > 0 && d[j].Mount < d[j-1].Mount; j-- {
			d[j], d[j-1] = d[j-1], d[j]
		}
	}
}

// ── the real machine ─────────────────────────────────────────────────────────

func realMemory() (int64, int64, error) {
	v, err := mem.VirtualMemory()
	if err != nil {
		return 0, 0, err
	}
	// Available, not free. On Linux "free" excludes the page cache and reads
	// alarmingly low on a healthy machine; what a person wants to know is what
	// a new process could actually get.
	return int64(v.Total), int64(v.Available), nil
}

func realCPU(window time.Duration) (float64, error) {
	pct, err := cpu.Percent(window, false)
	if err != nil {
		return 0, err
	}
	if len(pct) == 0 {
		return 0, nil
	}
	return pct[0], nil
}

func realLoad() ([]float64, error) {
	if runtime.GOOS == "windows" {
		// gopsutil will synthesise one from a performance counter. That number
		// is not a Unix load average and reporting it as one would be a lie
		// with three decimal places on it.
		return nil, errLoadUnsupported
	}
	avg, err := load.Avg()
	if err != nil {
		return nil, err
	}
	return []float64{avg.Load1, avg.Load5, avg.Load15}, nil
}

func realUptime() (int64, error) {
	up, err := host.Uptime()
	return int64(up), err
}

func realPartitions() ([]Partition, error) {
	// all=false asks for real filesystems; the skip list above catches what
	// slips through, which on Linux is mostly snap's squashfs mounts.
	ps, err := disk.Partitions(false)
	if err != nil {
		return nil, err
	}
	out := make([]Partition, 0, len(ps))
	for _, p := range ps {
		out = append(out, Partition{Mount: p.Mountpoint, FSType: p.Fstype, Device: p.Device})
	}
	return out, nil
}

func realUsage(mount string) (int64, int64, error) {
	u, err := disk.Usage(mount)
	if err != nil {
		return 0, 0, err
	}
	return int64(u.Total), int64(u.Free), nil
}
