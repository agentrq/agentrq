// Copyright 2026 Contextual, Inc. https://agentrq.com

package ratelimit

import (
	"sync"
	"time"
)

type Limiter interface {
	AllowWorkspace(userID int64) bool
	AllowTask(userID int64) bool
	AllowMessage(userID int64) bool
	AllowTelemetry(userID int64) bool
}

type rotatingBucket struct {
	LastUpdate int64
	Buckets    [60]uint8
}

type limiter struct {
	sync.Mutex
	workspaceRequests map[int64]*rotatingBucket
	taskRequests      map[int64]*rotatingBucket
	messageRequests   map[int64]*rotatingBucket
	telemetryRequests map[int64]*rotatingBucket
}

func New() Limiter {
	return &limiter{
		workspaceRequests: make(map[int64]*rotatingBucket),
		taskRequests:      make(map[int64]*rotatingBucket),
		messageRequests:   make(map[int64]*rotatingBucket),
		telemetryRequests: make(map[int64]*rotatingBucket),
	}
}

func (l *limiter) AllowWorkspace(userID int64) bool {
	l.Lock()
	defer l.Unlock()

	now := time.Now().Unix()
	rb, ok := l.workspaceRequests[userID]
	if !ok {
		rb = &rotatingBucket{}
		l.workspaceRequests[userID] = rb
	}

	return allow(rb, now, 1, 2)
}

func (l *limiter) AllowTask(userID int64) bool {
	l.Lock()
	defer l.Unlock()

	now := time.Now().Unix()
	rb, ok := l.taskRequests[userID]
	if !ok {
		rb = &rotatingBucket{}
		l.taskRequests[userID] = rb
	}

	return allow(rb, now, 1, 10)
}

func (l *limiter) AllowMessage(userID int64) bool {
	l.Lock()
	defer l.Unlock()

	now := time.Now().Unix()
	rb, ok := l.messageRequests[userID]
	if !ok {
		rb = &rotatingBucket{}
		l.messageRequests[userID] = rb
	}

	return allow(rb, now, 5, 60)
}

// AllowTelemetry caps client-reported telemetry at 60 per rolling minute.
//
// Unlike the other buckets this guards a route whose whole job is to be called
// from the browser on a click, so the ceiling is what stops a page — buggy or
// hostile — from inflating the counters it feeds.
//
// It was 2/second and 10/minute when the only client-reported actions were two
// rare local-AI events. Interface usage is a different traffic profile
// altogether: shortcuts, searches and copies are ordinary interactions, and an
// engaged user passes ten a minute without trying. Left at ten, the new counts
// would be silently short *and* would starve the local-AI metrics that share
// this bucket — a change that quietly degrades a metric which works today.
//
// Sixty a minute is one a second sustained, which no honest interface exceeds
// and which still bounds a hostile page to sixty small rows per user per
// minute. The per-second burst of five covers a genuine flurry — a shortcut
// that switches view and is counted as both — without letting a loop run away.
func (l *limiter) AllowTelemetry(userID int64) bool {
	l.Lock()
	defer l.Unlock()

	now := time.Now().Unix()
	rb, ok := l.telemetryRequests[userID]
	if !ok {
		rb = &rotatingBucket{}
		l.telemetryRequests[userID] = rb
	}

	return allow(rb, now, 5, 60)
}

func allow(rb *rotatingBucket, now int64, maxSec int, maxMin int) bool {
	// Calculate difference and clear stale buckets
	diff := now - rb.LastUpdate
	if diff >= 60 {
		rb.Buckets = [60]uint8{}
	} else if diff > 0 {
		for t := rb.LastUpdate + 1; t <= now; t++ {
			rb.Buckets[t%60] = 0
		}
	}

	// Enforce second-level limit
	if rb.Buckets[now%60] >= uint8(maxSec) {
		return false
	}

	// Enforce minute-level limit
	var sum int
	for _, count := range rb.Buckets {
		sum += int(count)
	}
	if sum >= maxMin {
		return false
	}

	// Record request
	rb.Buckets[now%60]++
	rb.LastUpdate = now
	return true
}
