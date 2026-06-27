package backfill

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	backfillsvc "github.com/agentrq/agentrq/backend/internal/service/backfill"
	"github.com/glebarez/sqlite"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type testDB struct{ db *gorm.DB }

func (m *testDB) Conn(ctx context.Context) *gorm.DB { return m.db }
func (m *testDB) Close(ctx context.Context)         {}

func newTestRepo(t *testing.T) (base.Repository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.Message{}, &model.Backfill{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return base.New(&testDB{db: db}), db
}

func metaOf(t *testing.T, db *gorm.DB, id int64) map[string]any {
	t.Helper()
	var m model.Message
	if err := db.First(&m, "id = ?", id).Error; err != nil {
		t.Fatalf("load message %d: %v", id, err)
	}
	out := map[string]any{}
	if len(m.Metadata) > 0 {
		if err := json.Unmarshal(m.Metadata, &out); err != nil {
			t.Fatalf("unmarshal metadata: %v", err)
		}
	}
	return out
}

func newController(t *testing.T) (*controller, *gorm.DB) {
	t.Helper()
	repo, db := newTestRepo(t)
	return New(repo, backfillsvc.New(repo)).(*controller), db
}

func TestBackfillMessageMetadataKeys(t *testing.T) {
	c, db := newController(t)
	ctx := context.Background()

	db.Create(&model.Message{ID: 1, TaskID: 10, Metadata: datatypes.JSON(`{"type":"permission_request","request_id":"req-1","tool_name":"bash","input_preview":"ls","status":"pending"}`)})
	db.Create(&model.Message{ID: 2, TaskID: 10, Metadata: datatypes.JSON(`{"slack_user":"alice","decided_in_slack":true,"slack_user_id":"U1","slack_user_name":"Alice"}`)})
	db.Create(&model.Message{ID: 3, TaskID: 10, Metadata: datatypes.JSON(`{"type":"permission_request","requestId":"req-3","toolName":"bash"}`)})
	db.Create(&model.Message{ID: 4, TaskID: 10})
	db.Create(&model.Message{ID: 5, TaskID: 10, Metadata: datatypes.JSON(`{"request_id":"old","requestId":"new"}`)})

	if err := c.backfillMessageMetadataKeys(ctx); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	m1 := metaOf(t, db, 1)
	for _, k := range []string{"request_id", "tool_name", "input_preview"} {
		if _, ok := m1[k]; ok {
			t.Errorf("msg1: legacy key %q should have been removed", k)
		}
	}
	if m1["requestId"] != "req-1" || m1["toolName"] != "bash" || m1["inputPreview"] != "ls" {
		t.Errorf("msg1: unexpected camelCase values: %v", m1)
	}
	if m1["type"] != "permission_request" || m1["status"] != "pending" {
		t.Errorf("msg1: non-renamed keys should be preserved: %v", m1)
	}

	m2 := metaOf(t, db, 2)
	if m2["slackUser"] != "alice" || m2["decidedInSlack"] != true || m2["slackUserId"] != "U1" || m2["slackUserName"] != "Alice" {
		t.Errorf("msg2: unexpected values: %v", m2)
	}
	for _, k := range []string{"slack_user", "decided_in_slack", "slack_user_id", "slack_user_name"} {
		if _, ok := m2[k]; ok {
			t.Errorf("msg2: legacy key %q should have been removed", k)
		}
	}

	if m3 := metaOf(t, db, 3); m3["requestId"] != "req-3" || m3["toolName"] != "bash" {
		t.Errorf("msg3: camelCase values should be untouched: %v", m3)
	}

	m5 := metaOf(t, db, 5)
	if m5["requestId"] != "new" {
		t.Errorf("msg5: existing camelCase value must win, got %v", m5["requestId"])
	}
	if _, ok := m5["request_id"]; ok {
		t.Errorf("msg5: legacy key should be dropped, got %v", m5)
	}

	// Idempotent: a second run changes nothing.
	if err := c.backfillMessageMetadataKeys(ctx); err != nil {
		t.Fatalf("backfill (2nd run): %v", err)
	}
	if again := metaOf(t, db, 1); len(again) != len(m1) {
		t.Errorf("msg1: second run altered metadata: before=%v after=%v", m1, again)
	}
}

// TestRun_SkipsWhenApplied verifies that a recorded backfill is not re-run when
// the controller is invoked through the service.
func TestRun_SkipsWhenApplied(t *testing.T) {
	c, db := newController(t)
	ctx := context.Background()

	for _, b := range c.backfills() {
		if err := c.repo.SystemRecordBackfill(ctx, b.Name); err != nil {
			t.Fatalf("record backfill: %v", err)
		}
	}
	db.Create(&model.Message{ID: 1, TaskID: 10, Metadata: datatypes.JSON(`{"request_id":"req-1"}`)})

	if err := c.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, ok := metaOf(t, db, 1)["request_id"]; !ok {
		t.Errorf("msg1 should be untouched when all backfills already applied")
	}
}

func TestBackfillNamesStartWithDate(t *testing.T) {
	c := New(nil, nil).(*controller)
	for _, b := range c.backfills() {
		if len(b.Name) < 7 || b.Name[6] != '_' {
			t.Errorf("backfill name %q must start with YYMMDD_ prefix", b.Name)
			continue
		}
		for i := range 6 {
			if b.Name[i] < '0' || b.Name[i] > '9' {
				t.Errorf("backfill name %q must start with a 6-digit YYMMDD date", b.Name)
				break
			}
		}
	}
}
