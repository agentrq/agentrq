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

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/githubapp"
	"github.com/agentrq/agentrq/backend/internal/service/skillimport"
)

// fakeGitHubApp is a configured GitHub App: a state is "state-<user>", and
// tokens are minted per installation as tokens says.
type fakeGitHubApp struct {
	off           bool
	installations []githubapp.Installation
	listErr       error
	stateErr      error
	urlErr        error
	tokens        map[int64]error
	minted        []string
}

func (f *fakeGitHubApp) Enabled() bool { return !f.off }
func (f *fakeGitHubApp) InstallURL(_ context.Context, state string) (string, error) {
	return "https://github.com/apps/x/installations/new?state=" + state, f.urlErr
}
func (f *fakeGitHubApp) NewState(_ context.Context, userID int64) (string, error) {
	return fmt.Sprintf("state-%d", userID), f.stateErr
}
func (f *fakeGitHubApp) CheckState(_ context.Context, state string, userID int64) error {
	if state != fmt.Sprintf("state-%d", userID) {
		return githubapp.ErrInvalidState
	}
	return nil
}
func (f *fakeGitHubApp) UserInstallations(_ context.Context, code string) ([]githubapp.Installation, error) {
	return f.installations, f.listErr
}
func (f *fakeGitHubApp) RepoToken(_ context.Context, id int64, repo string) (string, error) {
	f.minted = append(f.minted, fmt.Sprintf("%d/%s", id, repo))
	if err := f.tokens[id]; err != nil {
		return "", err
	}
	return fmt.Sprintf("tok-%d-%s", id, repo), nil
}

func githubEnv(t *testing.T) (*skillEnv, *fakeGitHubApp) {
	e := newSkillEnv(t)
	app := &fakeGitHubApp{tokens: map[int64]error{}}
	e.c.githubApp = app
	return e, app
}

func (e *skillEnv) connected(t *testing.T) string {
	t.Helper()
	rs, err := e.c.GetGitHubConnection(e.ctx, entity.GetGitHubConnectionRequest{UserID: skUserStr})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, in := range rs.Installations {
		out = append(out, fmt.Sprintf("%s:%d", in.Account, in.InstallationID))
	}
	return strings.Join(out, " ")
}

func TestGitHubConnection(t *testing.T) {
	e, app := githubEnv(t)
	app.installations = []githubapp.Installation{{ID: 9, Account: "acme", AccountType: "Organization"}, {ID: 10, Account: "me", AccountType: "User"}}

	rs, err := e.c.GetGitHubConnection(e.ctx, entity.GetGitHubConnectionRequest{UserID: skUserStr})
	if err != nil || !rs.Enabled || rs.InstallURL != fmt.Sprintf("https://github.com/apps/x/installations/new?state=state-%d", skUser) || len(rs.Installations) != 0 {
		t.Fatalf("before connecting: %+v, %v", rs, err)
	}

	// A connect started for another account is refused.
	_, err = e.c.ConnectGitHub(e.ctx, entity.ConnectGitHubRequest{UserID: skUserStr, Code: "c", State: fmt.Sprintf("state-%d", skOther)})
	var se *SkillError
	if !errors.As(err, &se) || se.Kind != SkillInvalid || e.connected(t) != "" {
		t.Fatalf("foreign state: %v", err)
	}

	rs, err = e.c.ConnectGitHub(e.ctx, entity.ConnectGitHubRequest{UserID: skUserStr, Code: "c", State: fmt.Sprintf("state-%d", skUser)})
	if err != nil || len(rs.Installations) != 2 || rs.Installations[0].AccountType != "Organization" {
		t.Fatalf("connect: %+v, %v", rs, err)
	}
	if got := e.connected(t); got != "acme:9 me:10" {
		t.Fatalf("connected %q", got)
	}

	// Connecting again takes what GitHub lists now.
	app.installations = app.installations[1:]
	if _, err := e.c.ConnectGitHub(e.ctx, entity.ConnectGitHubRequest{UserID: skUserStr, Code: "c", State: fmt.Sprintf("state-%d", skUser)}); err != nil || e.connected(t) != "me:10" {
		t.Fatalf("reconnect: %v %q", err, e.connected(t))
	}

	if err := e.c.DisconnectGitHub(e.ctx, entity.DisconnectGitHubRequest{UserID: skUserStr, InstallationID: 9}); !errors.Is(err, base.ErrNotFound) {
		t.Fatalf("disconnect of one not connected: %v", err)
	}
	if err := e.c.DisconnectGitHub(e.ctx, entity.DisconnectGitHubRequest{UserID: skUserStr, InstallationID: 10}); err != nil || e.connected(t) != "" {
		t.Fatalf("disconnect: %v", err)
	}
}

