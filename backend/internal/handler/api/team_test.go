// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	teamctrl "github.com/agentrq/agentrq/backend/internal/controller/team"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type teamConn struct{ db *gorm.DB }

func (c teamConn) Conn(context.Context) *gorm.DB { return c.db }
func (c teamConn) Close(context.Context)         {}

type teamIDSequence struct{ n int64 }

func (s *teamIDSequence) NextID() int64 { s.n++; return 5000 + s.n }

// teamServer is the REST routes over the real controller and a SQLite
// database, signed in as the account the X-Test-User header names. Accounts
// 1 (Alice), 2 (Bob) and 3 (Dave) exist; Alice owns workspace 100.
func teamServer(t *testing.T, maxInvites int) (*fiber.App, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+filepath.Join(t.TempDir(), "a.db")+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"),
		&gorm.Config{TranslateError: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&model.User{}, &model.Workspace{}, &model.Team{}, &model.TeamMember{}, &model.TeamInvitation{}, &model.WorkspaceShare{}, &model.TeamMemory{}); err != nil {
		t.Fatal(err)
	}
	db.Create(&model.User{ID: 1, Name: "Alice", Email: "alice@example.com"})
	db.Create(&model.User{ID: 2, Name: "Bob", Email: "bob@example.com"})
	db.Create(&model.User{ID: 3, Name: "Dave", Email: "dave@example.com"})
	db.Create(&model.Workspace{ID: 100, UserID: 1, Name: "backend"})
	ctrl := teamctrl.New(teamctrl.Params{Repository: base.New(teamConn{db}), IDGen: &teamIDSequence{}, BaseURL: "https://rq", MaxInvitesPerDay: maxInvites})
	return teamApp(ctrl), db
}

func teamApp(ctrl teamctrl.Controller) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", c.Get("X-Test-User"))
		return c.Next()
	})
	h := &handler{router: app.Group("/api/v1"), teams: ctrl}
	h.registerTeamRoutes()
	return app
}

var (
	aliceID = monoflake.ID(1).String()
	bobID   = monoflake.ID(2).String()
	daveID  = monoflake.ID(3).String()
	wsID    = monoflake.ID(100).String()
)

// call sends a request as user and returns the status and the decoded body.
func call(t *testing.T, app *fiber.App, user, method, path, body string) (int, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, "/api/v1"+path, rd)
	req.Header.Set("X-Test-User", user)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func expectStatus(t *testing.T, what string, got, want int, body map[string]any) {
	t.Helper()
	if got != want {
		t.Errorf("%s answered %d (%v), want %d", what, got, body, want)
	}
}

func errorMessage(body map[string]any) string {
	e, _ := body["error"].(map[string]any)
	s, _ := e["message"].(string)
	return s
}

