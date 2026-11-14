// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package team

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/smtp"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// The cast: Alice, Bob and Carol are in the "core" team, which Alice made.
// Every member is equal. Dave and Eve are in no team; Nobody has no account
// at all.
const (
	alice  int64 = 1
	bob    int64 = 2
	carol  int64 = 3
	dave   int64 = 4
	eve    int64 = 5
	nobody int64 = 99
)

var t0 = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

var errDB = errors.New("database unavailable")

type testConn struct{ db *gorm.DB }

func (c testConn) Conn(context.Context) *gorm.DB { return c.db }
func (c testConn) Close(context.Context)         {}

type sequence struct{ n atomic.Int64 }

func (s *sequence) NextID() int64 { return 1000 + s.n.Add(1) }

// mailbox is an SMTP stand-in.
type mailbox struct {
	mu   sync.Mutex
	err  error
	sent []smtp.SendRequest
}

func (m *mailbox) Send(_ context.Context, rq smtp.SendRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.sent = append(m.sent, rq)
	return nil
}

// events records what was published.
type events struct {
	mu   sync.Mutex
	seen []entity.CRUDEvent
}

func (e *events) Publish(_ context.Context, rq pubsub.PublishRequest) (*pubsub.PublishResponse, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.seen = append(e.seen, rq.Event.(entity.CRUDEvent))
	return &pubsub.PublishResponse{}, nil
}

func (e *events) actions() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []string{}
	for _, ev := range e.seen {
		out = append(out, fmt.Sprintf("%s:%d", ev.Action, ev.WorkspaceID))
	}
	return out
}

func openDB(t *testing.T, d gorm.Dialector) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(d, &gorm.Config{TranslateError: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

// databases is a SQLite file opened as the app opens it, and a database of
// its own on the Postgres AGENTRQ_TEST_POSTGRES_DSN names, when it is set.
func databases(t *testing.T) map[string]*gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agentrq.db")
	dbs := map[string]*gorm.DB{"sqlite": openDB(t, sqlite.Open("file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"))}
	if dsn := os.Getenv("AGENTRQ_TEST_POSTGRES_DSN"); dsn != "" {
		admin := openDB(t, postgres.Open(dsn))
		name := fmt.Sprintf("teams_ctrl_%d", time.Now().UnixNano())
		if err := admin.Exec("CREATE DATABASE " + name).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { admin.Exec("DROP DATABASE " + name + " WITH (FORCE)") })
		dbs["postgres"] = openDB(t, postgres.Open(dsn+" dbname="+name))
	}
	return dbs
}

// fixture is the cast's database, the controller over it, and a clock tests
// move.
type fixture struct {
	t      *testing.T
	ctx    context.Context
	db     *gorm.DB
	repo   base.Repository
	c      Controller
	now    time.Time
	mail   *mailbox
	events *events
	team   int64
	ids    *sequence
}

type option func(*Params)

func withMailer(m *mailbox) option { return func(p *Params) { p.Mailer = m } }
func withCap(n int) option         { return func(p *Params) { p.MaxInvitesPerDay = n } }
func withUserCap(n int) option     { return func(p *Params) { p.MaxInvitesPerUserPerDay = n } }

// each runs fn once per database, on a fresh cast.
func each(t *testing.T, fn func(f *fixture), opts ...option) {
	t.Helper()
	for name, db := range databases(t) {
		t.Run(name, func(t *testing.T) {
			fn(newFixture(t, db, opts...))
		})
	}
}

func newFixture(t *testing.T, db *gorm.DB, opts ...option) *fixture {
	t.Helper()
	if err := db.AutoMigrate(&model.User{}, &model.Workspace{}, &model.Team{}, &model.TeamMember{}, &model.TeamInvitation{}, &model.WorkspaceShare{}, &model.TeamManagementActivity{}); err != nil {
		t.Fatal(err)
	}
	for _, u := range []model.User{
		{ID: alice, Name: "Alice", Email: "alice@example.com", Picture: "https://img/alice"},
		{ID: bob, Name: "Bob", Email: "bob@example.com"},
		{ID: carol, Name: "Carol", Email: "Carol@Example.com"},
		{ID: dave, Name: "Dave", Email: "dave@example.com"},
		{ID: eve, Name: "Eve", Email: "eve@example.com"},
	} {
		if err := db.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
	}
	f := &fixture{t: t, ctx: context.Background(), db: db, repo: base.New(testConn{db}), now: t0, events: &events{}, ids: &sequence{}}
	p := Params{
		Repository: f.repo, IDGen: f.ids, PubSub: f.events, BaseURL: "https://agentrq.example/",
		MaxInvitesPerDay: 10, Now: func() time.Time { return f.now },
	}
	for _, o := range opts {
		o(&p)
	}
	if m, ok := p.Mailer.(*mailbox); ok {
		f.mail = m
	}
	f.c = New(p)
	f.team = must(f.c.CreateTeam(f.ctx, alice, "  core  ")).ID
	f.join(bob, "bob@example.com")
	f.join(carol, "carol@example.com")
	return f
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// join invites the account and accepts as it, through the link, or the inbox
// when the invitation was emailed.
func (f *fixture) join(userID int64, email string) {
	f.t.Helper()
	rs, err := f.c.CreateInvitation(f.ctx, f.ch(alice), email)
	if err != nil {
		f.t.Fatalf("invite %s: %v", email, err)
	}
	accept := func() (entity.Team, error) { return f.c.AcceptInvitation(f.ctx, userID, token(rs.Link)) }
	if rs.Link == "" { // emailed: accepted from the inbox
		accept = func() (entity.Team, error) { return f.c.AcceptInvitationByID(f.ctx, userID, rs.Invitation.ID) }
	}
	if _, err := accept(); err != nil {
		f.t.Fatalf("accept as %d: %v", userID, err)
	}
}

// version is the team's version in the database now; 0 when it is gone.
func (f *fixture) version(teamID int64) int64 {
	var t model.Team
	f.db.Where("id = ?", teamID).Limit(1).Find(&t)
	return t.Version
}

// ch is a change to the cast's team by userID, from its version now.
func (f *fixture) ch(userID int64) entity.TeamChange { return f.chOf(userID, f.team) }

// chOf is a change to teamID by userID, from its version now.
func (f *fixture) chOf(userID, teamID int64) entity.TeamChange {
	return entity.TeamChange{UserID: userID, TeamID: teamID, Version: f.version(teamID)}
}

// stale is ch from the version before the one now.
func (f *fixture) stale(userID int64) entity.TeamChange {
	c := f.ch(userID)
	c.Version--
	return c
}

// activities are the team's activities, newest first, as "action actor
// target", the target its user, email or workspace.
func (f *fixture) activities(teamID int64) []string {
	f.t.Helper()
	var rows []model.TeamManagementActivity
	f.db.Where("team_id = ?", teamID).Order("created_at DESC, id DESC").Find(&rows)
	out := []string{}
	for _, r := range rows {
		s := fmt.Sprintf("%s %d", r.Action, r.ActorUserID)
		switch {
		case r.TargetUserID != nil:
			s += fmt.Sprintf(" %d", *r.TargetUserID)
		case r.TargetEmail != nil:
			s += " " + *r.TargetEmail
		case r.TargetWorkspaceID != nil:
			s += fmt.Sprintf(" ws%d", *r.TargetWorkspaceID)
		}
		out = append(out, s)
	}
	return out
}

func token(link string) string {
	_, tok, _ := strings.Cut(link, "#")
	return tok
}

// kind is err's TeamError kind, or -1 when it is not one.
func kind(err error) entity.TeamErrorKind {
	var te *entity.TeamError
	if errors.As(err, &te) {
		return te.Kind
	}
	return -1
}

func expectKind(t *testing.T, what string, err error, want entity.TeamErrorKind) {
	t.Helper()
	if kind(err) != want {
		t.Errorf("%s returned %v, want a team error of kind %d", what, err, want)
	}
}

func expectNotFound(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, base.ErrNotFound) {
		t.Errorf("%s returned %v, want not found", what, err)
	}
}