func TestGitHubConnection_Failures(t *testing.T) {
	ok := entity.ConnectGitHubRequest{UserID: skUserStr, Code: "c", State: fmt.Sprintf("state-%d", skUser)}

	e, app := githubEnv(t)
	app.listErr = errors.New("GitHub answered 500")
	var se *SkillError
	if _, err := e.c.ConnectGitHub(e.ctx, ok); !errors.As(err, &se) || se.Kind != SkillUpstream {
		t.Fatalf("GitHub failing: %v", err)
	}

	app.listErr, app.stateErr = nil, errors.New("no state")
	if _, err := e.c.GetGitHubConnection(e.ctx, entity.GetGitHubConnectionRequest{UserID: skUserStr}); err == nil {
		t.Fatal("state failing")
	}
	app.stateErr, app.urlErr = nil, errors.New("no url")
	if _, err := e.c.GetGitHubConnection(e.ctx, entity.GetGitHubConnectionRequest{UserID: skUserStr}); err == nil {
		t.Fatal("url failing")
	}
	app.urlErr = nil

	for _, bad := range []string{"", "!"} {
		if _, err := e.c.GetGitHubConnection(e.ctx, entity.GetGitHubConnectionRequest{UserID: bad}); err == nil {
			t.Errorf("get as %q", bad)
		}
		if _, err := e.c.ConnectGitHub(e.ctx, entity.ConnectGitHubRequest{UserID: bad}); err == nil {
			t.Errorf("connect as %q", bad)
		}
		if err := e.c.DisconnectGitHub(e.ctx, entity.DisconnectGitHubRequest{UserID: bad}); err == nil {
			t.Errorf("disconnect as %q", bad)
		}
	}

	// Off, or never given, the server offers nothing and refuses a connect.
	for _, off := range []githubapp.Service{&fakeGitHubApp{off: true}, nil} {
		e.c.githubApp = off
		rs, err := e.c.GetGitHubConnection(e.ctx, entity.GetGitHubConnectionRequest{UserID: skUserStr})
		if err != nil || rs.Enabled || rs.InstallURL != "" || rs.Installations == nil {
			t.Fatalf("off: %+v, %v", rs, err)
		}
		if _, err := e.c.ConnectGitHub(e.ctx, ok); !errors.As(err, &se) || !strings.Contains(se.Message, "not configured") {
			t.Fatalf("connect while off: %v", err)
		}
	}

	e.c.githubApp = app
	if _, err := e.c.ConnectGitHub(e.ctx, ok); err != nil {
		t.Fatal(err)
	}
	e.db.Exec("DROP TABLE github_installations")
	if _, err := e.c.ConnectGitHub(e.ctx, ok); err == nil {
		t.Fatal("store failing")
	}
	if _, err := e.c.GetGitHubConnection(e.ctx, entity.GetGitHubConnectionRequest{UserID: skUserStr}); err == nil {
		t.Fatal("list failing")
	}
}

func TestImportSkills_PrivateRepositoryThroughGitHubApp(t *testing.T) {
	e, app := githubEnv(t)
	e.importer.res = &skillimport.Result{Repo: "Acme/skills", Ref: "main"}
	if err := e.c.repository.ReplaceGitHubInstallations(e.ctx, skUser, []model.GitHubInstallation{
		{ID: 1, UserID: skUser, InstallationID: 9, Account: "acme"},
		{ID: 2, UserID: skUser, InstallationID: 10, Account: "me"},
	}); err != nil {
		t.Fatal(err)
	}
	imp := func(url string) error {
		_, err := e.c.ImportSkills(e.ctx, entity.ImportSkillsRequest{UserID: skUserStr, URL: url})
		return err
	}

	// The owner's installation reads the repository, matched whatever its case.
	if err := imp("https://github.com/Acme/skills"); err != nil || e.importer.token != "tok-9-skills" {
		t.Fatalf("import: %v, token %q", err, e.importer.token)
	}
	// No installation on the owner, or a link that is no link: anonymous.
	for _, url := range []string{"https://github.com/obra/superpowers", "not a link"} {
		if err := imp(url); err != nil || e.importer.token != "" {
			t.Fatalf("%s: %v, token %q", url, err, e.importer.token)
		}
	}
	// A repository the installation was not given is read anonymously too.
	app.tokens[9] = githubapp.ErrRepoNotCovered
	if err := imp("https://github.com/acme/other"); err != nil || e.importer.token != "" {
		t.Fatalf("not covered: %v, %q", err, e.importer.token)
	}
	// So is one while GitHub fails to mint.
	app.tokens[9] = errors.New("GitHub answered 500")
	if err := imp("https://github.com/acme/other"); err != nil || e.importer.token != "" {
		t.Fatalf("mint failing: %v, %q", err, e.importer.token)
	}
	// An installation GitHub no longer has is forgotten.
	app.tokens[9] = githubapp.ErrInstallationGone
	if err := imp("https://github.com/acme/skills"); err != nil || e.importer.token != "" || e.connected(t) != "me:10" {
		t.Fatalf("gone: %v, %q, %q", err, e.importer.token, e.connected(t))
	}

	// Not found anonymously, the refusal says how to import a private one.
	e.importer.res, e.importer.err = nil, fmt.Errorf("%w: repository acme/x does not exist or is not public", skillimport.ErrNotFound)
	var se *SkillError
	if err := imp("https://github.com/acme/x"); !errors.As(err, &se) || se.Kind != SkillInvalid || !strings.Contains(se.Message, "choose Connect GitHub beside the import") {
		t.Fatalf("not found: %v", err)
	}
	// Not with a token, which was the way already, or with no app.
	if err := imp("https://github.com/me/x"); !errors.As(err, &se) || strings.Contains(se.Message, "connect GitHub") {
		t.Fatalf("not found with a token: %v", err)
	}
	e.c.githubApp = nil
	if err := imp("https://github.com/acme/x"); !errors.As(err, &se) || strings.Contains(se.Message, "connect GitHub") {
		t.Fatalf("not found with no app: %v", err)
	}

	// The account's installations failing to load leaves the import anonymous.
	e.c.githubApp = app
	e.importer.err = nil
	e.importer.res = &skillimport.Result{Repo: "me/x", Ref: "main"}
	e.db.Exec("DROP TABLE github_installations")
	if err := imp("https://github.com/me/x"); err != nil || e.importer.token != "" {
		t.Fatalf("list failing: %v, %q", err, e.importer.token)
	}
}
