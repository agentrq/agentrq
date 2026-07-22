package base

import (
	"context"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type mockDB struct {
	db *gorm.DB
}

func (m *mockDB) Conn(ctx context.Context) *gorm.DB {
	return m.db
}

func (m *mockDB) Close(ctx context.Context) {}

func TestRepository_GetNextTask(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}

	_ = db.AutoMigrate(&model.Task{})
	repo := New(&mockDB{db: db})

	ctx := context.Background()
	workspaceID := int64(100)
	userID := int64(1)

	// Case 1: No tasks
	_, err = repo.GetNextTask(ctx, workspaceID, userID)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

	// Case 2: Tasks exist but none match filters
	db.Create(&model.Task{
		ID:          1,
		WorkspaceID: workspaceID,
		UserID:      userID,
		Status:      "ongoing", // wrong status
		Assignee:    "agent",
	})
	db.Create(&model.Task{
		ID:          2,
		WorkspaceID: workspaceID,
		UserID:      userID,
		Status:      "notstarted",
		Assignee:    "human", // wrong assignee
	})
	db.Create(&model.Task{
		ID:          3,
		WorkspaceID: 200, // wrong workspace
		UserID:      userID,
		Status:      "notstarted",
		Assignee:    "agent",
	})

	_, err = repo.GetNextTask(ctx, workspaceID, userID)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound for non-matching tasks, got %v", err)
	}

	// Case 3: Proper match and sorting
	now := time.Now()
	db.Create(&model.Task{
		ID:          10,
		WorkspaceID: workspaceID,
		UserID:      userID,
		Status:      "notstarted",
		Assignee:    "agent",
		SortOrder:   0, // fallback to CreatedAt
		CreatedAt:   now.Add(time.Hour),
	})
	db.Create(&model.Task{
		ID:          11,
		WorkspaceID: workspaceID,
		UserID:      userID,
		Status:      "notstarted",
		Assignee:    "agent",
		SortOrder:   5, // explicit sort order (prioritized)
		CreatedAt:   now.Add(2 * time.Hour),
	})
	db.Create(&model.Task{
		ID:          12,
		WorkspaceID: workspaceID,
		UserID:      userID,
		Status:      "notstarted",
		Assignee:    "agent",
		SortOrder:   10,
		CreatedAt:   now.Add(-time.Hour),
	})

	// Expected order:
	// 1. ID 11 (SortOrder 5)
	// 2. ID 12 (SortOrder 10)
	// 3. ID 10 (SortOrder 0 -> CreatedAt)

	task, err := repo.GetNextTask(ctx, workspaceID, userID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.ID != 11 {
		t.Errorf("expected task 11, got %d", task.ID)
	}

	// Case 4: Tie-break by ID
	db.Create(&model.Task{
		ID:          13,
		WorkspaceID: workspaceID,
		UserID:      userID,
		Status:      "notstarted",
		Assignee:    "agent",
		SortOrder:   5,
		CreatedAt:   now.Add(3 * time.Hour),
	})
	// Now 11 and 13 have same SortOrder 5. ID 11 should come first.
	task, _ = repo.GetNextTask(ctx, workspaceID, userID)
	if task.ID != 11 {
		t.Errorf("expected task 11 (tie-break by ID), got %d", task.ID)
	}
}

func TestRepository_UpdateMessageMetadata(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}

	_ = db.AutoMigrate(&model.Message{})
	repo := New(&mockDB{db: db})

	ctx := context.Background()
	taskID := int64(100)
	messageID := int64(500)

	db.Create(&model.Message{
		ID:     messageID,
		TaskID: taskID,
		Text:   "Initial text",
	})

	// Case 1: Success update with correct taskID
	err = repo.UpdateMessageMetadata(ctx, taskID, messageID, []byte(`{"updated":true}`))
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}

	var m model.Message
	db.First(&m, messageID)
	if string(m.Metadata) != `{"updated":true}` {
		t.Errorf("expected metadata to be updated, got %s", string(m.Metadata))
	}

	// Case 2: Update with WRONG taskID (IDOR)
	err = repo.UpdateMessageMetadata(ctx, 999, messageID, []byte(`{"hacked":true}`))
	if err != nil {
		t.Errorf("expected nil error (GORM Update doesn't return error on no rows), got %v", err)
	}

	db.First(&m, messageID)
	if string(m.Metadata) == `{"hacked":true}` {
		t.Error("vulnerability detected: metadata was updated with wrong taskID")
	}
}