func expectOK(t *testing.T, what string, err error) {
	t.Helper()
	if err != nil {
		t.Errorf("%s returned %v, want success", what, err)
	}
}

// expectConflict is err a version conflict that names the version now.
func expectConflict(t *testing.T, what string, err error, now int64) {
	t.Helper()
	var ve *entity.TeamVersionError
	if !errors.As(err, &ve) || ve.Version != now {
		t.Errorf("%s returned %v, want a version conflict at %d", what, err, now)
	}
}

func errOf[T any](_ T, err error) error { return err }

func TestTeamsAreSeenOnlyByTheirMembers(t *testing.T) {
	each(t, func(f *fixture) {
		// Made at 1, two invitations and two accepts: 5.
		teams := must(f.c.ListTeams(f.ctx, carol))
		if len(teams) != 1 || teams[0].Name != "core" || teams[0].Version != 5 || teams[0].MemberCount != 3 {
			t.Errorf("Carol's teams are %+v, want core at version 5, of 3", teams)
		}
		if teams := must(f.c.ListTeams(f.ctx, dave)); len(teams) != 0 {
			t.Errorf("Dave, in no team, lists %+v", teams)
		}
		got := must(f.c.GetTeam(f.ctx, bob, f.team))
		if got.Name != "core" || got.Version != 5 || got.MemberCount != 3 || !got.CreatedAt.Equal(t0) {
			t.Errorf("Bob sees the team as %+v", got)
		}
		_, err := f.c.GetTeam(f.ctx, dave, f.team)
		expectNotFound(t, "Dave reading a team he is not in", err)
		_, err = f.c.GetTeam(f.ctx, alice, 424242)
		expectNotFound(t, "reading an unknown team", err)
		_, err = f.c.ListMembers(f.ctx, dave, f.team)
		expectNotFound(t, "Dave listing a team's members", err)
		_, err = f.c.ListTeamInvitations(f.ctx, dave, f.team)
		expectNotFound(t, "Dave listing a team's invitations", err)
		_, err = f.c.ListActivities(f.ctx, entity.ListTeamActivitiesRequest{UserID: dave, TeamID: f.team})
		expectNotFound(t, "Dave listing a team's activity", err)
	})
}

func TestCreatingATeam(t *testing.T) {
	each(t, func(f *fixture) {
		got := must(f.c.CreateTeam(f.ctx, dave, " ops "))
		if got.Name != "ops" || got.Version != 1 || got.MemberCount != 1 || !got.CreatedAt.Equal(t0) {
			t.Errorf("Dave's new team is %+v, want ops at version 1, of 1", got)
		}
		if v := f.version(got.ID); v != 1 {
			t.Errorf("the stored team is at version %d, want 1", v)
		}
		if acts := f.activities(got.ID); strings.Join(acts, ",") != "team.created 4" {
			t.Errorf("the new team's activity is %v, want team.created by Dave", acts)
		}
		for _, name := range []string{"", "   ", strings.Repeat("é", 129), "ops\r\nBcc: x@example.com", "ops\tteam", "ops\x00"} {
			_, err := f.c.CreateTeam(f.ctx, dave, name)
			expectKind(t, fmt.Sprintf("creating a team named %q", name), err, entity.TeamInvalid)
		}
		if _, err := f.c.CreateTeam(f.ctx, dave, strings.Repeat("é", 128)); err != nil {
			t.Errorf("a 128-character name returned %v", err)
		}
		if acts := f.events.actions(); !strings.HasPrefix(strings.Join(acts, " "), "team_create:0") {
			t.Errorf("events are %v, want team_create first, with no workspace", acts)
		}
	})
}

func TestAnyMemberRenames(t *testing.T) {
	each(t, func(f *fixture) {
		got, err := f.c.RenameTeam(f.ctx, f.ch(carol), " platform ")
		if err != nil || got.Name != "platform" || got.Version != 6 || got.MemberCount != 3 || got.ID != f.team {
			t.Errorf("Carol renaming returned %+v, %v; want platform at version 6", got, err)
		}
		expectConflict(t, "Bob renaming from the version before", errOf(f.c.RenameTeam(f.ctx, f.stale(bob), "x")), 6)
		expectNotFound(t, "Dave, outside, renaming from the right version", errOf(f.c.RenameTeam(f.ctx, f.ch(dave), "x")))
		expectNotFound(t, "Dave, outside, renaming from a stale one", errOf(f.c.RenameTeam(f.ctx, f.stale(dave), "x")))
		expectKind(t, "renaming to nothing", errOf(f.c.RenameTeam(f.ctx, f.ch(alice), " ")), entity.TeamInvalid)
		if v := f.version(f.team); v != 6 {
			t.Errorf("after refused renames the team is at %d, want 6", v)
		}
		as := must(f.c.ListActivities(f.ctx, entity.ListTeamActivitiesRequest{UserID: alice, TeamID: f.team, Limit: 1}))
		if len(as.Activities) != 1 || as.Version != 6 {
			t.Fatalf("the latest activity is %+v", as)
		}
		a := as.Activities[0]
		if a.Action != model.TeamActivityTeamRenamed || a.Actor.UserID != carol || a.Actor.Name != "Carol" ||
			a.Details["from"] != "core" || a.Details["to"] != "platform" || a.Target.UserID != 0 || a.TargetEmail != "" || a.TargetWorkspaceID != 0 {
			t.Errorf("the rename is recorded as %+v", a)
		}
	})
}

