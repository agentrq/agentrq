package memq

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

type (
	Service interface {
		Create(ctx context.Context, req CreateRequest) (*CreateResponse, error)
		AddTask(ctx context.Context, req AddTaskRequest) error
		AddWorkers(ctx context.Context, req AddWorkersRequest) error
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
	}

	Params struct{}

	Task struct {
		ID  int64
		Val any
	}

	queue struct {
		name  string
		stats stats
		items chan Task
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

	select {
	case q.items <- req.Task:
		_ = atomic.AddInt32(&q.stats.enqueuedJobs, 1)
		return nil
	default:
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
		atomic.AddInt32(&q.stats.processingJobs, -1)
		atomic.AddInt32(&q.stats.processedJobs, 1)

		if r := recover(); r != nil {
			log.Printf("[memq] worker panic recovered: %v, task ID: %d, queue: %s", r, t.ID, q.name)
			atomic.AddInt32(&q.stats.failedJobs, 1)
		} else if err != nil {
			log.Printf("[memq] processing task failed: %v, task ID: %d, queue: %s", err, t.ID, q.name)
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
				log.Printf("[memq] stats - id: %d, name: %s, enqueued: %d, processed: %d, failed: %d, workers: %d",
					key.(uint32), q.name, atomic.LoadInt32(&q.stats.enqueuedJobs),
					atomic.LoadInt32(&q.stats.processedJobs), atomic.LoadInt32(&q.stats.failedJobs),
					atomic.LoadInt32(&q.stats.workers))
				return true
			})
		case <-s.done:
			return
		}
	}
}
