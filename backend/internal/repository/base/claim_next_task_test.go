package base

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newTaskRepo(t *testing.T) (Repository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.AutoMigrate(&model.Task{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return New(&mockDB{db: db}), db
}

func TestClaimNextTask_MarksOngoing(t *testing.T) {
	repo, db := newTaskRepo(t)
	ctx := context.Background()
	ws, uid := int64(100), int64(1)
	db.Create(&model.Task{ID: 10, WorkspaceID: ws, UserID: uid, Status: "notstarted", Assignee: "agent", CreatedAt: time.Now()})

	got, err := repo.ClaimNextTask(ctx, ws, uid)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if got.ID != 10 {
		t.Fatalf("claimed id = %d, want 10", got.ID)
	}
	if got.Status != "ongoing" {
		t.Fatalf("returned status = %q, want ongoing", got.Status)
	}
	var reloaded model.Task
	db.First(&reloaded, 10)
	if reloaded.Status != "ongoing" {
		t.Fatalf("persisted status = %q, want ongoing", reloaded.Status)
	}
}

// A claimed task must not be handed out again; the queue drains exactly once and in
// priority order (SortOrder asc, then CreatedAt, then id).
func TestClaimNextTask_DrainsQueueInOrder(t *testing.T) {
	repo, db := newTaskRepo(t)
	ctx := context.Background()
	ws, uid := int64(100), int64(1)
	now := time.Now()
	db.Create(&model.Task{ID: 10, WorkspaceID: ws, UserID: uid, Status: "notstarted", Assignee: "agent", SortOrder: 0, CreatedAt: now}) // fallback -> epoch(now), large
	db.Create(&model.Task{ID: 11, WorkspaceID: ws, UserID: uid, Status: "notstarted", Assignee: "agent", SortOrder: 5, CreatedAt: now})
	db.Create(&model.Task{ID: 12, WorkspaceID: ws, UserID: uid, Status: "notstarted", Assignee: "agent", SortOrder: 10, CreatedAt: now})

	wantOrder := []int64{11, 12, 10} // 5 < 10 < epoch(now)
	for i, want := range wantOrder {
		got, err := repo.ClaimNextTask(ctx, ws, uid)
		if err != nil {
			t.Fatalf("claim %d: %v", i, err)
		}
		if got.ID != want {
			t.Fatalf("claim %d = %d, want %d", i, got.ID, want)
		}
	}
	if _, err := repo.ClaimNextTask(ctx, ws, uid); err != ErrNotFound {
		t.Fatalf("empty queue: got %v, want ErrNotFound", err)
	}
}

func TestClaimNextTask_RespectsFilters(t *testing.T) {
	repo, db := newTaskRepo(t)
	ctx := context.Background()
	ws, uid := int64(100), int64(1)
	db.Create(&model.Task{ID: 1, WorkspaceID: ws, UserID: uid, Status: "ongoing", Assignee: "agent"})     // wrong status
	db.Create(&model.Task{ID: 2, WorkspaceID: ws, UserID: uid, Status: "notstarted", Assignee: "human"})  // wrong assignee
	db.Create(&model.Task{ID: 3, WorkspaceID: 999, UserID: uid, Status: "notstarted", Assignee: "agent"}) // wrong workspace
	db.Create(&model.Task{ID: 4, WorkspaceID: ws, UserID: 999, Status: "notstarted", Assignee: "agent"})  // wrong user

	if _, err := repo.ClaimNextTask(ctx, ws, uid); err != ErrNotFound {
		t.Fatalf("got %v, want ErrNotFound (no eligible task)", err)
	}
}

// The core regression: many concurrent claimers must never receive the same task.
func TestClaimNextTask_ConcurrentNoDoubleClaim(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "claim.db") + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close() // release the file handle so TempDir cleanup can remove it on Windows
	if err := db.AutoMigrate(&model.Task{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := New(&mockDB{db: db})
	ctx := context.Background()
	ws, uid := int64(100), int64(1)

	const tasks = 25
	for i := 0; i < tasks; i++ {
		db.Create(&model.Task{ID: int64(1000 + i), WorkspaceID: ws, UserID: uid, Status: "notstarted", Assignee: "agent", SortOrder: float64(i + 1)})
	}

	const goroutines = 40
	var wg sync.WaitGroup
	var mu sync.Mutex
	claimed := map[int64]int{}
	notFound := 0
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := repo.ClaimNextTask(ctx, ws, uid)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == ErrNotFound:
				notFound++
			case err != nil:
				t.Errorf("unexpected claim error: %v", err)
			default:
				claimed[got.ID]++
			}
		}()
	}
	wg.Wait()

	if len(claimed) != tasks {
		t.Errorf("distinct tasks claimed = %d, want %d", len(claimed), tasks)
	}
	for id, c := range claimed {
		if c != 1 {
			t.Errorf("task %d was claimed %d times (must be exactly once)", id, c)
		}
	}
	if notFound != goroutines-tasks {
		t.Errorf("ErrNotFound count = %d, want %d", notFound, goroutines-tasks)
	}
	var stillPending int64
	db.Model(&model.Task{}).Where("status = ?", "notstarted").Count(&stillPending)
	if stillPending != 0 {
		t.Errorf("%d tasks left notstarted, want 0", stillPending)
	}
}