// Changes made at once from one version: exactly one lands, and every
// other is told the version it landed at.
func TestConcurrentChangesFromOneVersion(t *testing.T) {
	each(t, func(f *fixture) {
		const writers = 8
		from := f.ch(alice)
		var wg sync.WaitGroup
		errs := make([]error, writers)
		for i := range writers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ch := from
				ch.UserID = []int64{alice, bob, carol}[i%3]
				_, errs[i] = f.c.RenameTeam(f.ctx, ch, fmt.Sprintf("name %d", i))
			}()
		}
		wg.Wait()
		ok := 0
		for _, err := range errs {
			if err == nil {
				ok++
				continue
			}
			expectConflict(t, "a rename that lost", err, from.Version+1)
		}
		if ok != 1 || f.version(f.team) != from.Version+1 {
			t.Errorf("%d renames landed and the team is at %d, want 1 at %d", ok, f.version(f.team), from.Version+1)
		}
	})
}

func TestAnyMemberDeletesTheTeam(t *testing.T) {
	each(t, func(f *fixture) {
		expectNotFound(t, "Dave, outside, deleting", f.c.DeleteTeam(f.ctx, f.ch(dave)))
		expectConflict(t, "Carol deleting from the version before", f.c.DeleteTeam(f.ctx, f.stale(carol)), 5)
		expectOK(t, "Carol deleting", f.c.DeleteTeam(f.ctx, f.ch(carol)))
		expectNotFound(t, "reading the deleted team", errOf(f.c.GetTeam(f.ctx, alice, f.team)))
		if teams := must(f.c.ListTeams(f.ctx, alice)); len(teams) != 0 {
			t.Errorf("Alice still lists %+v after the team was deleted", teams)
		}
		// The record stays.
		if acts := f.activities(f.team); len(acts) != 6 || acts[0] != "team.deleted 3" {
			t.Errorf("the deleted team's activity is %v, want team.deleted by Carol on top of 5", acts)
		}
	})
}

func TestMembersAreListedWithTheirAccounts(t *testing.T) {
	each(t, func(f *fixture) {
		members := must(f.c.ListMembers(f.ctx, carol, f.team))
		got := []string{}
		for _, m := range members.Members {
			got = append(got, fmt.Sprintf("%d:%s:%s:%s", m.UserID, m.Name, m.Email, m.Picture))
		}
		want := "1:Alice:alice@example.com:https://img/alice 2:Bob:bob@example.com: 3:Carol:Carol@Example.com:"
		if strings.Join(got, " ") != want || members.Version != 5 {
			t.Errorf("members are %v at %d, want %s at 5", got, members.Version, want)
		}
		if !members.Members[1].JoinedAt.Equal(t0) {
			t.Errorf("Bob joined at %v, want %v", members.Members[1].JoinedAt, t0)
		}
	})
}

func TestAnyMemberRemovesAMember(t *testing.T) {
	each(t, func(f *fixture) {
		f.join(dave, "dave@example.com")
		expectNotFound(t, "Eve, outside, removing Dave", errOf(f.c.RemoveMember(f.ctx, f.ch(eve), dave)))
		expectNotFound(t, "removing Eve, not in the team", errOf(f.c.RemoveMember(f.ctx, f.ch(alice), eve)))
		expectConflict(t, "Carol removing Dave from the version before", errOf(f.c.RemoveMember(f.ctx, f.stale(carol), dave)), 7)
		v, err := f.c.RemoveMember(f.ctx, f.ch(carol), dave)
		if err != nil || v != 8 {
			t.Errorf("Carol removing Dave returned %d, %v; want version 8", v, err)
		}
		expectNotFound(t, "Dave reading the team he was removed from", errOf(f.c.GetTeam(f.ctx, dave, f.team)))
		expectNotFound(t, "Dave, removed, removing Carol", errOf(f.c.RemoveMember(f.ctx, f.ch(dave), carol)))
		v, err = f.c.RemoveMember(f.ctx, f.ch(carol), carol)
		if err != nil || v != 9 {
			t.Errorf("Carol removing herself returned %d, %v; want version 9", v, err)
		}
		// Bob removes Alice, who made the team.
		expectOK(t, "Bob removing Alice", errOf(f.c.RemoveMember(f.ctx, f.ch(bob), alice)))
		if acts := f.activities(f.team); strings.Join(acts[:3], ",") != "member.removed 2 1,member.left 3,member.removed 3 4" {
			t.Errorf("the activity is %v", acts)
		}
		if must(f.c.GetTeam(f.ctx, bob, f.team)).MemberCount != 1 {
			t.Error("Bob is not alone in the team")
		}
	})
}

// The last member to go deletes the team, recorded after their leaving.
func TestTheLastMemberLeavingDeletesTheTeam(t *testing.T) {
	each(t, func(f *fixture) {
		expectOK(t, "Bob leaving", f.c.LeaveTeam(f.ctx, f.ch(bob)))
		expectOK(t, "Carol leaving", f.c.LeaveTeam(f.ctx, f.ch(carol)))
		expectNotFound(t, "Carol, gone, leaving again", f.c.LeaveTeam(f.ctx, f.ch(carol)))
		expectConflict(t, "Alice leaving from the version before", f.c.LeaveTeam(f.ctx, f.stale(alice)), 7)
		expectOK(t, "Alice, the last, leaving", f.c.LeaveTeam(f.ctx, f.ch(alice)))
		if _, err := f.repo.GetTeam(f.ctx, f.team); !errors.Is(err, base.ErrNotFound) {
			t.Errorf("the team is still there: %v", err)
		}
		acts := f.activities(f.team)
		if len(acts) < 2 || acts[0] != "team.deleted 1" || acts[1] != "member.left 1" {
			t.Errorf("the activity is %v, want Alice leaving, then the team deleted", acts)
		}
		var ids []int64
		f.db.Model(&model.TeamManagementActivity{}).Where("team_id = ?", f.team).Pluck("id", &ids)
		seen := map[int64]bool{}
		for _, id := range ids {
			if seen[id] {
				t.Errorf("activity id %d is used twice", id)
			}
			seen[id] = true
		}
	})
}

func TestLeavingAndRemovalUnshare(t *testing.T) {
	each(t, func(f *fixture) {
		f.db.Create(&model.Workspace{ID: 300, UserID: carol, Name: "carol-ws"})
		f.db.Create(&model.Workspace{ID: 200, UserID: bob, Name: "bob-ws"})
		must(f.c.ShareWorkspace(f.ctx, f.ch(carol), 300))
		must(f.c.ShareWorkspace(f.ctx, f.ch(bob), 200))
		expectNotFound(t, "Dave, outside, leaving", f.c.LeaveTeam(f.ctx, f.ch(dave)))
		expectOK(t, "Carol leaving", f.c.LeaveTeam(f.ctx, f.ch(carol)))
		if shares := must(f.c.ListShares(f.ctx, carol, 300)); len(shares) != 0 {
			t.Errorf("Carol's workspace is still shared after she left: %+v", shares)
		}
		expectOK(t, "Alice removing Bob", errOf(f.c.RemoveMember(f.ctx, f.ch(alice), bob)))
		if shares := must(f.c.ListShares(f.ctx, bob, 200)); len(shares) != 0 {
			t.Errorf("Bob's workspace is still shared after his removal: %+v", shares)
		}
	})
}

