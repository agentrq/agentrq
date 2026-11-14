// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	if err := db.AutoMigrate(&model.User{}, &model.Workspace{}, &model.Team{}, &model.TeamMember{}, &model.TeamInvitation{}, &model.WorkspaceShare{}, &model.TeamManagementActivity{}); err != nil {
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
	if body["name"] != "core" || body["version"] != float64(1) || body["memberCount"] != float64(1) || body["createdAt"] == nil || body["role"] != nil {
		t.Errorf("the new team is %v", body)
	}
	status, body = call(t, app, aliceID, http.MethodPost, "/teams", `{"name":""}`)
	expectStatus(t, "creating a team with no name", status, http.StatusUnprocessableEntity, body)
	status, body = call(t, app, aliceID, http.MethodPost, "/teams", `not json`)
	expectStatus(t, "creating a team from a body that is not JSON", status, http.StatusBadRequest, body)

	status, body = call(t, app, aliceID, http.MethodGet, "/teams", "")
	expectStatus(t, "listing teams", status, http.StatusOK, body)
	if teams, _ := body["teams"].([]any); len(teams) != 1 || teams[0].(map[string]any)["version"] != float64(1) {
		t.Errorf("Alice lists %v, want her one team at version 1", body)
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

	// Renaming: the version it was made from, or 409 with the version now.
	status, body = call(t, app, aliceID, http.MethodPatch, "/teams/"+team, `{"name":"platform","version":1}`)
	if status != http.StatusOK || body["name"] != "platform" || body["version"] != float64(2) {
		t.Errorf("renaming answered %d %v", status, body)
	}
	status, body = call(t, app, aliceID, http.MethodPatch, "/teams/"+team, `{"name":"stale","version":1}`)
	if e, _ := body["error"].(map[string]any); status != http.StatusConflict || body["conflict"] != true || body["version"] != float64(2) ||
		e["code"] != float64(409) || e["message"] != "someone changed this team: reload it and try again" {
		t.Errorf("renaming from a replaced version answered %d %v", status, body)
	}
	status, body = call(t, app, daveID, http.MethodPatch, "/teams/"+team, `{"name":"mine","version":1}`)
	expectStatus(t, "Dave, outside, renaming from a stale version", status, http.StatusNotFound, body)
	for what, b := range map[string]string{"no version": `{"name":"x"}`, "version 0": `{"name":"x","version":0}`, "not JSON": `[`} {
		status, body = call(t, app, aliceID, http.MethodPatch, "/teams/"+team, b)
		expectStatus(t, "renaming with "+what, status, http.StatusBadRequest, body)
	}

	// Invitations: the link comes back once; the cap is 2.
	status, body = call(t, app, aliceID, http.MethodPost, "/teams/"+team+"/invitations", `{"email":"Bob@Example.com","version":2}`)
	expectStatus(t, "inviting Bob", status, http.StatusCreated, body)
	inv, _ := body["invitation"].(map[string]any)
	link, _ := body["link"].(string)
	if inv["email"] != "bob@example.com" || inv["role"] != nil || inv["invitedByName"] != "Alice" || inv["teamId"] != team || body["emailSent"] != false ||
		!strings.HasPrefix(link, "https://rq/invite#") || body["version"] != float64(3) {
		t.Errorf("the invitation answer is %v", body)
	}
	_, bobToken, _ := strings.Cut(link, "#")
	status, body = call(t, app, aliceID, http.MethodPost, "/teams/"+team+"/invitations", `{"email":"dave@example.com","version":3}`)
	expectStatus(t, "inviting Dave", status, http.StatusCreated, body)
	daveInvitation, _ := body["invitation"].(map[string]any)["id"].(string)
	status, body = call(t, app, aliceID, http.MethodPost, "/teams/"+team+"/invitations", `{"email":"eve@example.com","version":4}`)
	expectStatus(t, "a 3rd invitation with a cap of 2", status, http.StatusTooManyRequests, body)
	if msg := errorMessage(body); msg != "this team has reached its limit of 2 invitations per day" {
		t.Errorf("the cap's message is %q", msg)
	}
	status, body = call(t, app, aliceID, http.MethodPost, "/teams/"+team+"/invitations", `{"email":"eve@example.com","version":3}`)
	if status != http.StatusConflict || body["version"] != float64(4) {
		t.Errorf("inviting from a replaced version answered %d %v", status, body)
	}
	status, body = call(t, app, aliceID, http.MethodPost, "/teams/"+team+"/invitations", `{`)
	expectStatus(t, "inviting from a body that is not JSON", status, http.StatusBadRequest, body)
	status, body = call(t, app, aliceID, http.MethodPost, "/teams/"+team+"/invitations", `{"email":"e@example.com"}`)
	expectStatus(t, "inviting with no version", status, http.StatusBadRequest, body)
	status, body = call(t, app, aliceID, http.MethodPost, "/teams/!/invitations", `{"email":"e@example.com","version":4}`)
	expectStatus(t, "inviting to a team id that is not one", status, http.StatusBadRequest, body)
	status, body = call(t, app, daveID, http.MethodPost, "/teams/"+team+"/invitations", `{"email":"e@example.com","version":4}`)
	expectStatus(t, "Dave, outside, inviting", status, http.StatusNotFound, body)
	status, body = call(t, app, aliceID, http.MethodGet, "/teams/"+team+"/invitations", "")
	if invs, _ := body["invitations"].([]any); status != http.StatusOK || len(invs) != 2 || body["version"] != float64(4) {
		t.Errorf("the team's invitations answered %d %v, want 2 at version 4", status, body)
	}

	// Bob previews and accepts by link; Dave accepts from his inbox. Each
	// accept moves the version: the invitee never loaded it.
	status, body = call(t, app, daveID, http.MethodPost, "/invitations/preview", `{"token":"`+bobToken+`"}`)
	if status != http.StatusOK || body["teamName"] != "platform" || body["email"] != "b***@example.com" || body["emailMatches"] != false || body["role"] != nil {
		t.Errorf("Dave previewing Bob's link answered %d %v", status, body)
	}
	status, body = call(t, app, daveID, http.MethodPost, "/invitations/preview", `x`)
	expectStatus(t, "previewing from a body that is not JSON", status, http.StatusBadRequest, body)
	status, body = call(t, app, daveID, http.MethodPost, "/invitations/accept", `{"token":"`+bobToken+`"}`)
	expectStatus(t, "Dave accepting Bob's link", status, http.StatusForbidden, body)
	status, body = call(t, app, bobID, http.MethodPost, "/invitations/accept", `{"token":"`+bobToken+`"}`)
	if status != http.StatusOK || body["id"] != team || body["version"] != float64(5) {
		t.Errorf("Bob accepting answered %d %v", status, body)
	}
	status, body = call(t, app, bobID, http.MethodPost, "/invitations/accept", `{"token":"`+bobToken+`"}`)
	expectStatus(t, "accepting a used link", status, http.StatusGone, body)
	status, body = call(t, app, bobID, http.MethodPost, "/invitations/accept", `x`)
	expectStatus(t, "accepting from a body that is not JSON", status, http.StatusBadRequest, body)
	status, body = call(t, app, daveID, http.MethodGet, "/invitations", "")
	if invs, _ := body["invitations"].([]any); status != http.StatusOK || len(invs) != 1 || invs[0].(map[string]any)["teamName"] != "platform" || body["version"] != nil {
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

	// Members, every one equal.
	status, body = call(t, app, daveID, http.MethodGet, "/teams/"+team+"/members", "")
	members, _ := body["members"].([]any)
	if status != http.StatusOK || len(members) != 3 || members[0].(map[string]any)["userId"] != aliceID || members[0].(map[string]any)["role"] != nil ||
		body["version"] != float64(6) {
		t.Errorf("members answered %d %v, want Alice first of 3, at version 6", status, body)
	}
	status, body = call(t, app, daveID, http.MethodPatch, "/teams/"+team+"/members/"+bobID, `{"role":"admin"}`)
	expectStatus(t, "changing a role, which no longer exists", status, http.StatusMethodNotAllowed, body)

	// Shares.
	status, body = call(t, app, aliceID, http.MethodPut, "/workspaces/"+wsID+"/shares/"+team, `{"version":6}`)
	if status != http.StatusOK || body["group"] != nil || body["teamName"] != "platform" || body["workspaceId"] != wsID || body["version"] != float64(7) {
		t.Errorf("sharing answered %d %v", status, body)
	}
	status, body = call(t, app, aliceID, http.MethodPut, "/workspaces/"+wsID+"/shares/"+team, `{"version":6}`)
	if status != http.StatusConflict || body["version"] != float64(7) {
		t.Errorf("sharing from a replaced version answered %d %v", status, body)
	}
	for what, b := range map[string]string{"no body": "", "not JSON": `{`, "no version": `{}`} {
		status, body = call(t, app, aliceID, http.MethodPut, "/workspaces/"+wsID+"/shares/"+team, b)
		expectStatus(t, "sharing with "+what, status, http.StatusBadRequest, body)
	}
	status, body = call(t, app, bobID, http.MethodPut, "/workspaces/"+wsID+"/shares/"+team, `{"version":7}`)
	expectStatus(t, "Bob sharing Alice's workspace", status, http.StatusNotFound, body)
	status, body = call(t, app, aliceID, http.MethodGet, "/workspaces/"+wsID+"/shares", "")
	if shares, _ := body["shares"].([]any); status != http.StatusOK || len(shares) != 1 || shares[0].(map[string]any)["version"] != nil {
		t.Errorf("listing shares answered %d %v", status, body)
	}
	status, body = call(t, app, aliceID, http.MethodDelete, "/workspaces/"+wsID+"/shares/"+team, "")
	expectStatus(t, "unsharing with no version", status, http.StatusBadRequest, body)
	status, body = call(t, app, aliceID, http.MethodDelete, "/workspaces/"+wsID+"/shares/"+team+"?version=7", "")
	if status != http.StatusOK || body["version"] != float64(8) {
		t.Errorf("unsharing answered %d %v", status, body)
	}

	// Any member removes another; removing, revoking, leaving, deleting.
	status, body = call(t, app, daveID, http.MethodDelete, "/teams/"+team+"/members/"+bobID+"?version=x", "")
	expectStatus(t, "removing with a version that is not one", status, http.StatusBadRequest, body)
	status, body = call(t, app, daveID, http.MethodDelete, "/teams/"+team+"/members/"+bobID+"?version=8", "")
	if status != http.StatusOK || body["version"] != float64(9) {
		t.Errorf("Dave removing Bob answered %d %v", status, body)
	}
	status, body = call(t, app, aliceID, http.MethodDelete, "/teams/"+team+"/invitations/"+daveInvitation+"?version=9", "")
	expectStatus(t, "revoking an accepted invitation", status, http.StatusNotFound, body)
	status, body = call(t, app, aliceID, http.MethodDelete, "/teams/"+team+"/invitations/"+daveInvitation, "")
	expectStatus(t, "revoking with no version", status, http.StatusBadRequest, body)
	status, body = call(t, app, daveID, http.MethodPost, "/teams/"+team+"/leave", "")
	expectStatus(t, "leaving with no version", status, http.StatusBadRequest, body)
	status, body = call(t, app, daveID, http.MethodPost, "/teams/"+team+"/leave", `{"version":8}`)
	expectStatus(t, "leaving from a replaced version", status, http.StatusConflict, body)
	status, body = call(t, app, daveID, http.MethodPost, "/teams/"+team+"/leave", `{"version":9}`)
	expectStatus(t, "Dave leaving", status, http.StatusNoContent, body)
	status, body = call(t, app, daveID, http.MethodDelete, "/teams/"+team+"?version=10", "")
	expectStatus(t, "Dave, gone, deleting the team", status, http.StatusNotFound, body)
	status, body = call(t, app, aliceID, http.MethodDelete, "/teams/"+team, "")
	expectStatus(t, "deleting with no version", status, http.StatusBadRequest, body)
	status, body = call(t, app, aliceID, http.MethodDelete, "/teams/"+team+"?version=9", "")
	expectStatus(t, "deleting from a replaced version", status, http.StatusConflict, body)

	// The activity, newest first, a page at a time.
	status, body = call(t, app, aliceID, http.MethodGet, "/teams/"+team+"/activities", "")
	actions := list(body, "activities", "action")
	if status != http.StatusOK || body["version"] != float64(10) || !sameList(actions, "member.left", "member.removed", "workspace.unshared",
		"workspace.shared", "invitation.accepted", "invitation.accepted", "member.invited", "member.invited", "team.renamed", "team.created") {
		t.Errorf("the activity answered %d %v", status, actions)
	}
	acts, _ := body["activities"].([]any)
	if len(acts) == 10 {
		removed, _ := acts[1].(map[string]any)
		actor, _ := removed["actor"].(map[string]any)
		target, _ := removed["targetUser"].(map[string]any)
		shared, _ := acts[3].(map[string]any)
		renamed, _ := acts[8].(map[string]any)
		invited, _ := acts[7].(map[string]any)
		if actor["name"] != "Dave" || target["name"] != "Bob" || target["userId"] != bobID ||
			shared["targetWorkspaceId"] != wsID || fmt.Sprint(shared["details"]) != "map[workspaceName:backend]" ||
			fmt.Sprint(renamed["details"]) != "map[from:core to:platform]" || invited["targetEmail"] != "bob@example.com" {
			t.Errorf("the activities read %v", acts)
		}
	}
	status, body = call(t, app, aliceID, http.MethodGet, "/teams/"+team+"/activities?limit=2&before="+list(body, "activities", "id")[1], "")
	if got := list(body, "activities", "action"); status != http.StatusOK || !sameList(got, "workspace.unshared", "workspace.shared") {
		t.Errorf("the page after the 2nd answered %d %v", status, got)
	}
	status, body = call(t, app, aliceID, http.MethodGet, "/teams/"+team+"/activities?before=!", "")
	expectStatus(t, "a page before an id that is not one", status, http.StatusBadRequest, body)
	status, body = call(t, app, bobID, http.MethodGet, "/teams/"+team+"/activities", "")
	expectStatus(t, "Bob, removed, reading the activity", status, http.StatusNotFound, body)

	status, body = call(t, app, aliceID, http.MethodDelete, "/teams/"+team+"?version=10", "")
	expectStatus(t, "Alice deleting her team", status, http.StatusNoContent, body)
}

// A member revokes an invitation and an invitee declines one, each moving
// the version; the last member to remove themself deletes the team.
func TestTeamRevokeDeclineAndTheLastMember(t *testing.T) {
	app, _ := teamServer(t, 5)
	_, body := call(t, app, aliceID, http.MethodPost, "/teams", `{"name":"core"}`)
	team, _ := body["id"].(string)
	_, body = call(t, app, aliceID, http.MethodPost, "/teams/"+team+"/invitations", `{"email":"bob@example.com","version":1}`)
	bobInvitation, _ := body["invitation"].(map[string]any)["id"].(string)
	_, body = call(t, app, aliceID, http.MethodPost, "/teams/"+team+"/invitations", `{"email":"dave@example.com","version":2}`)
	daveInvitation, _ := body["invitation"].(map[string]any)["id"].(string)

	status, body := call(t, app, aliceID, http.MethodDelete, "/teams/"+team+"/invitations/"+daveInvitation+"?version=3", "")
	if status != http.StatusOK || body["version"] != float64(4) {
		t.Errorf("revoking Dave's invitation answered %d %v", status, body)
	}
	status, body = call(t, app, bobID, http.MethodPost, "/invitations/"+bobInvitation+"/decline", "")
	expectStatus(t, "Bob declining", status, http.StatusNoContent, body)
	_, body = call(t, app, aliceID, http.MethodGet, "/teams/"+team+"/activities", "")
	if got := list(body, "activities", "action"); body["version"] != float64(5) ||
		!sameList(got, "invitation.declined", "invitation.revoked", "member.invited", "member.invited", "team.created") {
		t.Errorf("the activity is %v at %v", got, body["version"])
	}

	status, body = call(t, app, aliceID, http.MethodDelete, "/teams/"+team+"/members/"+aliceID+"?version=5", "")
	expectStatus(t, "Alice, the last member, removing herself", status, http.StatusNoContent, body)
	status, body = call(t, app, aliceID, http.MethodGet, "/teams/"+team, "")
	expectStatus(t, "the team its last member left", status, http.StatusNotFound, body)
}

func TestInvitationsTurnedOffAnswerForbidden(t *testing.T) {
	app, _ := teamServer(t, 0)
	_, body := call(t, app, aliceID, http.MethodPost, "/teams", `{"name":"core"}`)
	status, body := call(t, app, aliceID, http.MethodPost, "/teams/"+body["id"].(string)+"/invitations", `{"email":"bob@example.com","version":1}`)
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

// One account's invitations across its teams answer 429 past its own cap, in
// words that say which cap it is.
func TestThePerUserInvitationCapAnswers429(t *testing.T) {
	_, db := teamServer(t, 10)
	app := teamApp(teamctrl.New(teamctrl.Params{Repository: base.New(teamConn{db}), IDGen: &teamIDSequence{}, BaseURL: "https://rq", MaxInvitesPerDay: 10, MaxInvitesPerUserPerDay: 1}))
	teams := make([]string, 2)
	for i, name := range []string{"core", "ops"} {
		status, body := call(t, app, aliceID, http.MethodPost, "/teams", `{"name":"`+name+`"}`)
		expectStatus(t, "creating "+name, status, http.StatusCreated, body)
		teams[i], _ = body["id"].(string)
	}
	status, body := call(t, app, aliceID, http.MethodPost, "/teams/"+teams[0]+"/invitations", `{"email":"bob@example.com","version":1}`)
	expectStatus(t, "Alice's 1st invitation", status, http.StatusCreated, body)
	status, body = call(t, app, aliceID, http.MethodPost, "/teams/"+teams[1]+"/invitations", `{"email":"dave@example.com","version":1}`)
	expectStatus(t, "Alice's 2nd, to another team, with a cap of 1", status, http.StatusTooManyRequests, body)
	if msg := errorMessage(body); msg != "you have reached your limit of 1 invitations per day across all your teams" {
		t.Errorf("the cap's message is %q", msg)
	}
}
