package memq

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestMemq(t *testing.T) {
	s, err := New(Params{})
	if err != nil {
		t.Fatalf("failed to create memq: %v", err)
	}
	defer s.Close()

	t.Run("CreateAndAddTask", func(t *testing.T) {
		res, err := s.Create(context.Background(), CreateRequest{Name: "test-q", Size: 10})
		if err != nil {
			t.Fatal(err)
		}

		err = s.AddTask(context.Background(), AddTaskRequest{
			QueueID: res.ID,
			Task:    Task{ID: 1, Val: "v1"},
		})
		if err != nil {
			t.Errorf("failed to add task: %v", err)
		}

		// Double check stats if possible (not exposed easily but we can check if it works)
	})

	t.Run("AddWorkersAndProcess", func(t *testing.T) {
		res, _ := s.Create(context.Background(), CreateRequest{Name: "worker-q", Size: 10})
		
		var processed int32
		done := make(chan bool)
		
		handle := func(ctx context.Context, t Task) error {
			atomic.AddInt32(&processed, 1)
			if atomic.LoadInt32(&processed) == 2 {
				done <- true
			}
			return nil
		}

		err = s.AddWorkers(context.Background(), AddWorkersRequest{
			QueueID: res.ID,
			Count:   2,
			Handle:  handle,
		})
		if err != nil {
			t.Fatal(err)
		}

		s.AddTask(context.Background(), AddTaskRequest{QueueID: res.ID, Task: Task{ID: 1}})
		s.AddTask(context.Background(), AddTaskRequest{QueueID: res.ID, Task: Task{ID: 2}})

		select {
		case <-done:
			// Success
		case <-time.After(1 * time.Second):
			t.Error("timeout waiting for tasks to process")
		}
	})

	t.Run("QueueNotFound", func(t *testing.T) {
		err := s.AddTask(context.Background(), AddTaskRequest{QueueID: 999})
		if err != ErrQueueNotFound {
			t.Errorf("expected ErrQueueNotFound, got %v", err)
		}
		
		err = s.AddWorkers(context.Background(), AddWorkersRequest{QueueID: 999})
		if err != ErrQueueNotFound {
			t.Errorf("expected ErrQueueNotFound, got %v", err)
		}
	})

	t.Run("QueueFull", func(t *testing.T) {
		res, _ := s.Create(context.Background(), CreateRequest{Name: "full-q", Size: 1})
		s.AddTask(context.Background(), AddTaskRequest{QueueID: res.ID, Task: Task{ID: 1}})
		err := s.AddTask(context.Background(), AddTaskRequest{QueueID: res.ID, Task: Task{ID: 2}})
		if err != ErrQueueFull {
			t.Errorf("expected ErrQueueFull, got %v", err)
		}
	})

	t.Run("InvalidHandler", func(t *testing.T) {
		res, _ := s.Create(context.Background(), CreateRequest{Name: "invalid-h", Size: 10})
		err := s.AddWorkers(context.Background(), AddWorkersRequest{QueueID: res.ID, Handle: nil})
		if err != ErrInvalidTaskHandler {
			t.Errorf("expected ErrInvalidTaskHandler, got %v", err)
		}
	})

	t.Run("WorkerPanicRecovery", func(t *testing.T) {
		res, _ := s.Create(context.Background(), CreateRequest{Name: "panic-q", Size: 10})
		
		var panics int32
		handle := func(ctx context.Context, t Task) error {
			atomic.AddInt32(&panics, 1)
			panic("boom")
		}
		
		s.AddWorkers(context.Background(), AddWorkersRequest{QueueID: res.ID, Count: 1, Handle: handle})
		s.AddTask(context.Background(), AddTaskRequest{QueueID: res.ID, Task: Task{ID: 1}})
		
		// Wait a bit for recovery
		time.Sleep(100 * time.Millisecond)
		if atomic.LoadInt32(&panics) != 1 {
			t.Error("expected 1 panic")
		}
	})
}

func TestErrError(t *testing.T) {
	var e err = "test-err"
	if e.Error() != "test-err" {
		t.Error("error string mismatch")
	}
}