func TestAnyMemberInvites(t *testing.T) {
	each(t, func(f *fixture) {
		expectNotFound(t, "Dave, outside, inviting", errOf(f.c.CreateInvitation(f.ctx, f.ch(dave), "x@example.com")))
		// An outsider learns nothing of who is in the team.
		expectNotFound(t, "Dave, outside, inviting Carol", errOf(f.c.CreateInvitation(f.ctx, f.ch(dave), "carol@example.com")))
		for _, bad := range []string{"", "not an address", "Name <x@example.com>", strings.Repeat("a", 250) + "@example.com"} {
			expectKind(t, fmt.Sprintf("inviting %q", bad), errOf(f.c.CreateInvitation(f.ctx, f.ch(carol), bad)), entity.TeamInvalid)
		}
		expectKind(t, "inviting Bob, already in, by another case", errOf(f.c.CreateInvitation(f.ctx, f.ch(carol), "BOB@example.com")), entity.TeamConflict)
		expectConflict(t, "Carol inviting from the version before", errOf(f.c.CreateInvitation(f.ctx, f.stale(carol), "dave@example.com")), 5)

		rs, err := f.c.CreateInvitation(f.ctx, f.ch(carol), " Dave@Example.com ")
		if err != nil {
			t.Fatal(err)
		}
		inv := rs.Invitation
		if inv.Email != "dave@example.com" || inv.TeamName != "core" || inv.InvitedBy != carol || inv.TeamID != f.team ||
			inv.InvitedByName != "Carol" || !inv.ExpiresAt.Equal(t0.Add(7*24*time.Hour)) || !inv.CreatedAt.Equal(t0) || rs.EmailSent || rs.Version != 6 {
			t.Errorf("the invitation is %+v (email sent %v, version %d)", inv, rs.EmailSent, rs.Version)
		}
		if !strings.HasPrefix(rs.Link, "https://agentrq.example/invite#") || len(token(rs.Link)) != 43 {
			t.Errorf("the link is %q, want https://agentrq.example/invite#<43 base64url characters>", rs.Link)
		}
		var stored model.TeamInvitation
		f.db.First(&stored, inv.ID)
		if stored.TokenHash != HashToken(token(rs.Link)) || strings.Contains(stored.TokenHash, token(rs.Link)) {
			t.Errorf("the stored token is %q, want only the token's SHA-256", stored.TokenHash)
		}
		expectKind(t, "inviting Dave again", errOf(f.c.CreateInvitation(f.ctx, f.ch(alice), "dave@example.com")), entity.TeamConflict)
		expectOK(t, "Bob inviting Eve", errOf(f.c.CreateInvitation(f.ctx, f.ch(bob), "eve@example.com")))

		list := must(f.c.ListTeamInvitations(f.ctx, carol, f.team))
		if len(list.Invitations) != 2 || list.Invitations[0].Email != "eve@example.com" || list.Invitations[1].Email != "dave@example.com" ||
			list.Invitations[1].InvitedByName != "Carol" || list.Version != 7 {
			t.Errorf("pending invitations are %+v, want Eve's then Dave's at version 7", list)
		}
		if acts := f.activities(f.team); acts[0] != "member.invited 2 eve@example.com" || acts[1] != "member.invited 3 dave@example.com" {
			t.Errorf("the activity is %v", acts)
		}
		if acts := strings.Join(f.events.actions(), " "); !strings.Contains(acts, "team_invite:0") {
			t.Errorf("events are %s, want a team_invite", acts)
		}
	})
}

// The inviter learns nothing about whether the address has an account.
func TestTheAnswerIsTheSameWithOrWithoutAnAccount(t *testing.T) {
	each(t, func(f *fixture) {
		withAccount := must(f.c.CreateInvitation(f.ctx, f.ch(alice), "dave@example.com"))
		without := must(f.c.CreateInvitation(f.ctx, f.ch(alice), "stranger@example.com"))
		if withAccount.EmailSent != without.EmailSent || (withAccount.Link == "") != (without.Link == "") {
			t.Errorf("answers differ: %+v and %+v", withAccount, without)
		}
	})
}

func TestInvitationsAreEmailedWhenSMTPIsSetUp(t *testing.T) {
	m := &mailbox{}
	each(t, func(f *fixture) {
		m.sent = nil
		rs := must(f.c.CreateInvitation(f.ctx, f.ch(bob), "dave@example.com"))
		if !rs.EmailSent || rs.Link != "" || len(m.sent) != 1 || rs.Version != 6 {
			t.Fatalf("answer %+v with %d emails, want the email sent, no link, version 6", rs, len(m.sent))
		}
		mail := m.sent[0]
		if mail.To[0] != "dave@example.com" || mail.Subject != "Bob invited you to the core team on AgentRQ" ||
			!strings.Contains(mail.Body, "https://agentrq.example/invite#") || !strings.Contains(mail.Body, "expires in 7 days") {
			t.Errorf("the email is %+v", mail)
		}
		// An email that fails gives the link instead.
		m.err = errors.New("network unreachable")
		rs = must(f.c.CreateInvitation(f.ctx, f.ch(bob), "eve@example.com"))
		if rs.EmailSent || !strings.HasPrefix(rs.Link, "https://agentrq.example/invite#") || rs.Version != 7 {
			t.Errorf("after a failed email the answer is %+v, want the link", rs)
		}
		m.err = nil
	}, withMailer(m))
}

func TestInviteSubjectWithoutAName(t *testing.T) {
	if got := inviteSubject("", "core"); got != "Someone invited you to the core team on AgentRQ" {
		t.Errorf("inviteSubject without an inviter name returned %q", got)
	}
}

func TestMaskEmail(t *testing.T) {
	for in, want := range map[string]string{"bob@acme.com": "b***@acme.com", "@acme.com": "***", "plain": "***"} {
		if got := maskEmail(in); got != want {
			t.Errorf("maskEmail(%q) returned %q, want %q", in, got, want)
		}
	}
}

