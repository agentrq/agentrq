// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/mustafaturan/monoflake"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
)

// teamStreamDBs is a SQLite file, and a fresh Postgres database when
// AGENTRQ_TEST_POSTGRES_DSN is set.
func teamStreamDBs(t *testing.T) map[string]*gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+filepath.Join(t.TempDir(), "a.db")+"?_pragma=busy_timeout(5000)"),
		&gorm.Config{TranslateError: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	dbs := map[string]*gorm.DB{"sqlite": db}
	if os.Getenv("AGENTRQ_TEST_POSTGRES_DSN") != "" {
		dbs["postgres"] = openPostgres(t, freshPostgres(t))
	}
	return dbs
}

// streamEvent is one event read off a stream.
type streamEvent struct {
	Type    string         `json:"type"`
	TeamID  string         `json:"teamId"`
	Payload map[string]any `json:"payload"`
}

// drain is every event waiting on ch.
func drain(t *testing.T, ch chan []byte) []streamEvent {
	t.Helper()
	var out []streamEvent
	for {
		select {
		case line := <-ch:
			var e streamEvent
			if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(string(line), "data: "))), &e); err != nil {
				t.Fatal(err)
			}
			out = append(out, e)
		default:
			return out
		}
	}
}

// kinds is the events as "type@team", sorted.
func kinds(es []streamEvent) string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Type + "@" + e.TeamID
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

// A teammate's open stream gets the task events of every workspace shared
// with a team of theirs, as team.<type> tagged with the team, and stops
// getting them the moment the share, their membership or the workspace goes.
func TestForwardCRUDEvent_Teams(t *testing.T) {
	for name, db := range teamStreamDBs(t) {
		t.Run(name, func(t *testing.T) {
			if err := db.AutoMigrate(&model.User{}, &model.Workspace{}, &model.Task{}, &model.Message{}, &model.ToolCall{}, &model.TaskPlan{},
				&model.Team{}, &model.TeamMember{}, &model.WorkspaceShare{}); err != nil {
				t.Fatal(err)
			}
			// Alice (1) shares 100 with team 10 (Alice, Bob) and team 20
			// (Alice, Erin); 101 is its fork; 102 is not shared. Dave (3) is
			// in no team.
			for _, row := range []any{
				&model.User{ID: 1, Name: "Alice", Email: "a@x"}, &model.User{ID: 2, Name: "Bob", Email: "b@x"}, &model.User{ID: 3, Name: "Dave", Email: "d@x"}, &model.User{ID: 5, Name: "Erin", Email: "e@x"},
				&model.Workspace{ID: 100, UserID: 1, Name: "backend"},
				&model.Workspace{ID: 101, UserID: 1, Name: "fork", ForkOfID: 100},
				&model.Workspace{ID: 102, UserID: 1, Name: "secret"},
				&model.Team{ID: 10, Name: "core"}, &model.Team{ID: 20, Name: "ops"},
				&model.TeamMember{ID: 11, TeamID: 10, UserID: 1, Role: "owner"},
				&model.TeamMember{ID: 12, TeamID: 10, UserID: 2, Role: "member"},
				&model.TeamMember{ID: 21, TeamID: 20, UserID: 1, Role: "owner"},
				&model.TeamMember{ID: 25, TeamID: 20, UserID: 5, Role: "member"},
				&model.WorkspaceShare{ID: 31, TeamID: 10, WorkspaceID: 100, UserID: 1, GroupName: "Backend"},
				&model.WorkspaceShare{ID: 32, TeamID: 20, WorkspaceID: 100, UserID: 1, GroupName: "Services"},
				&model.Task{ID: 1000, WorkspaceID: 100, UserID: 1, Title: "shared", Status: "ongoing"},
				&model.Task{ID: 1010, WorkspaceID: 101, UserID: 1, Title: "forked", Status: "ongoing"},
				&model.Task{ID: 1020, WorkspaceID: 102, UserID: 1, Title: "secret", Status: "ongoing"},
			} {
				if err := db.Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}
			repo := base.New(sqliteConn{db: db})
			bus := eventbus.New()
			sub := func(id int64) chan []byte { return bus.Subscribe(0, monoflake.ID(id).String()) }
			alice, bob, dave, erin := sub(1), sub(2), sub(3), sub(5)
			ten, twenty := monoflake.ID(10).String(), monoflake.ID(20).String()
			update := func(ws, task int64) {
				forwardCRUDEvent(context.Background(), repo, nil, bus, entity.CRUDEvent{
					WorkspaceID: ws, UserID: 1, ResourceType: entity.ResourceTask, Action: entity.ActionTaskUpdate, ResourceID: task,
				})
			}
			expect := func(what string, ch chan []byte, want string) []streamEvent {
				t.Helper()
				es := drain(t, ch)
				if got := kinds(es); got != want {
					t.Errorf("%s: got %q, want %q", what, got, want)
				}
				return es
			}

			update(100, 1000)
			expect("Alice, the owner, in both teams", alice, "task.updated@ team.task.updated@"+ten+" team.task.updated@"+twenty)
			es := expect("Bob, in team 10", bob, "team.task.updated@"+ten)
			if len(es) == 1 {
				owner, _ := es[0].Payload["owner"].(map[string]any)
				if es[0].Payload["id"] != monoflake.ID(1000).String() || es[0].Payload["group"] != "Backend" || owner["name"] != "Alice" {
					t.Errorf("Bob's event carries %v", es[0].Payload)
				}
			}
			if es := expect("Erin, in team 20", erin, "team.task.updated@"+twenty); len(es) == 1 && es[0].Payload["group"] != "Services" {
				t.Errorf("Erin's event is in group %v, want her team's", es[0].Payload["group"])
			}
			expect("Dave, in no team", dave, "")

			update(101, 1010)
			expect("Bob, for the fork of a shared workspace", bob, "team.task.updated@"+ten)
			update(102, 1020)
			expect("Bob, for an unshared workspace", bob, "")
			drain(t, alice)
			drain(t, erin)

			forwardCRUDEvent(context.Background(), repo, nil, bus, entity.CRUDEvent{
				WorkspaceID: 100, UserID: 1, ResourceType: entity.ResourceTask, Action: entity.ActionTaskDelete, ResourceID: 1000,
			})
			if es := expect("Bob, for a delete", bob, "team.task.deleted@"+ten); len(es) == 1 && es[0].Payload["id"] != monoflake.ID(1000).String() {
				t.Errorf("the delete carries %v", es[0].Payload)
			}
			drain(t, erin)

			// Each change is seen by the very next event.
			db.Delete(&model.WorkspaceShare{}, 32)
			update(100, 1000)
			expect("Erin, after 100 was unshared from her team", erin, "")
			expect("Bob, whose team still has it", bob, "team.task.updated@"+ten)

			db.Delete(&model.TeamMember{}, 12)
			update(100, 1000)
			update(101, 1010)
			expect("Bob, just removed from the team", bob, "")

			db.Create(&model.TeamMember{ID: 13, TeamID: 10, UserID: 2, Role: "member"})
			update(100, 1000)
			expect("Bob, back in the team", bob, "team.task.updated@"+ten)
			db.Model(&model.Workspace{}).Where("id = ?", 100).Update("archived_at", time.Now())
			update(100, 1000)
			update(101, 1010)
			expect("Bob, once 100 was archived", bob, "")
		})
	}
}