func TestRepository_ListTasks_PreloadMessages(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}

	_ = db.AutoMigrate(&model.Task{}, &model.Message{})
	repo := New(&mockDB{db: db})

	ctx := context.Background()
	workspaceID := int64(1)
	userID := int64(10)

	// Create 12 tasks to test batching and limits
	for i := int64(1); i <= 12; i++ {
		db.Create(&model.Task{
			ID:          i,
			WorkspaceID: workspaceID,
			UserID:      userID,
			Title:       "Task",
			Status:      "notstarted",
		})

		// Create a message for the task
		db.Create(&model.Message{
			ID:     100 + i,
			TaskID: i,
			Text:   "Initial msg for task",
		})
	}

	// Case 1: Fetch tasks with PreloadMessages=true
	req := entity.ListTasksRequest{
		WorkspaceID:     workspaceID,
		PreloadMessages: true,
		Limit:           10,
	}

	tasks, err := repo.ListTasks(ctx, req, userID)
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}

	if len(tasks) != 10 {
		t.Errorf("expected 10 tasks, got %d", len(tasks))
	}

	for _, task := range tasks {
		if len(task.Messages) != 1 {
			t.Errorf("expected 1 preloaded message for task %d, got %d", task.ID, len(task.Messages))
		}
	}
}

func TestRepository_GetWorkspaceTaskCountsByCategory(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}

	_ = db.AutoMigrate(&model.Task{}, &model.Message{})
	repo := New(&mockDB{db: db})

	ctx := context.Background()
	workspaceID := int64(100)
	userID := int64(1)

	// Create test tasks in various categories
	// Ongoing: 2 tasks
	db.Create(&model.Task{ID: 1, WorkspaceID: workspaceID, UserID: userID, Status: "ongoing"})
	db.Create(&model.Task{ID: 2, WorkspaceID: workspaceID, UserID: userID, Status: "blocked"})

	// Not started: 3 tasks
	db.Create(&model.Task{ID: 3, WorkspaceID: workspaceID, UserID: userID, Status: "notstarted", Assignee: "agent"})
	db.Create(&model.Task{ID: 4, WorkspaceID: workspaceID, UserID: userID, Status: "notstarted", Assignee: "agent"})
	db.Create(&model.Task{ID: 5, WorkspaceID: workspaceID, UserID: userID, Status: "notstarted", Assignee: "human"})

	// Scheduled: 1 task
	db.Create(&model.Task{ID: 6, WorkspaceID: workspaceID, UserID: userID, Status: "cron"})

	// Completed: 2 tasks
	db.Create(&model.Task{ID: 7, WorkspaceID: workspaceID, UserID: userID, Status: "completed"})
	db.Create(&model.Task{ID: 8, WorkspaceID: workspaceID, UserID: userID, Status: "rejected"})

	// Pending (Action Required): 1 task
	db.Create(&model.Task{ID: 9, WorkspaceID: workspaceID, UserID: userID, Status: "ongoing"})
	db.Create(&model.Message{
		ID:        901,
		TaskID:    9,
		CreatedAt: time.Now(),
		Metadata:  []byte(`{"type":"permission_request","status":"pending"}`),
	})

	counts, err := repo.GetWorkspaceTaskCountsByCategory(ctx, workspaceID, userID)
	if err != nil {
		t.Fatalf("GetWorkspaceTaskCountsByCategory failed: %v", err)
	}

	if counts["ongoing"] != 3 { // ID 1, 2, 9
		t.Errorf("expected 3 ongoing tasks, got %d", counts["ongoing"])
	}
	if counts["notstarted"] != 3 { // ID 3, 4, 5
		t.Errorf("expected 3 notstarted tasks, got %d", counts["notstarted"])
	}
	if counts["scheduled"] != 1 { // ID 6
		t.Errorf("expected 1 scheduled tasks, got %d", counts["scheduled"])
	}
	if counts["completed"] != 2 { // ID 7, 8
		t.Errorf("expected 2 completed tasks, got %d", counts["completed"])
	}
	if counts["pending"] != 1 { // ID 9
		t.Errorf("expected 1 pending tasks, got %d", counts["pending"])
	}
}