func TestAcceptingByLink(t *testing.T) {
	each(t, func(f *fixture) {
		rs := must(f.c.CreateInvitation(f.ctx, f.ch(carol), "dave@example.com"))
		tok := token(rs.Link)

		p := must(f.c.PreviewInvitation(f.ctx, eve, tok))
		if p.TeamID != f.team || p.TeamName != "core" || p.InvitedByName != "Carol" || p.Email != "d***@example.com" || p.EmailMatches ||
			!p.ExpiresAt.Equal(t0.Add(InvitationTTL)) {
			t.Errorf("Eve previews %+v, want core, from Carol, for d***@example.com, not hers", p)
		}
		if p := must(f.c.PreviewInvitation(f.ctx, dave, tok)); !p.EmailMatches {
			t.Errorf("Dave previews %+v, want it to match his email", p)
		}
		_, err := f.c.AcceptInvitation(f.ctx, eve, tok)
		expectKind(t, "Eve accepting Dave's invitation", err, entity.TeamForbidden)
		if err == nil || err.Error() != "this invitation is for d***@example.com: sign in with that account" {
			t.Errorf("Eve is told %q", err)
		}
		expectNotFound(t, "accepting an unknown token", errOf(f.c.AcceptInvitation(f.ctx, dave, "nope")))
		expectNotFound(t, "accepting no token", errOf(f.c.AcceptInvitation(f.ctx, dave, "")))
		expectNotFound(t, "previewing an unknown token", errOf(f.c.PreviewInvitation(f.ctx, dave, "nope")))

		// The invitee names no version; accepting moves it on.
		got := must(f.c.AcceptInvitation(f.ctx, dave, tok))
		if got.ID != f.team || got.MemberCount != 4 || got.Version != 7 {
			t.Errorf("Dave accepting got %+v, want core of 4 at version 7", got)
		}
		expectKind(t, "accepting a used link", errOf(f.c.AcceptInvitation(f.ctx, dave, tok)), entity.TeamGone)
		expectKind(t, "previewing a used link", errOf(f.c.PreviewInvitation(f.ctx, dave, tok)), entity.TeamGone)
		if acts := strings.Join(f.events.actions(), " "); !strings.Contains(acts, "team_join:0") {
			t.Errorf("events are %s, want a team_join", acts)
		}
		if acts := f.activities(f.team); acts[0] != "invitation.accepted 4 dave@example.com" {
			t.Errorf("the activity is %v", acts)
		}

		// The account's email in another case still matches.
		f.db.Model(&model.User{}).Where("id = ?", eve).Update("email", "EVE@example.com")
		rs = must(f.c.CreateInvitation(f.ctx, f.ch(alice), "eve@example.com"))
		expectOK(t, "Eve accepting with her email in capitals", errOf(f.c.AcceptInvitation(f.ctx, eve, token(rs.Link))))
	})
}

func TestRevokedAndExpiredInvitationsAreRefused(t *testing.T) {
	each(t, func(f *fixture) {
		revoked := must(f.c.CreateInvitation(f.ctx, f.ch(bob), "dave@example.com"))
		expectNotFound(t, "Dave, outside, revoking", errOf(f.c.RevokeInvitation(f.ctx, f.ch(dave), revoked.Invitation.ID)))
		expectNotFound(t, "revoking an unknown invitation", errOf(f.c.RevokeInvitation(f.ctx, f.ch(bob), 424242)))
		expectConflict(t, "Carol revoking from the version before", errOf(f.c.RevokeInvitation(f.ctx, f.stale(carol), revoked.Invitation.ID)), 6)
		if v, err := f.c.RevokeInvitation(f.ctx, f.ch(carol), revoked.Invitation.ID); err != nil || v != 7 {
			t.Errorf("Carol revoking returned %d, %v; want version 7", v, err)
		}
		expectNotFound(t, "revoking it twice", errOf(f.c.RevokeInvitation(f.ctx, f.ch(carol), revoked.Invitation.ID)))
		expectKind(t, "accepting a revoked invitation", errOf(f.c.AcceptInvitation(f.ctx, dave, token(revoked.Link))), entity.TeamGone)
		if list := must(f.c.ListTeamInvitations(f.ctx, alice, f.team)); len(list.Invitations) != 0 {
			t.Errorf("a revoked invitation is still listed: %+v", list)
		}
		if acts := f.activities(f.team); acts[0] != "invitation.revoked 3 dave@example.com" {
			t.Errorf("the activity is %v", acts)
		}

		expired := must(f.c.CreateInvitation(f.ctx, f.ch(bob), "eve@example.com"))
		f.now = t0.Add(InvitationTTL)
		expectKind(t, "accepting at the very moment it expires", errOf(f.c.AcceptInvitation(f.ctx, eve, token(expired.Link))), entity.TeamGone)
		expectKind(t, "accepting an expired invitation by id", errOf(f.c.AcceptInvitationByID(f.ctx, eve, expired.Invitation.ID)), entity.TeamGone)
		f.now = t0.Add(InvitationTTL - time.Second)
		expectOK(t, "accepting a second before it expires", errOf(f.c.AcceptInvitation(f.ctx, eve, token(expired.Link))))
	})
}

func TestTheInboxAcceptsAndDeclines(t *testing.T) {
	each(t, func(f *fixture) {
		other := must(f.c.CreateTeam(f.ctx, eve, "ops"))
		first := must(f.c.CreateInvitation(f.ctx, f.ch(alice), "dave@example.com"))
		f.now = t0.Add(time.Minute)
		second := must(f.c.CreateInvitation(f.ctx, f.chOf(eve, other.ID), "DAVE@example.com"))

		inbox := must(f.c.ListMyInvitations(f.ctx, dave))
		if len(inbox) != 2 || inbox[0].TeamName != "ops" || inbox[0].InvitedByName != "Eve" || inbox[1].TeamName != "core" || inbox[1].InvitedByName != "Alice" {
			t.Errorf("Dave's inbox is %+v, want ops from Eve, then core from Alice", inbox)
		}
		if inbox := must(f.c.ListMyInvitations(f.ctx, carol)); len(inbox) != 0 {
			t.Errorf("Carol's inbox is %+v, want it empty", inbox)
		}
		expectNotFound(t, "Carol accepting Dave's invitation by id", errOf(f.c.AcceptInvitationByID(f.ctx, carol, first.Invitation.ID)))
		expectNotFound(t, "Carol declining Dave's invitation", f.c.DeclineInvitation(f.ctx, carol, first.Invitation.ID))
		expectNotFound(t, "accepting an unknown id", errOf(f.c.AcceptInvitationByID(f.ctx, dave, 424242)))
		expectNotFound(t, "declining an unknown id", f.c.DeclineInvitation(f.ctx, dave, 424242))

		got := must(f.c.AcceptInvitationByID(f.ctx, dave, first.Invitation.ID))
		if got.ID != f.team || got.MemberCount != 4 {
			t.Errorf("Dave accepting from his inbox got %+v", got)
		}
		expectKind(t, "declining an accepted invitation", f.c.DeclineInvitation(f.ctx, dave, first.Invitation.ID), entity.TeamGone)
		before := f.version(other.ID)
		expectOK(t, "Dave declining ops", f.c.DeclineInvitation(f.ctx, dave, second.Invitation.ID))
		if v := f.version(other.ID); v != before+1 {
			t.Errorf("declining left ops at %d, want %d", v, before+1)
		}
		if acts := f.activities(other.ID); acts[0] != "invitation.declined 4 dave@example.com" {
			t.Errorf("ops' activity is %v, want Dave declining", acts)
		}
		expectKind(t, "declining twice", f.c.DeclineInvitation(f.ctx, dave, second.Invitation.ID), entity.TeamGone)
		expectKind(t, "accepting a declined invitation", errOf(f.c.AcceptInvitationByID(f.ctx, dave, second.Invitation.ID)), entity.TeamGone)
		if inbox := must(f.c.ListMyInvitations(f.ctx, dave)); len(inbox) != 0 {
			t.Errorf("Dave's inbox after answering both is %+v", inbox)
		}
		// An expired invitation may still be declined.
		late := must(f.c.CreateInvitation(f.ctx, f.chOf(eve, other.ID), "carol@example.com"))
		f.now = t0.Add(InvitationTTL + time.Hour)
		expectOK(t, "Carol declining an expired invitation", f.c.DeclineInvitation(f.ctx, carol, late.Invitation.ID))
		// An account with no email has no inbox.
		f.db.Model(&model.User{}).Where("id = ?", eve).Update("email", "")
		if inbox := must(f.c.ListMyInvitations(f.ctx, eve)); len(inbox) != 0 {
			t.Errorf("an account with no email lists %+v", inbox)
		}
	})
}

