// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	teamctrl "github.com/agentrq/agentrq/backend/internal/controller/team"
	"github.com/agentrq/agentrq/backend/internal/controller/teamview"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// teamModeDatabases is a SQLite file, and a database of its own on the
// Postgres AGENTRQ_TEST_POSTGRES_DSN names, when it is set.
func teamModeDatabases(t *testing.T) map[string]*gorm.DB {
	t.Helper()
	open := func(d gorm.Dialector) *gorm.DB {
		db, err := gorm.Open(d, &gorm.Config{TranslateError: true, Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if sqlDB, err := db.DB(); err == nil {
				_ = sqlDB.Close()
			}
		})
		return db
	}
	dbs := map[string]*gorm.DB{"sqlite": open(sqlite.Open("file:" + filepath.Join(t.TempDir(), "a.db") + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"))}
	if dsn := os.Getenv("AGENTRQ_TEST_POSTGRES_DSN"); dsn != "" {
		admin := open(postgres.Open(dsn))
		name := fmt.Sprintf("team_mode_%d", time.Now().UnixNano())
		if err := admin.Exec("CREATE DATABASE " + name).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { admin.Exec("DROP DATABASE " + name + " WITH (FORCE)") })
		dbs["postgres"] = open(postgres.Open(dsn + " dbname=" + name))
	}
	return dbs
}

// teamMode is the cast: Alice and Bob in team 10, Dave outside it.
//
//   - 100 "backend", Alice's, shared; 101 "backend fork" is its fork.
//   - 102 "secret", Alice's, not shared; 103 "old", shared, then archived.
//   - 200 "Backend ", Bob's, shared: the same team workspace as 100.
//   - 202 "api", Bob's, shared: a team workspace of its own.
//   - 201 "mine", Bob's, not shared: where he clones to.
type teamMode struct {
	app        *fiber.App
	db         *gorm.DB
	attachTask string // Alice's task in 100 with a file
	attachment string
	html, svg  string // two more of its files, a browser would run
}

var (
	teamTen  = monoflake.ID(10).String()
	ws100    = monoflake.ID(100).String()
	ws101    = monoflake.ID(101).String()
	ws102    = monoflake.ID(102).String()
	ws103    = monoflake.ID(103).String()
	ws200    = monoflake.ID(200).String()
	ws201    = monoflake.ID(201).String()
	ws202    = monoflake.ID(202).String()
	task1010 = monoflake.ID(1010).String() // in 101, the fork
	task1020 = monoflake.ID(1020).String() // in 102, unshared
	task1030 = monoflake.ID(1030).String() // in 103, archived
	task2000 = monoflake.ID(2000).String() // Bob's, in 200
	task2010 = monoflake.ID(2010).String() // Bob's, in 201, unshared
)

func newTeamMode(t *testing.T, db *gorm.DB) *teamMode {
	t.Helper()
	ctx := context.Background()
	if err := db.AutoMigrate(&model.User{}, &model.Workspace{}, &model.Task{}, &model.Message{}, &model.ToolCall{}, &model.TaskPlan{},
		&model.Telemetry{}, &model.Memory{}, &model.Skill{}, &model.SkillFile{}, &model.WorkspaceSkill{}, &model.PanoDoc{}, &model.PanoDocTask{},
		&model.Agent{}, &model.AgentModel{}, &model.TaskStateTransition{},
		&model.Team{}, &model.TeamMember{}, &model.TeamInvitation{}, &model.WorkspaceShare{}, &model.TeamManagementActivity{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	archived := now.Add(-time.Hour)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []any{
		&model.User{ID: 1, Name: "Alice", Email: "alice@example.com", Picture: "a.png"},
		&model.User{ID: 2, Name: "Bob", Email: "bob@example.com"},
		&model.User{ID: 3, Name: "Dave", Email: "dave@example.com"},
		&model.Workspace{ID: 100, UserID: 1, Name: "backend"},
		&model.Workspace{ID: 101, UserID: 1, Name: "backend fork", ForkOfID: 100},
		&model.Workspace{ID: 102, UserID: 1, Name: "secret"},
		&model.Workspace{ID: 103, UserID: 1, Name: "old", ArchivedAt: &archived},
		&model.Workspace{ID: 200, UserID: 2, Name: "Backend "},
		&model.Workspace{ID: 201, UserID: 2, Name: "mine"},
		&model.Workspace{ID: 202, UserID: 2, Name: "api"},
		&model.Team{ID: 10, Name: "core", Version: 1, CreatedAt: now, UpdatedAt: now},
		&model.TeamMember{ID: 11, TeamID: 10, UserID: 1},
		&model.TeamMember{ID: 12, TeamID: 10, UserID: 2},
		&model.WorkspaceShare{ID: 21, TeamID: 10, WorkspaceID: 100, UserID: 1},
		&model.WorkspaceShare{ID: 22, TeamID: 10, WorkspaceID: 103, UserID: 1},
		&model.WorkspaceShare{ID: 23, TeamID: 10, WorkspaceID: 200, UserID: 2},
		&model.WorkspaceShare{ID: 24, TeamID: 10, WorkspaceID: 202, UserID: 2},
		// Dave's own team, and what it shares, is no business of team 10.
		&model.Workspace{ID: 300, UserID: 3, Name: "backend"},
		&model.Team{ID: 30, Name: "other", Version: 1, CreatedAt: now, UpdatedAt: now},
		&model.TeamMember{ID: 31, TeamID: 30, UserID: 3},
		&model.WorkspaceShare{ID: 33, TeamID: 30, WorkspaceID: 300, UserID: 3},
		&model.Task{ID: 3000, UserID: 3, WorkspaceID: 300, Title: "dave's", Status: "notstarted", CreatedAt: now, UpdatedAt: now},
		&model.Task{ID: 1010, UserID: 1, WorkspaceID: 101, Title: "in the fork", Status: "ongoing", CreatedAt: now, UpdatedAt: now.Add(-3 * time.Minute)},
		&model.Task{ID: 1020, UserID: 1, WorkspaceID: 102, Title: "secret", Status: "notstarted", CreatedAt: now, UpdatedAt: now},
		&model.Task{ID: 1030, UserID: 1, WorkspaceID: 103, Title: "archived", Status: "notstarted", CreatedAt: now, UpdatedAt: now},
		&model.Task{ID: 2000, UserID: 2, WorkspaceID: 200, Title: "bob's", Status: "completed", CreatedAt: now, UpdatedAt: now.Add(-2 * time.Minute)},
		&model.Task{ID: 2010, UserID: 2, WorkspaceID: 201, Title: "bob's own", Status: "notstarted", CreatedAt: now, UpdatedAt: now},
		&model.Memory{ID: 31, UserID: 1, WorkspaceID: 100, Name: "memory.md", Content: "alice's notes", CreatedAt: now, UpdatedAt: now},
		&model.Memory{ID: 32, UserID: 1, WorkspaceID: 102, Name: "memory.md", Content: "secret notes", CreatedAt: now, UpdatedAt: now},
		&model.Memory{ID: 33, UserID: 2, WorkspaceID: 200, Name: "memory.md", Content: "bob's notes", CreatedAt: now, UpdatedAt: now},
		// Filed under the fork itself, which reads its parent's: its owner never sees it.
		&model.Memory{ID: 34, UserID: 1, WorkspaceID: 101, Name: "stale.md", Content: "stale", CreatedAt: now, UpdatedAt: now},
	} {
		must(db.Create(row).Error)
	}
	at := now.Add(-time.Hour).Unix()
	for _, tl := range []model.Telemetry{
		{UserID: 1, WorkspaceID: 100, OccurredAt: at, Action: model.ActionIDTaskComplete},
		{UserID: 1, WorkspaceID: 101, OccurredAt: at, Action: model.ActionIDTaskComplete},
		{UserID: 2, WorkspaceID: 200, OccurredAt: at, Action: model.ActionIDTaskComplete},
		{UserID: 2, WorkspaceID: 200, OccurredAt: at, Action: model.ActionIDMessageCreate},
		{UserID: 1, WorkspaceID: 102, OccurredAt: at, Action: model.ActionIDTaskComplete},
		{UserID: 1, WorkspaceID: 103, OccurredAt: at, Action: model.ActionIDTaskComplete},
		{UserID: 2, WorkspaceID: 201, OccurredAt: at, Action: model.ActionIDTaskComplete},
	} {
		must(db.Create(&tl).Error)
	}

	repo := base.New(teamConn{db})
	ids := &teamIDSequence{}
	dir := t.TempDir()
	atts, err := storage.NewNestedPublic(filepath.Join(dir, "artifacts"), "u")
	must(err)
	skillFiles, err := storage.NewNestedPublic(filepath.Join(dir, "skills"), "u")
	must(err)
	crudCtrl := crud.New(crud.Params{IDGen: ids, Repository: repo, Storage: atts, SkillStorage: skillFiles})
	teams := teamctrl.New(teamctrl.Params{Repository: repo, IDGen: ids, BaseURL: "https://rq", MaxInvitesPerDay: 5})
	tv := teamview.New(teamview.Params{Teams: teams, Owner: crudCtrl, Repository: repo})

	// Alice's task with a file, a message and a plan.
	rs, err := crudCtrl.CreateTask(ctx, entity.CreateTaskRequest{UserID: aliceID, Task: entity.Task{
		WorkspaceID: 100, Title: "ship it", Body: "the body", Status: "notstarted", Assignee: "human", CreatedBy: "human",
		Attachments: []entity.Attachment{
			{Filename: "notes.txt", MimeType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte("file bytes"))},
			{Filename: "page.html", MimeType: "text/html", Data: base64.StdEncoding.EncodeToString([]byte("<script>alert(document.cookie)</script>"))},
			{Filename: "pic.svg", MimeType: "image/svg+xml", Data: base64.StdEncoding.EncodeToString([]byte(`<svg onload="alert(1)"/>`))},
		},
	}})
	must(err)
	m := &teamMode{db: db, attachTask: monoflake.ID(rs.Task.ID).String(), attachment: rs.Task.Attachments[0].ID,
		html: rs.Task.Attachments[1].ID, svg: rs.Task.Attachments[2].ID}
	must(db.Create(&model.Message{ID: 41, TaskID: rs.Task.ID, UserID: 1, Sender: "agent", Text: "on it", CreatedAt: now,
		Attachments: []byte(`[{"id":"m1","filename":"log.txt","mimeType":"text/plain","url":"/storage/artifacts/w-1/x/m1"}]`)}).Error)
	must(db.Create(&model.TaskPlan{ID: rs.Task.ID, Content: "1. ship", Version: 1, CreatedAt: now, UpdatedAt: now}).Error)
	// A doc of Alice's Pano about the task, which her teammates never see.
	must(db.Create(&model.PanoDoc{ID: 51, UserID: 1, Title: "private", TTLDays: 30, CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}).Error)
	must(db.Create(&model.PanoDocTask{ID: 52, UserID: 1, DocID: 51, TaskID: rs.Task.ID, WorkspaceID: 100, CreatedAt: now}).Error)

	// deploy is on for both; alice-only in 100's fork, so in 100; hidden only
	// in the unshared 102; off is switched off.
	skills := []struct {
		user string
		ws   int64
		name string
	}{
		{aliceID, 100, "deploy"}, {bobID, 200, "deploy"}, {aliceID, 101, "alice-only"}, {aliceID, 102, "hidden"}, {aliceID, 100, "off"},
	}
	for _, s := range skills {
		_, err := crudCtrl.SaveSkillFile(ctx, entity.SaveSkillFileRequest{WorkspaceID: s.ws, UserID: s.user, Name: s.name, Path: "SKILL.md",
			Content: "---\nname: " + s.name + "\ndescription: " + s.name + " by " + s.user + "\n---\nsteps of " + s.user})
		must(err)
		time.Sleep(10 * time.Millisecond) // Bob's deploy is the later one
	}
	_, err = crudCtrl.SetSkillEnabled(ctx, entity.SetSkillEnabledRequest{UserID: aliceID, Name: "off", Enabled: false})
	must(err)

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", c.Get("X-Test-User"))
		return c.Next()
	})
	h := &handler{router: app.Group("/api/v1"), teams: teams, teamView: tv, crud: crudCtrl, mcpManager: &fakeMCPManager{server: &fakeWorkspaceServer{}}}
	h.registerTeamRoutes()
	h.registerTeamViewRoutes()
	must(h.registerTaskRoutes())
	m.app = app
	return m
}

// get is a GET as user, decoded.
func (m *teamMode) get(t *testing.T, user, path string) (int, map[string]any) {
	t.Helper()
	return call(t, m.app, user, http.MethodGet, path, "")
}

// list is the ids of the objects under key in body, by field.
func list(body map[string]any, key, field string) []string {
	items, _ := body[key].([]any)
	out := []string{}
	for _, it := range items {
		o, _ := it.(map[string]any)
		s, _ := o[field].(string)
		out = append(out, s)
	}
	return out
}

func sameList(got []string, want ...string) bool {
	return strings.Join(got, ",") == strings.Join(want, ",")
}

func TestTeamMode(t *testing.T) {
	for name, db := range teamModeDatabases(t) {
		t.Run(name, func(t *testing.T) {
			m := newTeamMode(t, db)
			t.Run("workspaces", m.workspaces)
			t.Run("tasks", m.tasks)
			t.Run("task", m.task)
			t.Run("attachment", m.attachmentRoute)
			t.Run("removed routes", m.removedRoutes)
			t.Run("clone", m.clone)
			t.Run("skills", m.skills)
			t.Run("unshare and remove", m.unshareAndRemove)
			t.Run("a team's life", m.flow)
		})
	}
}

func (m *teamMode) workspaces(t *testing.T) {
	status, body := m.get(t, bobID, "/teams/"+teamTen+"/workspaces")
	expectStatus(t, "Bob listing the team's workspaces", status, http.StatusOK, body)
	if got := list(body, "workspaces", "id"); !sameList(got, ws202, ws100, ws101, ws200) {
		t.Errorf("Bob sees %v, want 202, then 100, its fork and 200 by shared name: never the unshared 102 or the archived 103", got)
	}
	if got := list(body, "workspaces", "sharedName"); !sameList(got, "api", "backend", "backend", "Backend ") {
		t.Errorf("the shared names are %v: a fork's is its parent's", got)
	}
	ws, _ := body["workspaces"].([]any)
	if len(ws) == 4 {
		fork, _ := ws[2].(map[string]any)
		owner, _ := fork["owner"].(map[string]any)
		if fork["forkOfId"] != ws100 || fork["name"] != "backend fork" || fork["group"] != nil || owner["name"] != "Alice" || owner["userId"] != aliceID {
			t.Errorf("the fork reads %v", fork)
		}
	}
	status, body = m.get(t, daveID, "/teams/"+teamTen+"/workspaces")
	expectStatus(t, "Dave, outside, listing", status, http.StatusNotFound, body)
	status, body = m.get(t, bobID, "/teams/!/workspaces")
	expectStatus(t, "a team id that is not one", status, http.StatusBadRequest, body)
}

func (m *teamMode) tasks(t *testing.T) {
	base := "/teams/" + teamTen + "/tasks"
	status, body := m.get(t, bobID, base)
	expectStatus(t, "Bob listing the team's tasks", status, http.StatusOK, body)
	if got := list(body, "tasks", "id"); !sameList(got, m.attachTask, task2000, task1010) {
		t.Errorf("Bob lists %v, newest activity first, with nothing from 102, 103 or 201", got)
	}
	if ts, _ := body["tasks"].([]any); len(ts) > 0 {
		first, _ := ts[0].(map[string]any)
		owner, _ := first["owner"].(map[string]any)
		if owner["name"] != "Alice" || first["sharedName"] != "backend" || first["group"] != nil || first["title"] != "ship it" {
			t.Errorf("the first task reads %v", first)
		}
		if got, want := fileLinks(first), m.teamLinks(); got != want {
			t.Errorf("the listed task links its files to %q, want the team's route %q", got, want)
		}
	}
	for q, want := range map[string][]string{
		"?status=completed":                 {task2000},
		"?status=ongoing,notstarted":        {m.attachTask, task1010},
		"?member=" + bobID:                  {task2000},
		"?workspace=%20BACKEND":             {m.attachTask, task2000, task1010},
		"?workspace=api":                    {},
		"?workspace=frontend":               {},
		"?group=frontend":                   {m.attachTask, task2000, task1010},
		"?limit=1&offset=1":                 {task2000},
		"?limit=0&offset=-4":                {m.attachTask, task2000, task1010},
		"?member=" + daveID:                 {},
		"?member=" + aliceID + "&limit=100": {m.attachTask, task1010},
	} {
		status, body := m.get(t, bobID, base+q)
		if got := list(body, "tasks", "id"); status != http.StatusOK || !sameList(got, want...) {
			t.Errorf("%s answered %d %v, want %v", q, status, got, want)
		}
	}
	status, body = m.get(t, bobID, base+"?member=!")
	expectStatus(t, "a member id that is not one", status, http.StatusBadRequest, body)
	status, body = m.get(t, daveID, base)
	expectStatus(t, "Dave, outside, listing tasks", status, http.StatusNotFound, body)
}

func (m *teamMode) task(t *testing.T) {
	path := "/teams/" + teamTen + "/tasks/"
	status, body := m.get(t, bobID, path+m.attachTask)
	expectStatus(t, "Bob reading Alice's task", status, http.StatusOK, body)
	plan, _ := body["plan"].(map[string]any)
	owner, _ := body["owner"].(map[string]any)
	ws, _ := body["workspace"].(map[string]any)
	task, _ := body["task"].(map[string]any)
	if task["title"] != "ship it" || plan["content"] != "1. ship" || owner["name"] != "Alice" || ws["id"] != ws100 || ws["sharedName"] != "backend" {
		t.Errorf("Alice's task reads %v", body)
	}
	if msgs, _ := task["messages"].([]any); len(msgs) != 1 {
		t.Errorf("the task's messages are %v", task["messages"])
	}
	want := m.teamLinks()
	if got := fileLinks(task); got != want {
		t.Errorf("the task links its files to %q, want the team's route %q", got, want)
	}
	// The link is one the reader can follow.
	link := strings.Fields(want)[0]
	if status, _, b := m.raw(t, bobID, strings.TrimPrefix(link, "/api/v1")); status != http.StatusOK || b != "file bytes" {
		t.Errorf("following %s answered %d %q", link, status, b)
	}
	if docs, ok := body["panoDocs"].([]any); ok && len(docs) > 0 {
		t.Errorf("a teammate sees the owner's Pano docs %v", docs)
	}

	status, body = m.get(t, aliceID, path+task1010)
	if status != http.StatusOK || body["plan"] != nil {
		t.Errorf("a task in the fork, which follows its parent's share, answered %d %v with no plan", status, body)
	}
	status, body = m.get(t, aliceID, path+task2000)
	expectStatus(t, "Alice reading Bob's shared task", status, http.StatusOK, body)
	for what, id := range map[string]string{
		"a task in an unshared workspace":       task1020,
		"a task in an archived workspace":       task1030,
		"Bob's task in his unshared workspace":  task2010,
		"a task that does not exist":            monoflake.ID(999).String(),
		"a task the team cannot see, by its id": monoflake.ID(5).String(),
		"a task shared with another team":       monoflake.ID(3000).String(),
	} {
		status, body := m.get(t, aliceID, path+id)
		expectStatus(t, what, status, http.StatusNotFound, body)
	}
	status, body = m.get(t, daveID, path+m.attachTask)
	expectStatus(t, "Dave, outside, reading a task", status, http.StatusNotFound, body)
	status, body = m.get(t, bobID, path+"!")
	expectStatus(t, "a task id that is not one", status, http.StatusBadRequest, body)
}

// teamLinks is team 10's links to the files of Alice's task, its own then
// its message's.
func (m *teamMode) teamLinks() string {
	base := "/api/v1/teams/" + teamTen + "/tasks/" + m.attachTask + "/attachments/"
	return base + m.attachment + " " + base + m.html + " " + base + m.svg + " " + base + "m1"
}

// fileLinks is the url of every file of task, its own then its messages'.
func fileLinks(task map[string]any) string {
	var out []string
	add := func(list any) {
		items, _ := list.([]any)
		for _, it := range items {
			a, _ := it.(map[string]any)
			u, _ := a["url"].(string)
			out = append(out, u)
		}
	}
	add(task["attachments"])
	msgs, _ := task["messages"].([]any)
	for _, it := range msgs {
		msg, _ := it.(map[string]any)
		add(msg["attachments"])
	}
	return strings.Join(out, " ")
}

// raw is a GET as user, undecoded.
func (m *teamMode) raw(t *testing.T, user, path string) (int, http.Header, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1"+path, nil)
	req.Header.Set("X-Test-User", user)
	resp, err := m.app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, string(b)
}

func (m *teamMode) attachmentRoute(t *testing.T) {
	path := "/teams/" + teamTen + "/tasks/" + m.attachTask + "/attachments/" + m.attachment
	status, hdr, body := m.raw(t, bobID, path)
	if status != http.StatusOK || body != "file bytes" || hdr.Get("Content-Type") != "text/plain" || !strings.Contains(hdr.Get("Content-Disposition"), "notes.txt") {
		t.Errorf("Bob reading Alice's file answered %d %v %q", status, hdr, body)
	}
	// A file a browser would run is served as one it cannot, on the team's
	// route and the owner's alike, and a plain one is sandboxed too.
	for _, f := range []struct{ user, path, disposition string }{
		{bobID, path, "inline"},
		{bobID, "/teams/" + teamTen + "/tasks/" + m.attachTask + "/attachments/" + m.html, "attachment"},
		{bobID, "/teams/" + teamTen + "/tasks/" + m.attachTask + "/attachments/" + m.svg, "attachment"},
		{aliceID, "/workspaces/" + ws100 + "/tasks/" + m.attachTask + "/attachments/" + m.attachment, "inline"},
		{aliceID, "/workspaces/" + ws100 + "/tasks/" + m.attachTask + "/attachments/" + m.html, "attachment"},
		{aliceID, "/workspaces/" + ws100 + "/tasks/" + m.attachTask + "/attachments/" + m.svg, "attachment"},
	} {
		status, hdr, _ := m.raw(t, f.user, f.path)
		ct := map[string]string{"inline": "text/plain", "attachment": "application/octet-stream"}[f.disposition]
		if status != http.StatusOK || hdr.Get("Content-Type") != ct || hdr.Get("X-Content-Type-Options") != "nosniff" ||
			!strings.HasSuffix(hdr.Get("Content-Security-Policy"), "; sandbox") || !strings.HasPrefix(hdr.Get("Content-Disposition"), f.disposition+"; filename=") {
			t.Errorf("%s answered %d %v, want %s as %s, nosniff, sandboxed", f.path, status, hdr, f.disposition, ct)
		}
	}
	status, _, _ = m.raw(t, daveID, path)
	if status != http.StatusNotFound {
		t.Errorf("Dave, outside, reading the file answered %d", status)
	}
	status, _, _ = m.raw(t, bobID, "/teams/"+teamTen+"/tasks/"+task1020+"/attachments/"+m.attachment)
	if status != http.StatusNotFound {
		t.Errorf("the file by way of an unshared task answered %d", status)
	}
	status, _, _ = m.raw(t, bobID, "/teams/"+teamTen+"/tasks/"+m.attachTask+"/attachments/nope")
	if status != http.StatusNotFound {
		t.Errorf("a file that is not there answered %d", status)
	}
	status, _, _ = m.raw(t, bobID, "/teams/!/tasks/"+m.attachTask+"/attachments/"+m.attachment)
	if status != http.StatusBadRequest {
		t.Errorf("a team id that is not one answered %d", status)
	}
}

// Team analytics, the team's memory and the members' memory index are
// gone: their routes answer as any unknown route does.
func (m *teamMode) removedRoutes(t *testing.T) {
	for _, r := range []struct{ method, path string }{
		{http.MethodGet, "/analytics"}, {http.MethodGet, "/memory"}, {http.MethodPut, "/memory"},
		{http.MethodGet, "/memory/index"}, {http.MethodGet, "/workspaces/" + ws100 + "/memory/memory.md"},
	} {
		status, body := call(t, m.app, bobID, r.method, "/teams/"+teamTen+r.path, "")
		if status != http.StatusNotFound && status != http.StatusMethodNotAllowed {
			t.Errorf("%s %s answered %d %v, want it gone", r.method, r.path, status, body)
		}
	}
}

func (m *teamMode) clone(t *testing.T) {
	path := "/teams/" + teamTen + "/tasks/" + m.attachTask + "/clone"
	status, body := call(t, m.app, bobID, http.MethodPost, path, `{"workspaceId":"`+ws201+`","assignee":"agent","status":"ongoing"}`)
	expectStatus(t, "Bob cloning Alice's task into his workspace, for his agent", status, http.StatusCreated, body)
	created, _ := body["task"].(map[string]any)
	id, _ := created["id"].(string)
	var clone model.Task
	if err := m.db.First(&clone, monoflake.IDFromBase62(id).Int64()).Error; err != nil {
		t.Fatalf("the clone %v: %v", body, err)
	}
	note := "the body\n\n---\nCloned from [core / Alice / backend](/workspaces/" + ws100 + "/tasks/" + m.attachTask + "?team=" + teamTen + ")"
	if clone.WorkspaceID != 201 || clone.UserID != 2 || clone.Status != "notstarted" || clone.Title != "ship it" || clone.Body != note || clone.Assignee != "agent" {
		t.Errorf("the clone is %+v: want it not started, whatever status was sent", clone)
	}
	if got := string(clone.Attachments); !strings.Contains(got, "notes.txt") || !strings.Contains(got, "page.html") ||
		strings.Contains(got, "text/html") || strings.Contains(got, "image/svg") {
		t.Errorf("the clone's files are %s: want all three, none typed as one a browser runs", got)
	}

	status, body = call(t, m.app, bobID, http.MethodPost, "/teams/"+teamTen+"/tasks/"+task2000+"/clone", `{"workspaceId":"`+ws201+`"}`)
	if created, _ := body["task"].(map[string]any); status != http.StatusCreated || created["status"] != "notstarted" || created["assignee"] != "human" {
		t.Errorf("a clone with no assignee answered %d %v, want it a human's, not started", status, body)
	}
	status, body = call(t, m.app, bobID, http.MethodPost, path, `{"workspaceId":"`+ws100+`"}`)
	expectStatus(t, "Bob cloning into Alice's workspace", status, http.StatusNotFound, body)
	status, body = call(t, m.app, bobID, http.MethodPost, path, `{"workspaceId":"`+ws201+`","assignee":"robot"}`)
	if status != http.StatusUnprocessableEntity || errorMessage(body) != `a clone is for a human or an agent, not "robot"` {
		t.Errorf("a clone for a robot answered %d %v", status, body)
	}
	status, body = call(t, m.app, bobID, http.MethodPost, "/teams/"+teamTen+"/tasks/"+task1020+"/clone", `{"workspaceId":"`+ws201+`"}`)
	expectStatus(t, "cloning an unshared task", status, http.StatusNotFound, body)
	status, body = call(t, m.app, daveID, http.MethodPost, path, `{"workspaceId":"`+ws201+`"}`)
	expectStatus(t, "Dave, outside, cloning", status, http.StatusNotFound, body)
	status, body = call(t, m.app, bobID, http.MethodPost, path, `{"workspaceId":"!"}`)
	expectStatus(t, "a workspace id that is not one", status, http.StatusBadRequest, body)
	status, body = call(t, m.app, bobID, http.MethodPost, path, `{`)
	expectStatus(t, "a body that is not JSON", status, http.StatusBadRequest, body)
}

func (m *teamMode) skills(t *testing.T) {
	status, body := m.get(t, aliceID, "/teams/"+teamTen+"/skills")
	expectStatus(t, "listing the team's skills", status, http.StatusOK, body)
	if got := list(body, "skills", "name"); !sameList(got, "alice-only", "deploy") {
		t.Errorf("the team's skills are %v: none hidden in 102 or switched off", got)
	}
	status, body = m.get(t, aliceID, "/teams/"+teamTen+"/skills/deploy")
	skill, _ := body["skill"].(map[string]any)
	owners, _ := skill["owners"].([]any)
	if status != http.StatusOK || !strings.Contains(fmt.Sprint(skill["content"]), "steps of "+bobID) || len(owners) != 2 {
		t.Errorf("deploy, read from Bob's later copy, answered %d %v", status, body)
	}
	for what, name := range map[string]string{"hidden in 102": "hidden", "switched off": "off", "not there": "none", "not a name": "Not A Name"} {
		status, body := m.get(t, bobID, "/teams/"+teamTen+"/skills/"+pathEscape(name))
		expectStatus(t, "a skill "+what, status, http.StatusNotFound, body)
	}
	status, body = m.get(t, daveID, "/teams/"+teamTen+"/skills")
	expectStatus(t, "Dave, outside, listing skills", status, http.StatusNotFound, body)

	add := func(user, name, body string) (int, map[string]any) {
		return call(t, m.app, user, http.MethodPost, "/teams/"+teamTen+"/skills/"+name+"/add", body)
	}
	status, body = add(bobID, "alice-only", `{"workspaceIds":["`+ws100+`"]}`)
	expectStatus(t, "Bob adding a skill on in Alice's workspace", status, http.StatusNotFound, body)
	status, body = add(bobID, "deploy", `{"workspaceIds":["`+ws201+`"]}`)
	expectStatus(t, "Bob adding a skill he already has", status, http.StatusConflict, body)
	status, body = add(bobID, "alice-only", `{"workspaceIds":["!"]}`)
	expectStatus(t, "a workspace id that is not one", status, http.StatusBadRequest, body)
	status, body = add(bobID, "alice-only", `{`)
	expectStatus(t, "a body that is not JSON", status, http.StatusBadRequest, body)
	status, body = add(bobID, "hidden", `{"workspaceIds":["`+ws201+`"]}`)
	expectStatus(t, "adding a skill the team does not see", status, http.StatusNotFound, body)
	status, body = add(daveID, "alice-only", `{"workspaceIds":[]}`)
	expectStatus(t, "Dave, outside, adding", status, http.StatusNotFound, body)

	status, body = add(bobID, "alice-only", `{"workspaceIds":["`+ws201+`"]}`)
	skill, _ = body["skill"].(map[string]any)
	if status != http.StatusOK || skill["name"] != "alice-only" || fmt.Sprint(skill["workspaceIds"]) != "["+ws201+"]" {
		t.Errorf("Bob adding alice-only to 201 answered %d %v", status, body)
	}
	var copied model.Skill
	if err := m.db.Where("user_id = ? AND name = ?", 2, "alice-only").First(&copied).Error; err != nil || copied.Description != "alice-only by "+aliceID {
		t.Errorf("Bob's copy is %+v (%v)", copied, err)
	}
}

func pathEscape(s string) string { return strings.ReplaceAll(s, " ", "%20") }

// An unshared workspace and a removed member stop reading at once.
func (m *teamMode) unshareAndRemove(t *testing.T) {
	_, body := m.get(t, bobID, "/teams/"+teamTen)
	version, _ := body["version"].(float64)
	status, body := call(t, m.app, bobID, http.MethodDelete, fmt.Sprintf("/workspaces/%s/shares/%s?version=%d", ws200, teamTen, int(version)), "")
	if status != http.StatusOK || body["version"] != version+1 {
		t.Fatalf("Bob unsharing 200 answered %d %v", status, body)
	}
	status, body = m.get(t, aliceID, "/teams/"+teamTen+"/tasks/"+task2000)
	expectStatus(t, "a task of a workspace just unshared", status, http.StatusNotFound, body)
	status, body = m.get(t, aliceID, "/teams/"+teamTen+"/tasks")
	if got := list(body, "tasks", "id"); status != http.StatusOK || sameList(got) || strings.Contains(strings.Join(got, ","), task2000) {
		t.Errorf("after the unshare Alice lists %d %v", status, got)
	}

	status, body = call(t, m.app, aliceID, http.MethodDelete, fmt.Sprintf("/teams/%s/members/%s?version=%d", teamTen, bobID, int(version)+1), "")
	if status != http.StatusOK || body["version"] != version+2 {
		t.Fatalf("Alice removing Bob answered %d %v", status, body)
	}
	for _, p := range []string{"/tasks", "/tasks/" + m.attachTask, "/tasks/" + m.attachTask + "/attachments/" + m.attachment,
		"/workspaces", "/skills", "/activities"} {
		status, body := m.get(t, bobID, "/teams/"+teamTen+p)
		expectStatus(t, "Bob, just removed, reading "+p, status, http.StatusNotFound, body)
	}
}

// A team from the start: made, an invitation sent and accepted, a workspace
// shared, every step in the activity, and a change from a version another
// change replaced refused with the version now.
func (m *teamMode) flow(t *testing.T) {
	status, body := call(t, m.app, aliceID, http.MethodPost, "/teams", `{"name":"launch"}`)
	expectStatus(t, "Alice making a team", status, http.StatusCreated, body)
	team, _ := body["id"].(string)
	status, body = call(t, m.app, aliceID, http.MethodPost, "/teams/"+team+"/invitations", `{"email":"dave@example.com","version":1}`)
	expectStatus(t, "Alice inviting Dave", status, http.StatusCreated, body)
	link, _ := body["link"].(string)
	_, token, _ := strings.Cut(link, "#")
	status, body = call(t, m.app, daveID, http.MethodPost, "/invitations/accept", `{"token":"`+token+`"}`)
	if status != http.StatusOK || body["version"] != float64(3) || body["memberCount"] != float64(2) {
		t.Fatalf("Dave accepting answered %d %v", status, body)
	}
	status, body = call(t, m.app, daveID, http.MethodPut, "/workspaces/"+monoflake.ID(300).String()+"/shares/"+team, `{"version":3}`)
	if status != http.StatusOK || body["version"] != float64(4) {
		t.Fatalf("Dave sharing answered %d %v", status, body)
	}
	status, body = m.get(t, aliceID, "/teams/"+team+"/workspaces")
	if got := list(body, "workspaces", "sharedName"); status != http.StatusOK || !sameList(got, "backend") {
		t.Errorf("the new team's workspaces answered %d %v", status, got)
	}
	status, body = m.get(t, aliceID, "/teams/"+team+"/activities")
	if got := list(body, "activities", "action"); status != http.StatusOK || body["version"] != float64(4) ||
		!sameList(got, "workspace.shared", "invitation.accepted", "member.invited", "team.created") {
		t.Errorf("the activity answered %d %v at %v", status, got, body["version"])
	}
	status, body = call(t, m.app, aliceID, http.MethodPatch, "/teams/"+team, `{"name":"liftoff","version":2}`)
	if status != http.StatusConflict || body["conflict"] != true || body["version"] != float64(4) {
		t.Errorf("a rename from version 2 answered %d %v, want 409 at version 4", status, body)
	}
	status, body = call(t, m.app, aliceID, http.MethodPatch, "/teams/"+team, `{"name":"liftoff","version":4}`)
	if status != http.StatusOK || body["name"] != "liftoff" || body["version"] != float64(5) {
		t.Errorf("a rename from version 4 answered %d %v", status, body)
	}
}
