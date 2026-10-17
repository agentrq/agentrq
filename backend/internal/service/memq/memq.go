// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package memq

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	zlog "github.com/rs/zerolog/log"
)

type (
	Service interface {
		Create(ctx context.Context, req CreateRequest) (*CreateResponse, error)
		AddTask(ctx context.Context, req AddTaskRequest) error
		AddWorkers(ctx context.Context, req AddWorkersRequest) error
		// Drain refuses new tasks, waits for the queued and running ones
		// to finish, then closes. It gives up when ctx is done.
		Drain(ctx context.Context) error
		Close()
	}

	CreateRequest struct {
		Name string
		Size uint32
	}

	CreateResponse struct {
		ID uint32
	}

	AddTaskRequest struct {
		QueueID uint32
		Task    Task
	}

	AddWorkersRequest struct {
		QueueID uint32
		Count   uint32
		Handle  func(ctx context.Context, t Task) error
	}

	service struct {
		next      uint32
		queues    sync.Map
		done      chan struct{}
		closeOnce sync.Once
		// closedMu makes closed final: once Drain holds it, no AddTask is
		// still between the check and the enqueue.
		closedMu sync.RWMutex
		closed   bool
	}

	Params struct{}

	Task struct {
		ID  int64
		Val any
	}

	queue struct {
		// pending counts tasks added and not yet processed, queued or
		// running, so no task is missed between the two.
		pending int32
		name    string
		stats   stats
		items   chan Task
	}

	stats struct {
		enqueuedJobs        int32
		failedToAddTaskJobs int32
		processingJobs      int32
		processedJobs       int32
		failedJobs          int32
		workers             int32
	}

	err string
)

const (
	ErrQueueNotFound      err = "queue not found"
	ErrQueueFull          err = "queue is full"
	ErrInvalidTaskHandler err = "invalid task handler"
	ErrQueueClosed        err = "queue is closed"
)

func (e err) Error() string {
	return string(e)
}

func New(p Params) (Service, error) {
	s := &service{
		queues: sync.Map{},
		next:   0,
		done:   make(chan struct{}),
	}
	go s.printStats()
	return s, nil
}

// drainPoll is how often Drain looks for remaining work.
const drainPoll = 10 * time.Millisecond

func (s *service) Drain(ctx context.Context) error {
	s.closedMu.Lock()
	s.closed = true
	s.closedMu.Unlock()

	ticker := time.NewTicker(drainPoll)
	defer ticker.Stop()
	for !s.idle() {
		select {
		case <-ctx.Done():
			s.Close()
			return ctx.Err()
		case <-ticker.C:
		}
	}
	s.Close()
	return nil
}

// idle reports whether every task added has been processed.
func (s *service) idle() bool {
	idle := true
	s.queues.Range(func(_, value any) bool {
		q := value.(*queue)
		idle = atomic.LoadInt32(&q.pending) == 0
		return idle
	})
	return idle
}

func (s *service) Close() {
	s.closeOnce.Do(func() {
		close(s.done)
	})
}

func (s *service) Create(ctx context.Context, req CreateRequest) (*CreateResponse, error) {
	id := atomic.AddUint32(&s.next, 1)
	q := queue{
		name:  req.Name,
		items: make(chan Task, int(req.Size)),
		stats: stats{},
	}
	s.queues.Store(id, &q)
	return &CreateResponse{
		ID: id,
	}, nil
}

func (s *service) AddTask(ctx context.Context, req AddTaskRequest) error {
	v, ok := s.queues.Load(req.QueueID)
	if !ok {
		return ErrQueueNotFound
	}

	q := v.(*queue)

	s.closedMu.RLock()
	defer s.closedMu.RUnlock()
	if s.closed {
		return ErrQueueClosed
	}
	atomic.AddInt32(&q.pending, 1)
	select {
	case q.items <- req.Task:
		_ = atomic.AddInt32(&q.stats.enqueuedJobs, 1)
		return nil
	default:
		atomic.AddInt32(&q.pending, -1)
		_ = atomic.AddInt32(&q.stats.failedToAddTaskJobs, 1)
		return ErrQueueFull
	}
}

func (s *service) AddWorkers(ctx context.Context, req AddWorkersRequest) error {
	v, ok := s.queues.Load(req.QueueID)
	if !ok {
		return ErrQueueNotFound
	}

	if req.Handle == nil {
		return ErrInvalidTaskHandler
	}

	q := v.(*queue)
	for i := 0; i < int(req.Count); i++ {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-s.done:
					return
				case t, ok := <-q.items:
					if !ok {
						return
					}
					q.processTaskWithRecovery(ctx, t, req.Handle)
				}
			}
		}()
	}

	_ = atomic.AddInt32(&q.stats.workers, int32(req.Count))

	return nil
}

func (q *queue) processTaskWithRecovery(ctx context.Context, t Task, handle func(context.Context, Task) error) {
	atomic.AddInt32(&q.stats.processingJobs, 1)
	var err error

	defer func() {
		defer atomic.AddInt32(&q.pending, -1)
		atomic.AddInt32(&q.stats.processingJobs, -1)
		atomic.AddInt32(&q.stats.processedJobs, 1)

		if r := recover(); r != nil {
			zlog.Error().Interface("panic", r).Int64("task_id", t.ID).Str("queue", q.name).Msg("[memq] worker panic recovered")
			atomic.AddInt32(&q.stats.failedJobs, 1)
		} else if err != nil {
			zlog.Error().Err(err).Int64("task_id", t.ID).Str("queue", q.name).Msg("[memq] processing task failed")
			atomic.AddInt32(&q.stats.failedJobs, 1)
		}
	}()

	err = handle(ctx, t)
}

func (s *service) printStats() {
	ticker := time.NewTicker(3 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.queues.Range(func(key, value any) bool {
				q := value.(*queue)
				zlog.Info().Uint32("id", key.(uint32)).Str("name", q.name).
					Int32("enqueued", atomic.LoadInt32(&q.stats.enqueuedJobs)).
					Int32("processed", atomic.LoadInt32(&q.stats.processedJobs)).
					Int32("failed", atomic.LoadInt32(&q.stats.failedJobs)).
					Int32("workers", atomic.LoadInt32(&q.stats.workers)).
					Msg("[memq] stats")
				return true
			})
		case <-s.done:
			return
		}
	}
}