func TestTheDailyInvitationCap(t *testing.T) {
	each(t, func(f *fixture) {
		// The cast's two invitations count: one more fits under 3.
		expectOK(t, "the 3rd invitation of 3", errOf(f.c.CreateInvitation(f.ctx, f.ch(alice), "u3@example.com")))
		_, err := f.c.CreateInvitation(f.ctx, f.ch(alice), "u4@example.com")
		expectKind(t, "the 4th invitation of 3", err, entity.TeamThrottled)
		if err == nil || err.Error() != "this team has reached its limit of 3 invitations per day" {
			t.Errorf("the refusal says %q", err)
		}
		// A refused invitation leaves the version where it was.
		if v := f.version(f.team); v != 6 {
			t.Errorf("after the refusal the team is at %d, want 6", v)
		}
		// Revoking gives none back.
		list := must(f.c.ListTeamInvitations(f.ctx, alice, f.team))
		expectOK(t, "revoking", errOf(f.c.RevokeInvitation(f.ctx, f.ch(alice), list.Invitations[0].ID)))
		expectKind(t, "inviting after a revoke", errOf(f.c.CreateInvitation(f.ctx, f.ch(alice), "u4@example.com")), entity.TeamThrottled)
		// The window rolls: the cast's two were made at t0.
		f.now = t0.Add(24*time.Hour - time.Second)
		expectKind(t, "inviting a second before the window rolls", errOf(f.c.CreateInvitation(f.ctx, f.ch(alice), "u4@example.com")), entity.TeamThrottled)
		f.now = t0.Add(24 * time.Hour)
		expectOK(t, "inviting once the window rolls", errOf(f.c.CreateInvitation(f.ctx, f.ch(alice), "u4@example.com")))
		// Another team has its own cap.
		other := must(f.c.CreateTeam(f.ctx, dave, "ops"))
		expectOK(t, "another team inviting", errOf(f.c.CreateInvitation(f.ctx, f.chOf(dave, other.ID), "u5@example.com")))
	}, withCap(3))
}

func TestACapOfZeroTurnsInvitationsOff(t *testing.T) {
	for name, db := range databases(t) {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, db) // the cast joins under the default cap
			f.c.(*controller).maxInvite = 0
			_, err := f.c.CreateInvitation(f.ctx, f.ch(alice), "dave@example.com")
			expectKind(t, "inviting with a cap of 0", err, entity.TeamForbidden)
			if err == nil || err.Error() != "invitations are turned off on this server" {
				t.Errorf("the refusal says %q", err)
			}
		})
	}
}

// invite creates an invitation from the team's version now, loading it
// again whenever another invitation landed first, until it lands or is
// refused for another reason.
func (f *fixture) invite(userID, teamID int64, email string) error {
	for {
		_, err := f.c.CreateInvitation(f.ctx, f.chOf(userID, teamID), email)
		var ve *entity.TeamVersionError
		if !errors.As(err, &ve) {
			return err
		}
	}
}

// Parallel requests, each reloading on a conflict, never pass the cap
// together.
func TestTheCapHoldsUnderParallelRequests(t *testing.T) {
	each(t, func(f *fixture) {
		const requests = 20
		var wg sync.WaitGroup
		errs := make([]error, requests)
		for i := range requests {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs[i] = f.invite([]int64{alice, bob, carol}[i%3], f.team, fmt.Sprintf("p%d@example.com", i))
			}()
		}
		wg.Wait()
		ok, throttled := 0, 0
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case kind(err) == entity.TeamThrottled:
				throttled++
			default:
				t.Errorf("a request failed with %v, want success or the cap", err)
			}
		}
		// The cast used 2 of 7.
		if ok != 5 || throttled != 15 {
			t.Errorf("%d invitations created and %d refused, want 5 and 15", ok, throttled)
		}
	}, withCap(7))
}

func TestThePerUserInvitationCapSpansTeams(t *testing.T) {
	each(t, func(f *fixture) {
		// The cast's two are Alice's; a second team gives her no more.
		ops := must(f.c.CreateTeam(f.ctx, alice, "ops")).ID
		expectOK(t, "the 3rd of 4, in another team", errOf(f.c.CreateInvitation(f.ctx, f.chOf(alice, ops), "u3@example.com")))
		expectOK(t, "the 4th of 4", errOf(f.c.CreateInvitation(f.ctx, f.ch(alice), "u4@example.com")))
		_, err := f.c.CreateInvitation(f.ctx, f.chOf(alice, ops), "u5@example.com")
		expectKind(t, "the 5th of 4", err, entity.TeamThrottled)
		if err == nil || err.Error() != "you have reached your limit of 4 invitations per day across all your teams" {
			t.Errorf("the refusal says %q", err)
		}
		// Revoking gives none back; a resend is a new invitation, so it counts.
		list := must(f.c.ListTeamInvitations(f.ctx, alice, ops))
		expectOK(t, "revoking", errOf(f.c.RevokeInvitation(f.ctx, f.chOf(alice, ops), list.Invitations[0].ID)))
		expectKind(t, "resending after a revoke", errOf(f.c.CreateInvitation(f.ctx, f.chOf(alice, ops), "u3@example.com")), entity.TeamThrottled)
		// Bob, in the same team, has his own cap.
		expectOK(t, "another inviter", errOf(f.c.CreateInvitation(f.ctx, f.ch(bob), "u6@example.com")))
		// The window rolls.
		f.now = t0.Add(24 * time.Hour)
		expectOK(t, "inviting once the window rolls", errOf(f.c.CreateInvitation(f.ctx, f.chOf(alice, ops), "u5@example.com")))
	}, withUserCap(4))
}