// teamReadersStub answers publishToTeams' reads from fixed values.
type teamReadersStub struct {
	readers   []base.TeamReader
	err       error
	infoErr   error
	listCalls int
}

func (s *teamReadersStub) ListTeamReaders(context.Context, int64) ([]base.TeamReader, error) {
	s.listCalls++
	return s.readers, s.err
}

func (s *teamReadersStub) TeamWorkspaceInfo(_ context.Context, teamID, workspaceID int64) (base.TeamWorkspaceRow, error) {
	return base.TeamWorkspaceRow{ID: workspaceID, GroupName: "g", OwnerName: "Alice"}, s.infoErr
}

// With no account stream open nothing is read; a failed read sends nothing;
// a teammate whose workspace cannot be read is skipped.
func TestPublishToTeams_Failures(t *testing.T) {
	evt := eventbus.Event{Type: "task.updated", Payload: view.Task{ID: "x"}}

	bus := eventbus.New()
	repo := &teamReadersStub{readers: []base.TeamReader{{TeamID: 10, UserID: 2}}}
	page := bus.Subscribe(100, "")
	publishToTeams(context.Background(), repo, bus, 100, evt)
	if repo.listCalls != 0 || len(drain(t, page)) != 0 {
		t.Errorf("with no account stream open: %d reads", repo.listCalls)
	}

	bob := bus.Subscribe(0, monoflake.ID(2).String())
	repo.err = errors.New("database unavailable")
	publishToTeams(context.Background(), repo, bus, 100, evt)
	if got := drain(t, bob); repo.listCalls != 1 || len(got) != 0 {
		t.Errorf("a failed read of the readers sent %v", got)
	}

	repo.err, repo.infoErr = nil, errors.New("database unavailable")
	publishToTeams(context.Background(), repo, bus, 100, evt)
	if got := drain(t, bob); len(got) != 0 {
		t.Errorf("a failed read of the workspace sent %v", got)
	}

	// Two readers of one team read the workspace once; one with no stream
	// open is passed over.
	repo.infoErr = nil
	repo.readers = []base.TeamReader{{TeamID: 10, UserID: 2}, {TeamID: 10, UserID: 4}, {TeamID: 10, UserID: 6}}
	carol := bus.Subscribe(0, monoflake.ID(4).String())
	publishToTeams(context.Background(), repo, bus, 100, evt)
	if b, c := drain(t, bob), drain(t, carol); len(b) != 1 || len(c) != 1 || b[0].Payload["group"] != "g" || c[0].Type != "team.task.updated" {
		t.Errorf("Bob got %v, Carol %v", b, c)
	}
}
