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
				&model.TeamMember{ID: 11, TeamID: 10, UserID: 1},
				&model.TeamMember{ID: 12, TeamID: 10, UserID: 2},
				&model.TeamMember{ID: 21, TeamID: 20, UserID: 1},
				&model.TeamMember{ID: 25, TeamID: 20, UserID: 5},
				&model.WorkspaceShare{ID: 31, TeamID: 10, WorkspaceID: 100, UserID: 1},
				&model.WorkspaceShare{ID: 32, TeamID: 20, WorkspaceID: 100, UserID: 1},
				&model.Task{ID: 1000, WorkspaceID: 100, UserID: 1, Title: "shared", Status: "ongoing",
					Attachments: []byte(`[{"id":"f1","filename":"a.html","mimeType":"text/html","url":"/storage/artifacts/w-1/1000/f1"}]`)},
				&model.Message{ID: 900, TaskID: 1000, UserID: 1, Sender: "agent", Text: "see",
					Attachments: []byte(`[{"id":"f2","filename":"b.svg","mimeType":"image/svg+xml","url":"/storage/artifacts/w-1/1000/f2"}]`)},
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
			// A teammate's links to the task's files go through the team's
			// route, which ends with the share; the owner keeps hers.
			files := func(e streamEvent) string {
				var out []string
				for _, list := range []any{e.Payload["attachments"], e.Payload["messages"]} {
					items, _ := list.([]any)
					for _, it := range items {
						o, _ := it.(map[string]any)
						if atts, ok := o["attachments"].([]any); ok {
							for _, a := range atts {
								out = append(out, a.(map[string]any)["url"].(string))
							}
						} else {
							out = append(out, o["url"].(string))
						}
					}
				}
				return strings.Join(out, " ")
			}
			teamFiles := func(team string) string {
				base := "/api/v1/teams/" + team + "/tasks/" + monoflake.ID(1000).String() + "/attachments/"
				return base + "f1 " + base + "f2"
			}
			for _, e := range expect("Alice, the owner, in both teams", alice, "task.updated@ team.task.updated@"+ten+" team.task.updated@"+twenty) {
				want := map[string]string{"": "/storage/artifacts/w-1/1000/f1 /storage/artifacts/w-1/1000/f2", ten: teamFiles(ten), twenty: teamFiles(twenty)}[e.TeamID]
				if got := files(e); got != want {
					t.Errorf("Alice's %s@%s links to %q, want %q", e.Type, e.TeamID, got, want)
				}
			}
			es := expect("Bob, in team 10", bob, "team.task.updated@"+ten)
			if len(es) == 1 {
				owner, _ := es[0].Payload["owner"].(map[string]any)
				if es[0].Payload["id"] != monoflake.ID(1000).String() || es[0].Payload["sharedName"] != "backend" || es[0].Payload["group"] != nil || owner["name"] != "Alice" {
					t.Errorf("Bob's event carries %v", es[0].Payload)
				}
				if got := files(es[0]); got != teamFiles(ten) {
					t.Errorf("Bob's event links to %q", got)
				}
			}
			if es := expect("Erin, in team 20", erin, "team.task.updated@"+twenty); len(es) == 1 && es[0].Payload["sharedName"] != "backend" {
				t.Errorf("Erin's event is under %v, want the workspace's own name", es[0].Payload["sharedName"])
			}
			expect("Dave, in no team", dave, "")

			// An event naming a shared workspace for a task that is now in
			// an unshared one reaches the owner alone.
			update(100, 1020)
			expect("Bob, for a task moved out of the shared workspace", bob, "")
			expect("Alice, for a task moved out of the shared workspace", alice, "task.updated@")
			update(102, 1000)
			expect("Bob, for an event of an unshared workspace whose task is now in a shared one", bob, "")
			drain(t, alice)
			drain(t, erin)

			update(101, 1010)
			if es := expect("Bob, for the fork of a shared workspace", bob, "team.task.updated@"+ten); len(es) == 1 && es[0].Payload["sharedName"] != "backend" {
				t.Errorf("the fork's task is under %v, want its parent's name", es[0].Payload["sharedName"])
			}
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

			db.Create(&model.TeamMember{ID: 13, TeamID: 10, UserID: 2})
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
	infoCalls int
}

func (s *teamReadersStub) ListTeamReaders(context.Context, int64) ([]base.TeamReader, error) {
	s.listCalls++
	return s.readers, s.err
}

func (s *teamReadersStub) TeamWorkspaceInfo(_ context.Context, workspaceID int64) (base.TeamWorkspaceRow, error) {
	s.infoCalls++
	return base.TeamWorkspaceRow{ID: workspaceID, SharedName: "g", OwnerName: "Alice"}, s.infoErr
}

// With no account stream open nothing is read; a failed read sends nothing;
// a workspace that cannot be read sends nothing to anyone.
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
	repo.readers = []base.TeamReader{{TeamID: 10, UserID: 2}, {TeamID: 20, UserID: 2}}
	publishToTeams(context.Background(), repo, bus, 100, evt)
	if got := drain(t, bob); len(got) != 0 || repo.infoCalls != 1 {
		t.Errorf("a failed read of the workspace sent %v after %d reads, want nothing after one", got, repo.infoCalls)
	}

	// Readers in two teams read the workspace once; one with no stream open
	// is passed over; an event that carries no task is sent as it is.
	repo.infoErr, repo.infoCalls = nil, 0
	repo.readers = []base.TeamReader{{TeamID: 10, UserID: 2}, {TeamID: 20, UserID: 4}, {TeamID: 10, UserID: 6}}
	carol := bus.Subscribe(0, monoflake.ID(4).String())
	publishToTeams(context.Background(), repo, bus, 100, evt)
	b, c := drain(t, bob), drain(t, carol)
	if len(b) != 1 || len(c) != 1 || b[0].Payload["sharedName"] != "g" || c[0].Payload["sharedName"] != "g" || c[0].Type != "team.task.updated" ||
		c[0].TeamID != monoflake.ID(20).String() || repo.infoCalls != 1 {
		t.Errorf("Bob got %v, Carol %v, after %d reads of the workspace", b, c, repo.infoCalls)
	}
	publishToTeams(context.Background(), repo, bus, 100, eventbus.Event{Type: "task.deleted", Payload: map[string]any{"id": "x"}})
	if b := drain(t, bob); len(b) != 1 || b[0].Payload["id"] != "x" || repo.infoCalls != 1 {
		t.Errorf("a delete sent Bob %v after %d reads", b, repo.infoCalls)
	}
}