// Both caps apply: whichever is reached first answers, saying which.
func TestTheTeamCapStillAppliesUnderTheUserCap(t *testing.T) {
	each(t, func(f *fixture) {
		_, err := f.c.CreateInvitation(f.ctx, f.ch(alice), "u3@example.com")
		if err == nil || err.Error() != "this team has reached its limit of 2 invitations per day" {
			t.Errorf("the refusal says %q", err)
		}
	}, withCap(2), withUserCap(20))
}

func TestAPerUserCapOfZeroIsNoCap(t *testing.T) {
	each(t, func(f *fixture) {
		ops := must(f.c.CreateTeam(f.ctx, alice, "ops")).ID
		for i := range 12 { // with the cast's 2, 14: past any per-user cap a test sets
			team := f.team
			if i%2 == 1 {
				team = ops
			}
			expectOK(t, fmt.Sprintf("invitation %d", i+3), errOf(f.c.CreateInvitation(f.ctx, f.chOf(alice, team), fmt.Sprintf("z%d@example.com", i))))
		}
	}, withUserCap(0))
}

// One account's parallel requests to two teams never pass its cap together.
func TestThePerUserCapHoldsUnderParallelRequests(t *testing.T) {
	each(t, func(f *fixture) {
		ops := must(f.c.CreateTeam(f.ctx, alice, "ops")).ID
		const requests = 20
		var wg sync.WaitGroup
		errs := make([]error, requests)
		for i := range requests {
			wg.Add(1)
			go func() {
				defer wg.Done()
				team := f.team
				if i%2 == 1 {
					team = ops
				}
				errs[i] = f.invite(alice, team, fmt.Sprintf("p%d@example.com", i))
			}()
		}
		wg.Wait()
		ok, throttled := 0, 0
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case kind(err) == entity.TeamThrottled && err.Error() == "you have reached your limit of 7 invitations per day across all your teams":
				throttled++
			default:
				t.Errorf("a request failed with %v, want success or the per-user cap", err)
			}
		}
		// The cast used 2 of 7.
		if ok != 5 || throttled != 15 {
			t.Errorf("%d invitations created and %d refused, want 5 and 15", ok, throttled)
		}
	}, withCap(100), withUserCap(7))
}

func TestSharingAWorkspace(t *testing.T) {
	each(t, func(f *fixture) {
		archived := t0
		f.db.Create(&model.Workspace{ID: 100, UserID: alice, Name: "backend"})
		f.db.Create(&model.Workspace{ID: 101, UserID: alice, Name: "fork", ForkOfID: 100})
		f.db.Create(&model.Workspace{ID: 102, UserID: alice, Name: "old", ArchivedAt: &archived})
		other := must(f.c.CreateTeam(f.ctx, dave, "ops"))

		s := must(f.c.ShareWorkspace(f.ctx, f.ch(alice), 100))
		if s.TeamName != "core" || s.TeamID != f.team || s.WorkspaceID != 100 || s.Version != 6 || !s.CreatedAt.Equal(t0) {
			t.Errorf("sharing gave %+v, want backend in core at version 6", s)
		}
		// Sharing again changes nothing and records nothing.
		if s := must(f.c.ShareWorkspace(f.ctx, f.ch(alice), 100)); s.WorkspaceID != 100 || s.Version != 7 {
			t.Errorf("sharing again gave %+v", s)
		}
		expectConflict(t, "sharing from the version before", errOf(f.c.ShareWorkspace(f.ctx, f.stale(alice), 100)), 7)
		expectNotFound(t, "Carol sharing Alice's workspace", errOf(f.c.ShareWorkspace(f.ctx, f.ch(carol), 100)))
		expectNotFound(t, "Alice sharing with a team she is not in", errOf(f.c.ShareWorkspace(f.ctx, f.chOf(alice, other.ID), 100)))
		expectNotFound(t, "sharing an unknown workspace", errOf(f.c.ShareWorkspace(f.ctx, f.ch(alice), 424242)))
		expectNotFound(t, "sharing with an unknown team", errOf(f.c.ShareWorkspace(f.ctx, entity.TeamChange{UserID: alice, TeamID: 424242, Version: 1}, 100)))
		expectKind(t, "sharing a fork", errOf(f.c.ShareWorkspace(f.ctx, f.ch(alice), 101)), entity.TeamInvalid)
		expectKind(t, "sharing an archived workspace", errOf(f.c.ShareWorkspace(f.ctx, f.ch(alice), 102)), entity.TeamInvalid)

		shares := must(f.c.ListShares(f.ctx, alice, 100))
		if len(shares) != 1 || shares[0].TeamName != "core" || shares[0].Version != 0 {
			t.Errorf("Alice's shares of backend are %+v", shares)
		}
		expectNotFound(t, "Carol listing Alice's shares", errOf(f.c.ListShares(f.ctx, carol, 100)))
		if acts := strings.Join(f.events.actions(), " "); !strings.Contains(acts, "workspace_share:100") {
			t.Errorf("events are %s, want a workspace_share of workspace 100", acts)
		}

		expectNotFound(t, "Carol unsharing Alice's workspace", errOf(f.c.UnshareWorkspace(f.ctx, f.ch(carol), 100)))
		expectNotFound(t, "unsharing from a team she is not in", errOf(f.c.UnshareWorkspace(f.ctx, f.chOf(alice, other.ID), 100)))
		expectConflict(t, "unsharing from the version before", errOf(f.c.UnshareWorkspace(f.ctx, f.stale(alice), 100)), 7)
		f.db.Model(&model.Workspace{}).Where("id = ?", 100).Update("name", "backend v2")
		if v, err := f.c.UnshareWorkspace(f.ctx, f.ch(alice), 100); err != nil || v != 8 {
			t.Errorf("Alice unsharing returned %d, %v; want version 8", v, err)
		}
		expectNotFound(t, "unsharing it twice", errOf(f.c.UnshareWorkspace(f.ctx, f.ch(alice), 100)))
		if shares := must(f.c.ListShares(f.ctx, alice, 100)); len(shares) != 0 {
			t.Errorf("shares after unsharing are %+v", shares)
		}
		as := must(f.c.ListActivities(f.ctx, entity.ListTeamActivitiesRequest{UserID: bob, TeamID: f.team, Limit: 2}))
		if len(as.Activities) != 2 {
			t.Fatalf("the activity is %+v", as)
		}
		un, sh := as.Activities[0], as.Activities[1]
		if un.Action != model.TeamActivityWorkspaceUnshared || un.TargetWorkspaceID != 100 || un.Details["workspaceName"] != "backend v2" || un.Actor.UserID != alice ||
			sh.Action != model.TeamActivityWorkspaceShared || sh.TargetWorkspaceID != 100 || sh.Details["workspaceName"] != "backend" {
			t.Errorf("the share and unshare are recorded as %+v and %+v", sh, un)
		}
	})
}

