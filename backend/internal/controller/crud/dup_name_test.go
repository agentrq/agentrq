package crud

import (
	"context"
	"errors"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/glebarez/sqlite"
	"github.com/mustafaturan/monoflake"
	"gorm.io/gorm"
)

// openTestDB mirrors how the app opens its connection. TranslateError is the
// part that matters: it converts a driver's own duplicate-key error into
// gorm.ErrDuplicatedKey, and omitting it here is what let an earlier version of
// these tests pass while production still answered 500.
type realDB struct{ db *gorm.DB }

func (m *realDB) Conn(ctx context.Context) *gorm.DB { return m.db }
func (m *realDB) Close(ctx context.Context)         {}

type seqIDGen struct{ n int64 }

func (s *seqIDGen) NextID() int64        { s.n++; return s.n }
func (s *seqIDGen) NextIDString() string { s.n++; return monoflake.ID(s.n).String() }

// A name already taken by the same user must surface as ErrDuplicateName, not
// as a bare internal error — a collision is the user's input, and the caller
// has to be able to tell it apart from a real failure.
func TestCreateEvent_DuplicateNameSameUser(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err := db.AutoMigrate(&model.Event{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	c := &controller{repository: base.New(&realDB{db: db}), idgen: &seqIDGen{}}
	ctx := context.Background()
	user := monoflake.ID(4242).String()

	if _, err := c.CreateEvent(ctx, entity.CreateEventRequest{Name: "deploy", UserID: user}); err != nil {
		t.Fatalf("first create: %v", err)
	}

	_, err := c.CreateEvent(ctx, entity.CreateEventRequest{Name: "deploy", UserID: user})
	if err == nil {
		t.Fatal("expected a duplicate error")
	}
	t.Logf("raw error: %v", err)
	if !errors.Is(err, ErrDuplicateName) {
		t.Errorf("error must wrap ErrDuplicateName so the handler can answer 409, got: %v", err)
	}
}

// The unique index is scoped per user, so two accounts may hold the same name.
func TestCreateEvent_SameNameDifferentUsers(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err := db.AutoMigrate(&model.Event{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	c := &controller{repository: base.New(&realDB{db: db}), idgen: &seqIDGen{}}
	ctx := context.Background()

	if _, err := c.CreateEvent(ctx, entity.CreateEventRequest{Name: "deploy", UserID: monoflake.ID(1).String()}); err != nil {
		t.Fatalf("user 1: %v", err)
	}
	if _, err := c.CreateEvent(ctx, entity.CreateEventRequest{Name: "deploy", UserID: monoflake.ID(2).String()}); err != nil {
		t.Errorf("a second user must be able to use the same name, got: %v", err)
	}
}

// Deleting an event frees its name immediately: the row is hard-deleted, so
// there is nothing left to collide with.
func TestCreateEvent_NameReusableAfterDelete(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err := db.AutoMigrate(&model.Event{}, &model.EventTrigger{}, &model.WorkflowStep{}, &model.Workflow{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	c := &controller{repository: base.New(&realDB{db: db}), idgen: &seqIDGen{}}
	ctx := context.Background()
	user := monoflake.ID(7).String()

	created, err := c.CreateEvent(ctx, entity.CreateEventRequest{Name: "deploy", UserID: user})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := c.DeleteEvent(ctx, entity.DeleteEventRequest{ID: created.Event.ID, UserID: user}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := c.CreateEvent(ctx, entity.CreateEventRequest{Name: "deploy", UserID: user}); err != nil {
		t.Errorf("name must be reusable after delete, got: %v", err)
	}
}
