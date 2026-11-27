// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/gitlabapp"
	"github.com/agentrq/agentrq/backend/internal/service/security"
	"github.com/agentrq/agentrq/backend/internal/service/skillimport"
)

const testTokenKey = "0123456789abcdef0123456789abcdef"

// fakeGitLabApp is a configured GitLab application on gitlab.example: a
// state is "state-<user>", and each grant is numbered.
type fakeGitLabApp struct {
	off         bool
	host        string
	stateErr    error
	urlErr      error
	exchangeErr error
	userErr     error
	refreshErr  error
	revokeErr   error
	// onRefresh runs before a refresh answers, as another import might.
	onRefresh func()
	grants    int
	refreshed []string
	revoked   []string
}

func (f *fakeGitLabApp) Enabled() bool { return !f.off }
func (f *fakeGitLabApp) Host() string {
	if f.host == "" {
		return "gitlab.example"
	}
	return f.host
}
func (f *fakeGitLabApp) AuthorizeURL(_ context.Context, state string) (string, error) {
	return "https://gitlab.example/oauth/authorize?state=" + state, f.urlErr
}
func (f *fakeGitLabApp) NewState(_ context.Context, userID int64) (string, error) {
	return fmt.Sprintf("state-%d", userID), f.stateErr
}
func (f *fakeGitLabApp) CheckState(_ context.Context, state string, userID int64) error {
	if state != fmt.Sprintf("state-%d", userID) {
		return gitlabapp.ErrInvalidState
	}
	return nil
}
func (f *fakeGitLabApp) grant(life time.Duration) gitlabapp.Token {
	f.grants++
	return gitlabapp.Token{Access: fmt.Sprintf("a%d", f.grants), Refresh: fmt.Sprintf("r%d", f.grants), Expiry: time.Now().Add(life)}
}
func (f *fakeGitLabApp) Exchange(_ context.Context, code string) (gitlabapp.Token, error) {
	if f.exchangeErr != nil {
		return gitlabapp.Token{}, f.exchangeErr
	}
	return f.grant(2 * time.Hour), nil
}
func (f *fakeGitLabApp) Refresh(_ context.Context, refresh string) (gitlabapp.Token, error) {
	f.refreshed = append(f.refreshed, refresh)
	if f.onRefresh != nil {
		f.onRefresh()
	}
	if f.refreshErr != nil {
		return gitlabapp.Token{}, f.refreshErr
	}
	return f.grant(2 * time.Hour), nil
}
func (f *fakeGitLabApp) Username(_ context.Context, access string) (string, error) {
	return "ann", f.userErr
}
func (f *fakeGitLabApp) Revoke(_ context.Context, token string) error {
	f.revoked = append(f.revoked, token)
	return f.revokeErr
}

func gitlabEnv(t *testing.T) (*skillEnv, *fakeGitLabApp) {
	e := newSkillEnv(t)
	app := &fakeGitLabApp{}
	e.c.gitlabApp, e.c.tokenKey = app, testTokenKey
	return e, app
}

func gitlabResult(skills ...skillimport.Skill) *skillimport.Result {
	return &skillimport.Result{Host: "gitlab", Repo: "grp/sub/proj", Ref: "main", Commit: strings.Repeat("b", 40), Skills: skills}
}

func (e *skillEnv) connectGitLab(t *testing.T) *entity.GitLabConnectionResponse {
	t.Helper()
	rs, err := e.c.ConnectGitLab(e.ctx, entity.ConnectGitLabRequest{UserID: skUserStr, Code: "c", State: fmt.Sprintf("state-%d", skUser)})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	return rs
}

func (e *skillEnv) gitlabRow(t *testing.T) model.GitLabConnection {
	t.Helper()
	row, err := e.c.repository.GetGitLabConnection(e.ctx, skUser)
	if err != nil {
		t.Fatalf("connection: %v", err)
	}
	return row
}