func TestTheTeamReadCheck(t *testing.T) {
	each(t, func(f *fixture) {
		f.db.Create(&model.Workspace{ID: 100, UserID: alice, Name: "Backend"})
		f.db.Create(&model.Workspace{ID: 101, UserID: alice, Name: "fork", ForkOfID: 100})
		f.db.Create(&model.Workspace{ID: 103, UserID: alice, Name: "private"})
		f.db.Create(&model.Workspace{ID: 200, UserID: carol, Name: "docs"})
		must(f.c.ShareWorkspace(f.ctx, f.ch(alice), 100))
		must(f.c.ShareWorkspace(f.ctx, f.ch(carol), 200))
		can := func(user, ws int64) bool { return must(f.c.CanReadWorkspaceInTeam(f.ctx, user, f.team, ws)) }
		if !can(bob, 100) || !can(bob, 101) || can(bob, 103) || can(dave, 100) {
			t.Errorf("Bob reads backend %v, its fork %v, private %v; Dave, outside, reads backend %v; want true, true, false, false",
				can(bob, 100), can(bob, 101), can(bob, 103), can(dave, 100))
		}
		list := must(f.c.ListTeamWorkspaces(f.ctx, bob, f.team))
		got := []string{}
		for _, w := range list {
			got = append(got, fmt.Sprintf("%s/%s/%d/%s/%s", w.SharedName, w.Name, w.ForkOfID, w.OwnerName, w.OwnerPicture))
		}
		want := "Backend/Backend/0/Alice/https://img/alice Backend/fork/100/Alice/https://img/alice docs/docs/0/Carol/"
		if strings.Join(got, " ") != want {
			t.Errorf("the team's workspaces are %v, want %s", got, want)
		}
		if list[0].ID != 100 || list[0].OwnerID != alice {
			t.Errorf("the first workspace is %+v, want 100 owned by Alice", list[0])
		}
		expectNotFound(t, "Dave listing the team's workspaces", errOf(f.c.ListTeamWorkspaces(f.ctx, dave, f.team)))
		// Carol leaves: her workspace goes with her.
		expectOK(t, "Carol leaving", f.c.LeaveTeam(f.ctx, f.ch(carol)))
		if can(bob, 200) {
			t.Error("Bob still reads Carol's workspace after she left")
		}
	})
}

// An invitation is valid only while its inviter is still in the team.
func TestAnInvitationNeedsAnInviterStillInTheTeam(t *testing.T) {
	each(t, func(f *fixture) {
		toDave := must(f.c.CreateInvitation(f.ctx, f.ch(bob), "dave@example.com"))
		toEve := must(f.c.CreateInvitation(f.ctx, f.ch(bob), "eve@example.com"))
		expectOK(t, "accepting while Bob is in", errOf(f.c.AcceptInvitation(f.ctx, dave, token(toDave.Link))))
		expectOK(t, "Alice removing Bob", errOf(f.c.RemoveMember(f.ctx, f.ch(alice), bob)))
		for name, err := range map[string]error{
			"previewing a removed member's invitation": errOf(f.c.PreviewInvitation(f.ctx, eve, token(toEve.Link))),
			"accepting it by link":                     errOf(f.c.AcceptInvitation(f.ctx, eve, token(toEve.Link))),
			"accepting it from the inbox":              errOf(f.c.AcceptInvitationByID(f.ctx, eve, toEve.Invitation.ID)),
		} {
			expectKind(t, name, err, entity.TeamGone)
		}
	})
}

// The activity is paged newest first: 20 unless the page says, never more
// than 100, older than the activity before names.
func TestTheActivityIsPaged(t *testing.T) {
	each(t, func(f *fixture) {
		for i := range 20 {
			f.now = t0.Add(time.Duration(i+1) * time.Second)
			must(f.c.RenameTeam(f.ctx, f.ch([]int64{alice, bob, carol}[i%3]), fmt.Sprintf("core %d", i)))
		}
		// 5 from the cast and 20 renames.
		page := must(f.c.ListActivities(f.ctx, entity.ListTeamActivitiesRequest{UserID: carol, TeamID: f.team}))
		if len(page.Activities) != 20 || page.Version != 25 || page.Activities[0].Details["to"] != "core 19" {
			t.Fatalf("the first page has %d at %d, starting %+v", len(page.Activities), page.Version, page.Activities[0])
		}
		rest := must(f.c.ListActivities(f.ctx, entity.ListTeamActivitiesRequest{UserID: carol, TeamID: f.team, Before: page.Activities[19].ID, Limit: 100}))
		if len(rest.Activities) != 5 || rest.Activities[4].Action != model.TeamActivityTeamCreated || rest.Activities[4].Actor.Name != "Alice" {
			t.Errorf("the second page is %+v", rest.Activities)
		}
		accepted := rest.Activities[0]
		if accepted.Action != model.TeamActivityInvitationAccepted || accepted.Actor.UserID != carol || accepted.TargetEmail != "carol@example.com" {
			t.Errorf("Carol's accept is recorded as %+v", accepted)
		}
		spy := &limits{Repository: f.repo}
		f.c.(*controller).repo = spy
		for _, limit := range []int{0, -3, 1, 100, 101, 5000} {
			must(f.c.ListActivities(f.ctx, entity.ListTeamActivitiesRequest{UserID: carol, TeamID: f.team, Limit: limit}))
		}
		if fmt.Sprint(spy.asked) != "[20 20 1 100 100 100]" {
			t.Errorf("the limits asked for are %v", spy.asked)
		}
		// A removal names its target.
		f.c.(*controller).repo = f.repo
		must(f.c.RemoveMember(f.ctx, f.ch(alice), bob))
		a := must(f.c.ListActivities(f.ctx, entity.ListTeamActivitiesRequest{UserID: carol, TeamID: f.team, Limit: 1})).Activities[0]
		if a.Action != model.TeamActivityMemberRemoved || a.Target.UserID != bob || a.Target.Name != "Bob" || a.Actor.UserID != alice {
			t.Errorf("the removal is recorded as %+v", a)
		}
	})
}

// limits records the limit each activity page asks for.
type limits struct {
	Repository
	asked []int
}

func (l *limits) ListTeamActivities(ctx context.Context, teamID int64, limit int, before int64) ([]base.TeamActivityRow, error) {
	l.asked = append(l.asked, limit)
	return l.Repository.ListTeamActivities(ctx, teamID, limit, before)
}