func TestTeamRoutes(t *testing.T) {
	app, _ := teamServer(t, 2)

	status, body := call(t, app, aliceID, http.MethodPost, "/teams", `{"name":" core "}`)
	expectStatus(t, "creating a team", status, http.StatusCreated, body)
	team, _ := body["id"].(string)
	if body["name"] != "core" || body["role"] != "owner" || body["memberCount"] != float64(1) || body["createdAt"] == nil {
		t.Errorf("the new team is %v", body)
	}
	status, body = call(t, app, aliceID, http.MethodPost, "/teams", `{"name":""}`)
	expectStatus(t, "creating a team with no name", status, http.StatusUnprocessableEntity, body)
	status, body = call(t, app, aliceID, http.MethodPost, "/teams", `not json`)
	expectStatus(t, "creating a team from a body that is not JSON", status, http.StatusBadRequest, body)

	status, body = call(t, app, aliceID, http.MethodGet, "/teams", "")
	expectStatus(t, "listing teams", status, http.StatusOK, body)
	if teams, _ := body["teams"].([]any); len(teams) != 1 {
		t.Errorf("Alice lists %v, want her one team", body)
	}
	status, body = call(t, app, daveID, http.MethodGet, "/teams", "")
	if teams, ok := body["teams"].([]any); status != http.StatusOK || !ok || len(teams) != 0 {
		t.Errorf("Dave lists %d %v, want an empty list, not null", status, body)
	}
	status, body = call(t, app, aliceID, http.MethodGet, "/teams/"+team, "")
	expectStatus(t, "Alice reading her team", status, http.StatusOK, body)
	status, body = call(t, app, daveID, http.MethodGet, "/teams/"+team, "")
	expectStatus(t, "Dave reading a team he is not in", status, http.StatusNotFound, body)
	status, body = call(t, app, aliceID, http.MethodGet, "/teams/!", "")
	expectStatus(t, "reading a team by an id that is not one", status, http.StatusBadRequest, body)
	status, body = call(t, app, aliceID, http.MethodPatch, "/teams/"+team, `{"name":"platform"}`)
	if status != http.StatusOK || body["name"] != "platform" {
		t.Errorf("renaming answered %d %v", status, body)
	}
	status, body = call(t, app, aliceID, http.MethodPatch, "/teams/"+team, `[`)
	expectStatus(t, "renaming from a body that is not JSON", status, http.StatusBadRequest, body)

	// Invitations: the link comes back once; the cap is 2.
	status, body = call(t, app, aliceID, http.MethodPost, "/teams/"+team+"/invitations", `{"email":"Bob@Example.com","role":"admin"}`)
	expectStatus(t, "inviting Bob", status, http.StatusCreated, body)
	inv, _ := body["invitation"].(map[string]any)
	link, _ := body["link"].(string)
	if inv["email"] != "bob@example.com" || inv["role"] != "admin" || inv["invitedByName"] != "Alice" || inv["teamId"] != team || body["emailSent"] != false ||
		!strings.HasPrefix(link, "https://rq/invite#") {
		t.Errorf("the invitation answer is %v", body)
	}
	_, bobToken, _ := strings.Cut(link, "#")
	status, body = call(t, app, aliceID, http.MethodPost, "/teams/"+team+"/invitations", `{"email":"dave@example.com"}`)
	expectStatus(t, "inviting Dave", status, http.StatusCreated, body)
	daveInvitation, _ := body["invitation"].(map[string]any)["id"].(string)
	status, body = call(t, app, aliceID, http.MethodPost, "/teams/"+team+"/invitations", `{"email":"eve@example.com"}`)
	expectStatus(t, "a 3rd invitation with a cap of 2", status, http.StatusTooManyRequests, body)
	if msg := errorMessage(body); msg != "this team has reached its limit of 2 invitations per day" {
		t.Errorf("the cap's message is %q", msg)
	}
	status, body = call(t, app, aliceID, http.MethodPost, "/teams/"+team+"/invitations", `{`)
	expectStatus(t, "inviting from a body that is not JSON", status, http.StatusBadRequest, body)
	status, body = call(t, app, aliceID, http.MethodPost, "/teams/!/invitations", `{"email":"e@example.com"}`)
	expectStatus(t, "inviting to a team id that is not one", status, http.StatusBadRequest, body)
	status, body = call(t, app, daveID, http.MethodPost, "/teams/"+team+"/invitations", `{"email":"e@example.com"}`)
	expectStatus(t, "Dave, outside, inviting", status, http.StatusNotFound, body)
	status, body = call(t, app, aliceID, http.MethodGet, "/teams/"+team+"/invitations", "")
	if invs, _ := body["invitations"].([]any); status != http.StatusOK || len(invs) != 2 {
		t.Errorf("the team's invitations answered %d %v, want 2", status, body)
	}

	// Bob previews and accepts by link; Dave accepts from his inbox.
	status, body = call(t, app, daveID, http.MethodPost, "/invitations/preview", `{"token":"`+bobToken+`"}`)
	if status != http.StatusOK || body["teamName"] != "platform" || body["email"] != "b***@example.com" || body["emailMatches"] != false {
		t.Errorf("Dave previewing Bob's link answered %d %v", status, body)
	}
	status, body = call(t, app, daveID, http.MethodPost, "/invitations/preview", `x`)
	expectStatus(t, "previewing from a body that is not JSON", status, http.StatusBadRequest, body)
	status, body = call(t, app, daveID, http.MethodPost, "/invitations/accept", `{"token":"`+bobToken+`"}`)
	expectStatus(t, "Dave accepting Bob's link", status, http.StatusForbidden, body)
	status, body = call(t, app, bobID, http.MethodPost, "/invitations/accept", `{"token":"`+bobToken+`"}`)
	if status != http.StatusOK || body["role"] != "admin" || body["id"] != team {
		t.Errorf("Bob accepting answered %d %v", status, body)
	}
	status, body = call(t, app, bobID, http.MethodPost, "/invitations/accept", `{"token":"`+bobToken+`"}`)
	expectStatus(t, "accepting a used link", status, http.StatusGone, body)
	status, body = call(t, app, bobID, http.MethodPost, "/invitations/accept", `x`)
	expectStatus(t, "accepting from a body that is not JSON", status, http.StatusBadRequest, body)
	status, body = call(t, app, daveID, http.MethodGet, "/invitations", "")
	if invs, _ := body["invitations"].([]any); status != http.StatusOK || len(invs) != 1 || invs[0].(map[string]any)["teamName"] != "platform" {
		t.Errorf("Dave's inbox answered %d %v", status, body)
	}
	status, body = call(t, app, bobID, http.MethodPost, "/invitations/"+daveInvitation+"/accept", "")
	expectStatus(t, "Bob accepting Dave's invitation", status, http.StatusNotFound, body)
	status, body = call(t, app, daveID, http.MethodPost, "/invitations/"+daveInvitation+"/accept", "")
	expectStatus(t, "Dave accepting from his inbox", status, http.StatusOK, body)
	status, body = call(t, app, daveID, http.MethodPost, "/invitations/"+daveInvitation+"/decline", "")
	expectStatus(t, "declining an accepted invitation", status, http.StatusGone, body)
	status, body = call(t, app, daveID, http.MethodPost, "/invitations/!/decline", "")
	expectStatus(t, "declining an id that is not one", status, http.StatusBadRequest, body)

	// Members.
	status, body = call(t, app, daveID, http.MethodGet, "/teams/"+team+"/members", "")
	if members, _ := body["members"].([]any); status != http.StatusOK || len(members) != 3 || members[0].(map[string]any)["userId"] != aliceID {
		t.Errorf("members answered %d %v, want Alice first of 3", status, body)
	}
	status, body = call(t, app, daveID, http.MethodPatch, "/teams/"+team+"/members/"+bobID, `{"role":"member"}`)
	expectStatus(t, "Dave, a member, changing Bob's role", status, http.StatusForbidden, body)
	status, body = call(t, app, bobID, http.MethodPatch, "/teams/"+team+"/members/"+daveID, `{"role":"admin"}`)
	expectStatus(t, "Bob, an admin, making Dave an admin", status, http.StatusNoContent, body)
	status, body = call(t, app, bobID, http.MethodPatch, "/teams/"+team+"/members/"+daveID, `{`)
	expectStatus(t, "changing a role from a body that is not JSON", status, http.StatusBadRequest, body)
	status, body = call(t, app, bobID, http.MethodPatch, "/teams/"+team+"/members/"+aliceID, `{"role":"member"}`)
	expectStatus(t, "Bob, an admin, demoting Alice, the owner", status, http.StatusForbidden, body)
	status, body = call(t, app, aliceID, http.MethodPost, "/teams/"+team+"/leave", "")
	expectStatus(t, "Alice, the last owner, leaving", status, http.StatusConflict, body)

	// Shares.
	status, body = call(t, app, aliceID, http.MethodPut, "/workspaces/"+wsID+"/shares/"+team, "")
	if status != http.StatusOK || body["group"] != "backend" || body["teamName"] != "platform" || body["workspaceId"] != wsID {
		t.Errorf("sharing with no body answered %d %v", status, body)
	}
	status, body = call(t, app, aliceID, http.MethodPut, "/workspaces/"+wsID+"/shares/"+team, `{"group":"Platform"}`)
	if status != http.StatusOK || body["group"] != "Platform" {
		t.Errorf("sharing with a group answered %d %v", status, body)
	}
	status, body = call(t, app, aliceID, http.MethodPut, "/workspaces/"+wsID+"/shares/"+team, `{`)
	expectStatus(t, "sharing from a body that is not JSON", status, http.StatusBadRequest, body)
	status, body = call(t, app, bobID, http.MethodPut, "/workspaces/"+wsID+"/shares/"+team, "")
	expectStatus(t, "Bob sharing Alice's workspace", status, http.StatusNotFound, body)
	status, body = call(t, app, aliceID, http.MethodGet, "/workspaces/"+wsID+"/shares", "")
	if shares, _ := body["shares"].([]any); status != http.StatusOK || len(shares) != 1 {
		t.Errorf("listing shares answered %d %v", status, body)
	}
	status, body = call(t, app, aliceID, http.MethodDelete, "/workspaces/"+wsID+"/shares/"+team, "")
	expectStatus(t, "unsharing", status, http.StatusNoContent, body)

	// Removing, revoking, leaving, deleting.
	status, body = call(t, app, bobID, http.MethodDelete, "/teams/"+team+"/members/"+daveID, "")
	expectStatus(t, "Bob, an admin, removing Dave, an admin", status, http.StatusNoContent, body)
	status, body = call(t, app, aliceID, http.MethodDelete, "/teams/"+team+"/invitations/"+daveInvitation, "")
	expectStatus(t, "revoking an accepted invitation", status, http.StatusNotFound, body)
	status, body = call(t, app, bobID, http.MethodPost, "/teams/"+team+"/leave", "")
	expectStatus(t, "Bob leaving", status, http.StatusNoContent, body)
	status, body = call(t, app, bobID, http.MethodDelete, "/teams/"+team, "")
	expectStatus(t, "Bob, gone, deleting the team", status, http.StatusNotFound, body)
	status, body = call(t, app, aliceID, http.MethodDelete, "/teams/"+team, "")
	expectStatus(t, "Alice deleting her team", status, http.StatusNoContent, body)
}

func TestInvitationsTurnedOffAnswerForbidden(t *testing.T) {
	app, _ := teamServer(t, 0)
	_, body := call(t, app, aliceID, http.MethodPost, "/teams", `{"name":"core"}`)
	status, body := call(t, app, aliceID, http.MethodPost, "/teams/"+body["id"].(string)+"/invitations", `{"email":"bob@example.com"}`)
	if status != http.StatusForbidden || errorMessage(body) != "invitations are turned off on this server" {
		t.Errorf("inviting with a cap of 0 answered %d %v", status, body)
	}
}

// failingTeams is a controller that cannot reach its database.
type failingTeams struct{ teamctrl.Controller }

func (failingTeams) ListTeams(context.Context, int64) ([]entity.Team, error) {
	return nil, errors.New("database unavailable")
}

func TestATeamFailureAnswersAGenericError(t *testing.T) {
	status, body := call(t, teamApp(failingTeams{}), aliceID, http.MethodGet, "/teams", "")
	if status != http.StatusInternalServerError || errorMessage(body) != "internal server error" {
		t.Errorf("a database failure answered %d %v, want 500 without its cause", status, body)
	}
}