func TestRepository_Event_CRUD(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}
	_ = db.AutoMigrate(&model.Event{}, &model.EventTrigger{})
	repo := New(&mockDB{db: db})

	ctx := context.Background()
	userID := int64(1)
	otherUserID := int64(2)

	// Create
	e, err := repo.CreateEvent(ctx, model.Event{
		ID:                100,
		UserID:            userID,
		Name:              "code_review_done",
		PayloadGuidelines: "Describe what was reviewed",
	})
	if err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	if e.ID != 100 {
		t.Errorf("expected ID 100, got %d", e.ID)
	}

	// GetEvent — owner
	got, err := repo.GetEvent(ctx, 100, userID)
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if got.Name != "code_review_done" {
		t.Errorf("expected name code_review_done, got %s", got.Name)
	}

	// GetEvent — wrong user (IDOR guard)
	_, err = repo.GetEvent(ctx, 100, otherUserID)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound for wrong user, got %v", err)
	}

	// GetEventByName
	got, err = repo.GetEventByName(ctx, "code_review_done", userID)
	if err != nil {
		t.Fatalf("GetEventByName: %v", err)
	}
	if got.ID != 100 {
		t.Errorf("expected ID 100, got %d", got.ID)
	}

	// GetEventByName — wrong user
	_, err = repo.GetEventByName(ctx, "code_review_done", otherUserID)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound for wrong user in GetEventByName, got %v", err)
	}

	// ListEventsByUser
	_ = db.Create(&model.Event{ID: 101, UserID: userID, Name: "deploy_done"})
	_ = db.Create(&model.Event{ID: 200, UserID: otherUserID, Name: "other_event"})

	list, err := repo.ListEventsByUser(ctx, userID)
	if err != nil {
		t.Fatalf("ListEventsByUser: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 events for user 1, got %d", len(list))
	}

	// DeleteEvent — wrong user (IDOR guard)
	err = repo.DeleteEvent(ctx, 100, otherUserID)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound when deleting with wrong user, got %v", err)
	}

	// DeleteEvent — owner
	err = repo.DeleteEvent(ctx, 100, userID)
	if err != nil {
		t.Fatalf("DeleteEvent: %v", err)
	}
	_, err = repo.GetEvent(ctx, 100, userID)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestRepository_DeleteEvent_CascadesTriggers(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}
	_ = db.AutoMigrate(&model.Event{}, &model.EventTrigger{})
	repo := New(&mockDB{db: db})

	ctx := context.Background()
	userID := int64(1)

	db.Create(&model.Event{ID: 100, UserID: userID, Name: "deploy_done"})
	db.Create(&model.Event{ID: 200, UserID: userID, Name: "tests_done"})
	// Trigger owned by the event being deleted.
	db.Create(&model.EventTrigger{ID: 1, EventID: 100, UserID: userID, WorkspaceID: 10, Title: "t1"})
	// Trigger of another event that chains to the event being deleted.
	db.Create(&model.EventTrigger{ID: 2, EventID: 200, UserID: userID, WorkspaceID: 10, Title: "t2", EmitEventID: 100})

	if err := repo.DeleteEvent(ctx, 100, userID); err != nil {
		t.Fatalf("DeleteEvent: %v", err)
	}

	var count int64
	db.Model(&model.EventTrigger{}).Where("event_id = ?", 100).Count(&count)
	if count != 0 {
		t.Errorf("expected the deleted event's triggers to be removed, found %d", count)
	}

	var chained model.EventTrigger
	if err := db.First(&chained, 2).Error; err != nil {
		t.Fatalf("trigger of the surviving event should not be deleted: %v", err)
	}
	if chained.EmitEventID != 0 {
		t.Errorf("expected dangling emitEventId to be cleared, got %d", chained.EmitEventID)
	}
}