func (e *skillEnv) expireGitLab(t *testing.T) {
	t.Helper()
	if err := e.db.Model(&model.GitLabConnection{}).Where("user_id = ?", skUser).Update("expires_at", time.Now().Add(time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
}

func TestGitLabConnection(t *testing.T) {
	e, app := gitlabEnv(t)

	rs, err := e.c.GetGitLabConnection(e.ctx, entity.GetGitLabConnectionRequest{UserID: skUserStr})
	if err != nil || !rs.Enabled || rs.Host != "gitlab.example" || rs.Username != "" || rs.AuthorizeURL != fmt.Sprintf("https://gitlab.example/oauth/authorize?state=state-%d", skUser) {
		t.Fatalf("before connecting: %+v, %v", rs, err)
	}

	rs = e.connectGitLab(t)
	if rs.Username != "ann" {
		t.Fatalf("after connecting: %+v", rs)
	}
	// The tokens are kept encrypted, never as they came.
	row := e.gitlabRow(t)
	if row.AccessToken == "a1" || row.RefreshToken == "r1" || row.Host != "gitlab.example" {
		t.Fatalf("stored: %+v", row)
	}
	if access, err := security.Decrypt(row.AccessToken, testTokenKey, row.AccessNonce); err != nil || access != "a1" {
		t.Fatalf("stored access token: %q, %v", access, err)
	}

	// A connection made to the host the admin has since changed is none.
	app.host = "git.other"
	if rs, _ := e.c.GetGitLabConnection(e.ctx, entity.GetGitLabConnectionRequest{UserID: skUserStr}); rs.Username != "" {
		t.Errorf("another host's connection shows: %+v", rs)
	}
	app.host = ""

	// Disconnecting forgets it, and revokes the refresh token on GitLab.
	if err := e.c.DisconnectGitLab(e.ctx, entity.DisconnectGitLabRequest{UserID: skUserStr}); err != nil {
		t.Fatal(err)
	}
	if len(app.revoked) != 1 || app.revoked[0] != "r1" {
		t.Errorf("revoked %v", app.revoked)
	}
	if err := e.c.DisconnectGitLab(e.ctx, entity.DisconnectGitLabRequest{UserID: skUserStr}); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("a second disconnect: %v", err)
	}

	// A revoke GitLab refuses still disconnects; with connecting off, or on
	// another host, nothing is sent to GitLab.
	for _, setup := range []func(){
		func() { app.revokeErr = errors.New("GitLab answered 500") },
		func() { app.off = true },
		func() { app.off, app.host = false, "git.other" },
	} {
		app.off, app.host, app.revokeErr, app.revoked = false, "", nil, nil
		e.connectGitLab(t)
		setup()
		if err := e.c.DisconnectGitLab(e.ctx, entity.DisconnectGitLabRequest{UserID: skUserStr}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.c.repository.GetGitLabConnection(e.ctx, skUser); !errors.Is(err, base.ErrNotFound) {
			t.Errorf("still connected: %v", err)
		}
	}

	// Off, it names the host alone.
	app.off, app.host = true, ""
	if rs, err := e.c.GetGitLabConnection(e.ctx, entity.GetGitLabConnectionRequest{UserID: skUserStr}); err != nil || rs.Enabled || rs.AuthorizeURL != "" || rs.Host != "gitlab.example" {
		t.Errorf("off: %+v, %v", rs, err)
	}
	e.c.gitlabApp = nil
	if rs, err := e.c.GetGitLabConnection(e.ctx, entity.GetGitLabConnectionRequest{UserID: skUserStr}); err != nil || rs.Host != "gitlab.com" {
		t.Errorf("no app: %+v, %v", rs, err)
	}
}

func TestGitLabConnection_Errors(t *testing.T) {
	e, app := gitlabEnv(t)
	connect := func(state string) error {
		_, err := e.c.ConnectGitLab(e.ctx, entity.ConnectGitLabRequest{UserID: skUserStr, Code: "c", State: state})
		return err
	}
	good := fmt.Sprintf("state-%d", skUser)

	wantSkillErr(t, connect("state-1"), SkillInvalid, "expired or was started by another account")
	app.exchangeErr = errors.New("GitLab refused the authorization code")
	wantSkillErr(t, connect(good), SkillUpstream, "refused the authorization code")
	app.exchangeErr, app.userErr = nil, errors.New("GitLab answered 500 naming who connected")
	wantSkillErr(t, connect(good), SkillUpstream, "naming who connected")
	app.userErr = nil

	e.c.tokenKey = "short"
	if err := connect(good); err == nil {
		t.Error("a connect that cannot be encrypted must fail")
	}
	e.c.tokenKey = testTokenKey

	app.off = true
	wantSkillErr(t, connect(good), SkillInvalid, "not configured")
	app.off = false

	for _, id := range []string{"", "!"} {
		if _, err := e.c.GetGitLabConnection(e.ctx, entity.GetGitLabConnectionRequest{UserID: id}); err == nil {
			t.Errorf("get by %q", id)
		}
		if _, err := e.c.ConnectGitLab(e.ctx, entity.ConnectGitLabRequest{UserID: id}); err == nil {
			t.Errorf("connect by %q", id)
		}
		if err := e.c.DisconnectGitLab(e.ctx, entity.DisconnectGitLabRequest{UserID: id}); err == nil {
			t.Errorf("disconnect by %q", id)
		}
	}

	app.stateErr = errors.New("no state")
	if _, err := e.c.GetGitLabConnection(e.ctx, entity.GetGitLabConnectionRequest{UserID: skUserStr}); err == nil {
		t.Error("a state that cannot be made")
	}
	app.stateErr, app.urlErr = nil, errors.New("no url")
	if _, err := e.c.GetGitLabConnection(e.ctx, entity.GetGitLabConnectionRequest{UserID: skUserStr}); err == nil {
		t.Error("a link that cannot be made")
	}
	app.urlErr = nil

	// A database that fails says so.
	sqlDB, _ := e.db.DB()
	sqlDB.Close()
	if _, err := e.c.GetGitLabConnection(e.ctx, entity.GetGitLabConnectionRequest{UserID: skUserStr}); err == nil {
		t.Error("get on a closed database")
	}
	if err := connect(good); err == nil {
		t.Error("connect on a closed database")
	}
	if err := e.c.DisconnectGitLab(e.ctx, entity.DisconnectGitLabRequest{UserID: skUserStr}); err == nil {
		t.Error("disconnect on a closed database")
	}
	if tok := e.c.gitlabToken(e.ctx, skUser); tok != "" {
		t.Errorf("a token from a closed database: %q", tok)
	}
}

func TestDisconnectGitLab_DeleteFails(t *testing.T) {
	e, _ := gitlabEnv(t)
	e.connectGitLab(t)
	e.db.Exec("CREATE TRIGGER no_delete BEFORE DELETE ON gitlab_connections BEGIN SELECT RAISE(ABORT, 'nope'); END")
	if err := e.c.DisconnectGitLab(e.ctx, entity.DisconnectGitLabRequest{UserID: skUserStr}); err == nil {
		t.Error("a failed delete must be reported")
	}
}

func TestImportSkills_FromGitLab(t *testing.T) {
	e, app := gitlabEnv(t)
	e.importer.res = gitlabResult(importedSkill("tdd", "Theirs."))
	imp := func() *entity.ImportSkillsResponse {
		t.Helper()
		rs, err := e.c.ImportSkills(e.ctx, entity.ImportSkillsRequest{UserID: skUserStr, URL: "https://gitlab.example/grp/sub/proj", WorkspaceIDs: []int64{skWS}, Overwrite: true})
		if err != nil {
			t.Fatalf("import: %v", err)
		}
		return rs
	}

	// Not connected, the import is anonymous.
	rs := imp()
	if len(rs.Imported) != 1 || rs.Imported[0].SourceType != "gitlab" || rs.Imported[0].SourceRepo != "grp/sub/proj" || e.importer.token != "" {
		t.Fatalf("anonymous: %+v, token %q", rs, e.importer.token)
	}
	// A GitLab skill changed here is marked, as a GitHub one is.
	if saved := e.save(t, skWS, "tdd", "notes.md", "ours"); !saved.Skill.LocallyModified {
		t.Error("editing a skill imported from GitLab must mark it locally modified")
	}

	// Connected, it reads with the connection's token, renewing none yet.
	e.connectGitLab(t)
	imp()
	if e.importer.token != "a1" || len(app.refreshed) != 0 {
		t.Fatalf("connected: token %q, refreshed %v", e.importer.token, app.refreshed)
	}

	// Near its end, the token is renewed first and the renewal kept.
	e.expireGitLab(t)
	imp()
	if e.importer.token != "a2" || len(app.refreshed) != 1 || app.refreshed[0] != "r1" {
		t.Fatalf("renewed: token %q, refreshed %v", e.importer.token, app.refreshed)
	}
	imp()
	if e.importer.token != "a2" || len(app.refreshed) != 1 {
		t.Fatalf("after renewal: token %q, refreshed %v", e.importer.token, app.refreshed)
	}

	// A GitHub link never reads with the GitLab connection.
	e.importer.res = githubResult()
	if _, err := e.c.ImportSkills(e.ctx, entity.ImportSkillsRequest{UserID: skUserStr, URL: "https://github.com/obra/superpowers"}); err != nil || e.importer.token != "" {
		t.Fatalf("GitHub import: %v, token %q", err, e.importer.token)
	}
}

func TestGitLabToken_Renewal(t *testing.T) {
	e, app := gitlabEnv(t)
	e.connectGitLab(t)

	// Another import renewed it a moment before: its token is used.
	e.expireGitLab(t)
	app.refreshErr = gitlabapp.ErrRevoked
	app.onRefresh = func() {
		app.onRefresh = nil
		row := e.gitlabRow(t)
		if err := e.c.sealGitLabToken(&row, gitlabapp.Token{Access: "theirs", Refresh: "r-theirs", Expiry: time.Now().Add(time.Hour)}, time.Now()); err != nil {
			t.Fatal(err)
		}
		if err := e.db.Save(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if tok := e.c.gitlabToken(e.ctx, skUser); tok != "theirs" {
		t.Fatalf("raced renewal: %q", tok)
	}

	// A renewal that cannot be stored is still used, this once.
	e.expireGitLab(t)
	app.refreshErr = nil
	e.db.Exec("CREATE TRIGGER no_update BEFORE UPDATE ON gitlab_connections BEGIN SELECT RAISE(ABORT, 'nope'); END")
	if tok := e.c.gitlabToken(e.ctx, skUser); tok != "a2" {
		t.Fatalf("unstored renewal: %q", tok)
	}
	e.db.Exec("DROP TRIGGER no_update")

	// A renewal GitLab fails for a moment leaves the connection.
	e.expireGitLab(t)
	app.refreshErr = errors.New("GitLab answered 503")
	if tok := e.c.gitlabToken(e.ctx, skUser); tok != "" {
		t.Fatalf("failing renewal: %q", tok)
	}
	e.gitlabRow(t)

	// One GitLab refuses for good forgets it.
	app.refreshErr = gitlabapp.ErrRevoked
	if tok := e.c.gitlabToken(e.ctx, skUser); tok != "" {
		t.Fatalf("revoked: %q", tok)
	}
	if _, err := e.c.repository.GetGitLabConnection(e.ctx, skUser); !errors.Is(err, base.ErrNotFound) {
		t.Fatalf("a revoked connection is kept: %v", err)
	}
}

func TestGitLabToken_Unreadable(t *testing.T) {
	e, app := gitlabEnv(t)
	e.connectGitLab(t)

	// Another host's connection, or none at all with connecting off.
	app.host = "git.other"
	if tok := e.c.gitlabToken(e.ctx, skUser); tok != "" {
		t.Errorf("another host: %q", tok)
	}
	app.host, app.off = "", true
	if tok := e.c.gitlabToken(e.ctx, skUser); tok != "" {
		t.Errorf("off: %q", tok)
	}
	app.off = false

	// Tokens that no longer decrypt, under a changed key, read nothing.
	e.c.tokenKey = "fedcba9876543210fedcba9876543210"
	if tok := e.c.gitlabToken(e.ctx, skUser); tok != "" {
		t.Errorf("fresh token under another key: %q", tok)
	}
	e.expireGitLab(t)
	if tok := e.c.gitlabToken(e.ctx, skUser); tok != "" || len(app.refreshed) != 0 {
		t.Errorf("renewal under another key: %q, %v", tok, app.refreshed)
	}

	// A renewal that cannot be encrypted is still used, not stored: the key
	// breaks only once the refresh token was read with it.
	e.c.tokenKey = testTokenKey
	before := e.gitlabRow(t)
	app.onRefresh = func() { e.c.tokenKey = "short" }
	if tok := e.c.gitlabToken(e.ctx, skUser); tok != "a2" {
		t.Errorf("unsealable renewal: %q", tok)
	}
	if after := e.gitlabRow(t); after.RefreshToken != before.RefreshToken {
		t.Error("an unsealable renewal must not be stored")
	}
}

func TestImportSkills_GitLabNotFound(t *testing.T) {
	e, app := gitlabEnv(t)
	e.importer.err = fmt.Errorf("%w: repository grp/proj does not exist or is not public", skillimport.ErrNotFound)
	imp := func() error {
		_, err := e.c.ImportSkills(e.ctx, entity.ImportSkillsRequest{UserID: skUserStr, URL: "https://gitlab.example/grp/proj"})
		return err
	}
	// Not connected, it says how to read a private project.
	wantSkillErr(t, imp(), SkillInvalid, "choose Connect GitLab beside the import")
	// Not when connected, which was the way already, or with connecting off.
	e.connectGitLab(t)
	var se *SkillError
	if err := imp(); !errors.As(err, &se) || strings.Contains(se.Message, "Connect GitLab") {
		t.Fatalf("connected: %v", err)
	}
	app.off = true
	if err := imp(); !errors.As(err, &se) || strings.Contains(se.Message, "Connect GitLab") || strings.Contains(se.Message, "Connect GitHub") {
		t.Fatalf("off: %v", err)
	}
}

func TestImportTeamSkills_FromGitLab(t *testing.T) {
	e := newTeamSkillEnv(t)
	app := &fakeGitLabApp{}
	e.c.gitlabApp, e.c.tokenKey = app, testTokenKey
	if _, err := e.c.ConnectGitLab(e.ctx, entity.ConnectGitLabRequest{UserID: skUserStr, Code: "c", State: fmt.Sprintf("state-%d", skUser)}); err != nil {
		t.Fatal(err)
	}
	e.importer.res = gitlabResult(importedSkill("review", "Review it."))
	rs, err := e.c.ImportTeamSkills(e.ctx, entity.ImportTeamSkillsRequest{TeamSkillsRequest: e.member(skUser), URL: "https://gitlab.example/grp/sub/proj"})
	if err != nil || len(rs.Imported) != 1 || rs.Imported[0].SourceType != skillSourceGitLab || e.importer.token != "a1" {
		t.Fatalf("import: %+v, %v, token %q", rs, err, e.importer.token)
	}
	saved, err := e.agentSave(skWS, skUserStr, "review", "notes.md", "n", nil)
	if err != nil || !saved.Skill.LocallyModified {
		t.Errorf("a change to a GitLab import reads %+v (%v)", saved, err)
	}
}