func TestRepository_EventTrigger_CRUD(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}
	_ = db.AutoMigrate(&model.Event{}, &model.EventTrigger{}, &model.Task{})
	repo := New(&mockDB{db: db})

	ctx := context.Background()
	userID := int64(1)
	otherUserID := int64(2)
	eventID := int64(500)
	workspaceID := int64(10)

	// Create trigger
	tr, err := repo.CreateEventTrigger(ctx, model.EventTrigger{
		ID:          1000,
		EventID:     eventID,
		WorkspaceID: workspaceID,
		UserID:      userID,
		Title:       "Review completed: {{EVENT_PAYLOAD}}",
		Body:        "Details: {{EVENT_FAQ}}",
		Assignee:    "agent",
	})
	if err != nil {
		t.Fatalf("CreateEventTrigger: %v", err)
	}
	if tr.ID != 1000 {
		t.Errorf("expected ID 1000, got %d", tr.ID)
	}

	// GetEventTrigger — owner
	got, err := repo.GetEventTrigger(ctx, 1000, userID)
	if err != nil {
		t.Fatalf("GetEventTrigger: %v", err)
	}
	if got.EventID != eventID {
		t.Errorf("expected eventID %d, got %d", eventID, got.EventID)
	}

	// GetEventTrigger — wrong user (IDOR)
	_, err = repo.GetEventTrigger(ctx, 1000, otherUserID)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound for wrong user, got %v", err)
	}

	// ListEventTriggersByEvent — user-scoped
	_ = db.Create(&model.EventTrigger{ID: 1001, EventID: eventID, UserID: userID, WorkspaceID: 20})
	_ = db.Create(&model.EventTrigger{ID: 1002, EventID: eventID, UserID: otherUserID, WorkspaceID: 30})

	list, err := repo.ListEventTriggersByEvent(ctx, eventID, userID)
	if err != nil {
		t.Fatalf("ListEventTriggersByEvent: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 triggers for user 1 on event, got %d", len(list))
	}

	// SystemListEventTriggersByEventID — cross-user
	all, err := repo.SystemListEventTriggersByEventID(ctx, eventID)
	if err != nil {
		t.Fatalf("SystemListEventTriggersByEventID: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 triggers system-wide, got %d", len(all))
	}

	// DeleteEventTrigger — wrong user (IDOR)
	err = repo.DeleteEventTrigger(ctx, 1000, otherUserID)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound for wrong user delete, got %v", err)
	}

	// DeleteEventTrigger — owner
	err = repo.DeleteEventTrigger(ctx, 1000, userID)
	if err != nil {
		t.Fatalf("DeleteEventTrigger: %v", err)
	}
	_, err = repo.GetEventTrigger(ctx, 1000, userID)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestRepository_ListTasksByTriggerID(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}
	_ = db.AutoMigrate(&model.Task{})
	repo := New(&mockDB{db: db})

	ctx := context.Background()
	userID := int64(1)
	otherUserID := int64(2)
	triggerID := int64(500)

	db.Create(&model.Task{ID: 1, UserID: userID, TriggerID: triggerID, WorkspaceID: 10, Status: "completed"})
	db.Create(&model.Task{ID: 2, UserID: userID, TriggerID: triggerID, WorkspaceID: 10, Status: "notstarted"})
	db.Create(&model.Task{ID: 3, UserID: otherUserID, TriggerID: triggerID, WorkspaceID: 20, Status: "completed"})
	db.Create(&model.Task{ID: 4, UserID: userID, TriggerID: 999, WorkspaceID: 10, Status: "completed"})

	tasks, err := repo.ListTasksByTriggerID(ctx, triggerID, userID)
	if err != nil {
		t.Fatalf("ListTasksByTriggerID: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks for user 1 with triggerID %d, got %d", triggerID, len(tasks))
	}
	for _, task := range tasks {
		if task.TriggerID != triggerID {
			t.Errorf("unexpected triggerID %d", task.TriggerID)
		}
		if task.UserID != userID {
			t.Errorf("unexpected userID %d (IDOR leak)", task.UserID)
		}
	}
}

func TestRepository_Swarm(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}
	_ = db.AutoMigrate(&model.Swarm{}, &model.Task{})
	repo := New(&mockDB{db: db})
	ctx := context.Background()

	created, err := repo.CreateSwarm(ctx, model.Swarm{
		ID:                1,
		WorkspaceID:        10,
		Name:               "test-swarm",
		LeaderWorkspaceID:  100,
		MemberWorkspaceIDs: []byte(`[100,200,300]`),
	})
	if err != nil {
		t.Fatalf("CreateSwarm: %v", err)
	}
	if created.ID != 1 {
		t.Errorf("expected ID=1, got %d", created.ID)
	}

	got, err := repo.SystemGetSwarm(ctx, 1)
	if err != nil {
		t.Fatalf("SystemGetSwarm: %v", err)
	}
	if got.Name != "test-swarm" || got.LeaderWorkspaceID != 100 {
		t.Errorf("unexpected swarm: %+v", got)
	}

	_, err = repo.SystemGetSwarm(ctx, 999)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestRepository_ListChildTasksAndCountIncompleteChildren(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}
	_ = db.AutoMigrate(&model.Task{})
	repo := New(&mockDB{db: db})
	ctx := context.Background()

	db.Create(&model.Task{ID: 1, WorkspaceID: 100, Status: "notstarted"}) // parent
	db.Create(&model.Task{ID: 2, WorkspaceID: 200, ParentID: 1, Status: "completed"})
	db.Create(&model.Task{ID: 3, WorkspaceID: 300, ParentID: 1, Status: "ongoing"})
	db.Create(&model.Task{ID: 4, WorkspaceID: 400, ParentID: 1, Status: "notstarted"})
	db.Create(&model.Task{ID: 5, WorkspaceID: 500, ParentID: 999, Status: "ongoing"}) // unrelated parent

	children, err := repo.ListChildTasks(ctx, 1)
	if err != nil {
		t.Fatalf("ListChildTasks: %v", err)
	}
	if len(children) != 3 {
		t.Errorf("expected 3 children, got %d", len(children))
	}

	count, err := repo.CountIncompleteChildren(ctx, 1)
	if err != nil {
		t.Fatalf("CountIncompleteChildren: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 incomplete children (ongoing+notstarted), got %d", count)
	}

	// Mark remaining two terminal; count should drop to 0.
	db.Model(&model.Task{}).Where("id IN ?", []int64{3, 4}).Update("status", "completed")
	count, err = repo.CountIncompleteChildren(ctx, 1)
	if err != nil {
		t.Fatalf("CountIncompleteChildren (2nd): %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 incomplete children after completing siblings, got %d", count)
	}
}

