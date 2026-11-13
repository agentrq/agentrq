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

// The cast: Alice owns the "core" team, Bob is its admin and Carol a member.
// Dave and Eve are in no team; Nobody has no account at all.
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
	if err := db.AutoMigrate(&model.User{}, &model.Workspace{}, &model.Team{}, &model.TeamMember{}, &model.TeamInvitation{}, &model.WorkspaceShare{}, &model.TeamMemory{}); err != nil {
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
	f.join(bob, "bob@example.com", model.TeamRoleAdmin)
	f.join(carol, "carol@example.com", model.TeamRoleMember)
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
func (f *fixture) join(userID int64, email, role string) {
	f.t.Helper()
	rs, err := f.c.CreateInvitation(f.ctx, alice, f.team, email, role)
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

func TestTeamsAreSeenOnlyByTheirMembers(t *testing.T) {
	each(t, func(f *fixture) {
		teams := must(f.c.ListTeams(f.ctx, carol))
		if len(teams) != 1 || teams[0].Name != "core" || teams[0].Role != model.TeamRoleMember || teams[0].MemberCount != 3 {
			t.Errorf("Carol's teams are %+v, want core as member, of 3", teams)
		}
		if teams := must(f.c.ListTeams(f.ctx, dave)); len(teams) != 0 {
			t.Errorf("Dave, in no team, lists %+v", teams)
		}
		got := must(f.c.GetTeam(f.ctx, bob, f.team))
		if got.Name != "core" || got.Role != model.TeamRoleAdmin || got.MemberCount != 3 || !got.CreatedAt.Equal(t0) {
			t.Errorf("Bob sees the team as %+v", got)
		}
		_, err := f.c.GetTeam(f.ctx, dave, f.team)
		expectNotFound(t, "Dave reading a team he is not in", err)
		_, err = f.c.GetTeam(f.ctx, alice, 424242)
		expectNotFound(t, "reading an unknown team", err)
		_, err = f.c.ListMembers(f.ctx, dave, f.team)
		expectNotFound(t, "Dave listing a team's members", err)
	})
}

func TestCreatingATeamMakesItsCreatorOwner(t *testing.T) {
	each(t, func(f *fixture) {
		got := must(f.c.CreateTeam(f.ctx, dave, " ops "))
		if got.Name != "ops" || got.Role != model.TeamRoleOwner || got.MemberCount != 1 {
			t.Errorf("Dave's new team is %+v, want ops, owned by him, of 1", got)
		}
		for _, name := range []string{"", "   ", strings.Repeat("é", 129)} {
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

func TestRenamingTakesAnOwnerOrAdmin(t *testing.T) {
	each(t, func(f *fixture) {
		got, err := f.c.RenameTeam(f.ctx, bob, f.team, " platform ")
		if err != nil || got.Name != "platform" || got.Role != model.TeamRoleAdmin || got.MemberCount != 3 {
			t.Errorf("Bob, an admin, renaming returned %+v, %v", got, err)
		}
		expectOK(t, "Alice, the owner, renaming", errOf(f.c.RenameTeam(f.ctx, alice, f.team, "core")))
		expectKind(t, "Carol, a member, renaming", errOf(f.c.RenameTeam(f.ctx, carol, f.team, "x")), entity.TeamForbidden)
		expectNotFound(t, "Dave, outside, renaming", errOf(f.c.RenameTeam(f.ctx, dave, f.team, "x")))
		expectKind(t, "renaming to nothing", errOf(f.c.RenameTeam(f.ctx, alice, f.team, " ")), entity.TeamInvalid)
	})
}

func errOf[T any](_ T, err error) error { return err }

func TestOnlyAnOwnerDeletesTheTeam(t *testing.T) {
	each(t, func(f *fixture) {
		expectKind(t, "Bob, an admin, deleting", f.c.DeleteTeam(f.ctx, bob, f.team), entity.TeamForbidden)
		expectNotFound(t, "Dave, outside, deleting", f.c.DeleteTeam(f.ctx, dave, f.team))
		expectOK(t, "Alice, the owner, deleting", f.c.DeleteTeam(f.ctx, alice, f.team))
		expectNotFound(t, "reading the deleted team", errOf(f.c.GetTeam(f.ctx, alice, f.team)))
		if teams := must(f.c.ListTeams(f.ctx, carol)); len(teams) != 0 {
			t.Errorf("Carol still lists %+v after the team was deleted", teams)
		}
	})
}

func TestMembersAreListedWithTheirAccounts(t *testing.T) {
	each(t, func(f *fixture) {
		members := must(f.c.ListMembers(f.ctx, carol, f.team))
		got := []string{}
		for _, m := range members {
			got = append(got, fmt.Sprintf("%d:%s:%s:%s:%s", m.UserID, m.Name, m.Email, m.Picture, m.Role))
		}
		want := "1:Alice:alice@example.com:https://img/alice:owner 2:Bob:bob@example.com::admin 3:Carol:Carol@Example.com::member"
		if strings.Join(got, " ") != want {
			t.Errorf("members are %v, want %s", got, want)
		}
	})
}

func TestChangingRoles(t *testing.T) {
	each(t, func(f *fixture) {
		expectKind(t, "Carol, a member, changing a role", f.c.SetMemberRole(f.ctx, carol, f.team, bob, model.TeamRoleMember), entity.TeamForbidden)
		expectNotFound(t, "Dave, outside, changing a role", f.c.SetMemberRole(f.ctx, dave, f.team, carol, model.TeamRoleAdmin))
		expectKind(t, "a role that is not one", f.c.SetMemberRole(f.ctx, alice, f.team, carol, "boss"), entity.TeamInvalid)
		expectNotFound(t, "changing the role of Dave, not in the team", f.c.SetMemberRole(f.ctx, alice, f.team, dave, model.TeamRoleAdmin))
		expectKind(t, "Bob, an admin, making Carol an owner", f.c.SetMemberRole(f.ctx, bob, f.team, carol, model.TeamRoleOwner), entity.TeamForbidden)
		expectKind(t, "Bob, an admin, demoting Alice, the owner", f.c.SetMemberRole(f.ctx, bob, f.team, alice, model.TeamRoleMember), entity.TeamForbidden)
		expectKind(t, "Alice demoting herself, the last owner", f.c.SetMemberRole(f.ctx, alice, f.team, alice, model.TeamRoleAdmin), entity.TeamConflict)
		expectOK(t, "Bob, an admin, making Carol an admin", f.c.SetMemberRole(f.ctx, bob, f.team, carol, model.TeamRoleAdmin))
		expectOK(t, "Alice making Bob an owner", f.c.SetMemberRole(f.ctx, alice, f.team, bob, model.TeamRoleOwner))
		expectOK(t, "Alice stepping down, with Bob an owner", f.c.SetMemberRole(f.ctx, alice, f.team, alice, model.TeamRoleMember))
		if r := must(f.c.GetTeam(f.ctx, carol, f.team)).Role; r != model.TeamRoleAdmin {
			t.Errorf("Carol is %q, want admin", r)
		}
	})
}

func TestRemovingMembers(t *testing.T) {
	each(t, func(f *fixture) {
		f.join(dave, "dave@example.com", model.TeamRoleMember)
		expectKind(t, "Carol, a member, removing Dave", f.c.RemoveMember(f.ctx, carol, f.team, dave), entity.TeamForbidden)
		expectNotFound(t, "Eve, outside, removing Dave", f.c.RemoveMember(f.ctx, eve, f.team, dave))
		expectKind(t, "Bob, an admin, removing Alice, the owner", f.c.RemoveMember(f.ctx, bob, f.team, alice), entity.TeamForbidden)
		expectNotFound(t, "removing Eve, not in the team", f.c.RemoveMember(f.ctx, alice, f.team, eve))
		expectKind(t, "Alice removing herself, the last owner", f.c.RemoveMember(f.ctx, alice, f.team, alice), entity.TeamConflict)
		expectOK(t, "Bob, an admin, removing Dave", f.c.RemoveMember(f.ctx, bob, f.team, dave))
		expectNotFound(t, "Dave reading the team he was removed from", errOf(f.c.GetTeam(f.ctx, dave, f.team)))
		expectOK(t, "Carol removing herself", f.c.RemoveMember(f.ctx, carol, f.team, carol))
		// Another owner can be removed by an owner.
		expectOK(t, "Alice making Bob an owner", f.c.SetMemberRole(f.ctx, alice, f.team, bob, model.TeamRoleOwner))
		expectOK(t, "Alice removing Bob, an owner", f.c.RemoveMember(f.ctx, alice, f.team, bob))
	})
}

func TestLeavingAndRemovalUnshare(t *testing.T) {
	each(t, func(f *fixture) {
		f.db.Create(&model.Workspace{ID: 300, UserID: carol, Name: "carol-ws"})
		f.db.Create(&model.Workspace{ID: 200, UserID: bob, Name: "bob-ws"})
		must(f.c.ShareWorkspace(f.ctx, carol, 300, f.team, ""))
		must(f.c.ShareWorkspace(f.ctx, bob, 200, f.team, ""))
		expectNotFound(t, "Dave, outside, leaving", f.c.LeaveTeam(f.ctx, dave, f.team))
		expectKind(t, "Alice, the last owner, leaving", f.c.LeaveTeam(f.ctx, alice, f.team), entity.TeamConflict)
		expectOK(t, "Carol leaving", f.c.LeaveTeam(f.ctx, carol, f.team))
		if shares := must(f.c.ListShares(f.ctx, carol, 300)); len(shares) != 0 {
			t.Errorf("Carol's workspace is still shared after she left: %+v", shares)
		}
		expectOK(t, "Alice removing Bob", f.c.RemoveMember(f.ctx, alice, f.team, bob))
		if shares := must(f.c.ListShares(f.ctx, bob, 200)); len(shares) != 0 {
			t.Errorf("Bob's workspace is still shared after his removal: %+v", shares)
		}
	})
}

func TestInvitingTakesAnOwnerOrAdmin(t *testing.T) {
	each(t, func(f *fixture) {
		expectKind(t, "Carol, a member, inviting", errOf(f.c.CreateInvitation(f.ctx, carol, f.team, "x@example.com", "")), entity.TeamForbidden)
		expectNotFound(t, "Dave, outside, inviting", errOf(f.c.CreateInvitation(f.ctx, dave, f.team, "x@example.com", "")))
		expectKind(t, "Carol, a member, listing invitations", errOf(f.c.ListTeamInvitations(f.ctx, carol, f.team)), entity.TeamForbidden)
		expectKind(t, "Bob, an admin, inviting an owner", errOf(f.c.CreateInvitation(f.ctx, bob, f.team, "x@example.com", model.TeamRoleOwner)), entity.TeamForbidden)
		expectKind(t, "a role that is not one", errOf(f.c.CreateInvitation(f.ctx, bob, f.team, "x@example.com", "boss")), entity.TeamInvalid)
		for _, bad := range []string{"", "not an address", "Name <x@example.com>", strings.Repeat("a", 250) + "@example.com"} {
			expectKind(t, fmt.Sprintf("inviting %q", bad), errOf(f.c.CreateInvitation(f.ctx, bob, f.team, bad, "")), entity.TeamInvalid)
		}
		expectKind(t, "inviting Carol, already in, by another case", errOf(f.c.CreateInvitation(f.ctx, bob, f.team, "CAROL@example.com", "")), entity.TeamConflict)

		rs, err := f.c.CreateInvitation(f.ctx, bob, f.team, " Dave@Example.com ", "")
		if err != nil {
			t.Fatal(err)
		}
		inv := rs.Invitation
		if inv.Email != "dave@example.com" || inv.Role != model.TeamRoleMember || inv.TeamName != "core" || inv.InvitedBy != bob ||
			inv.InvitedByName != "Bob" || !inv.ExpiresAt.Equal(t0.Add(7*24*time.Hour)) || rs.EmailSent {
			t.Errorf("the invitation is %+v (email sent %v)", inv, rs.EmailSent)
		}
		if !strings.HasPrefix(rs.Link, "https://agentrq.example/invite#") || len(token(rs.Link)) != 43 {
			t.Errorf("the link is %q, want https://agentrq.example/invite#<43 base64url characters>", rs.Link)
		}
		var stored model.TeamInvitation
		f.db.First(&stored, inv.ID)
		if stored.TokenHash != HashToken(token(rs.Link)) || strings.Contains(stored.TokenHash, token(rs.Link)) {
			t.Errorf("the stored token is %q, want only the token's SHA-256", stored.TokenHash)
		}
		expectKind(t, "inviting Dave again", errOf(f.c.CreateInvitation(f.ctx, alice, f.team, "dave@example.com", "")), entity.TeamConflict)
		expectOK(t, "Alice inviting an owner", errOf(f.c.CreateInvitation(f.ctx, alice, f.team, "eve@example.com", model.TeamRoleOwner)))

		list := must(f.c.ListTeamInvitations(f.ctx, bob, f.team))
		if len(list) != 2 || list[0].Email != "eve@example.com" || list[1].Email != "dave@example.com" || list[1].InvitedByName != "Bob" {
			t.Errorf("pending invitations are %+v, want Eve's then Dave's", list)
		}
		if acts := strings.Join(f.events.actions(), " "); !strings.Contains(acts, "team_invite:0") {
			t.Errorf("events are %s, want a team_invite", acts)
		}
	})
}

// The inviter learns nothing about whether the address has an account.
func TestTheAnswerIsTheSameWithOrWithoutAnAccount(t *testing.T) {
	each(t, func(f *fixture) {
		withAccount := must(f.c.CreateInvitation(f.ctx, alice, f.team, "dave@example.com", ""))
		without := must(f.c.CreateInvitation(f.ctx, alice, f.team, "stranger@example.com", ""))
		if withAccount.EmailSent != without.EmailSent || (withAccount.Link == "") != (without.Link == "") {
			t.Errorf("answers differ: %+v and %+v", withAccount, without)
		}
	})
}

func TestInvitationsAreEmailedWhenSMTPIsSetUp(t *testing.T) {
	m := &mailbox{}
	each(t, func(f *fixture) {
		m.sent = nil
		rs := must(f.c.CreateInvitation(f.ctx, bob, f.team, "dave@example.com", ""))
		if !rs.EmailSent || rs.Link != "" || len(m.sent) != 1 {
			t.Fatalf("answer %+v with %d emails, want the email sent and no link", rs, len(m.sent))
		}
		mail := m.sent[0]
		if mail.To[0] != "dave@example.com" || mail.Subject != "Bob invited you to the core team on AgentRQ" ||
			!strings.Contains(mail.Body, "https://agentrq.example/invite#") || !strings.Contains(mail.Body, "expires in 7 days") {
			t.Errorf("the email is %+v", mail)
		}
		// An email that fails gives the link instead.
		m.err = errors.New("network unreachable")
		rs = must(f.c.CreateInvitation(f.ctx, bob, f.team, "eve@example.com", ""))
		if rs.EmailSent || !strings.HasPrefix(rs.Link, "https://agentrq.example/invite#") {
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
		rs := must(f.c.CreateInvitation(f.ctx, bob, f.team, "dave@example.com", model.TeamRoleAdmin))
		tok := token(rs.Link)

		p := must(f.c.PreviewInvitation(f.ctx, eve, tok))
		if p.TeamName != "core" || p.Role != model.TeamRoleAdmin || p.InvitedByName != "Bob" || p.Email != "d***@example.com" || p.EmailMatches {
			t.Errorf("Eve previews %+v, want core, admin, from Bob, for d***@example.com, not hers", p)
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

		got := must(f.c.AcceptInvitation(f.ctx, dave, tok))
		if got.ID != f.team || got.Role != model.TeamRoleAdmin || got.MemberCount != 4 {
			t.Errorf("Dave accepting got %+v, want core as admin, of 4", got)
		}
		expectKind(t, "accepting a used link", errOf(f.c.AcceptInvitation(f.ctx, dave, tok)), entity.TeamGone)
		expectKind(t, "previewing a used link", errOf(f.c.PreviewInvitation(f.ctx, dave, tok)), entity.TeamGone)
		if acts := strings.Join(f.events.actions(), " "); !strings.Contains(acts, "team_join:0") {
			t.Errorf("events are %s, want a team_join", acts)
		}

		// The account's email in another case still matches.
		f.db.Model(&model.User{}).Where("id = ?", eve).Update("email", "EVE@example.com")
		rs = must(f.c.CreateInvitation(f.ctx, alice, f.team, "eve@example.com", ""))
		expectOK(t, "Eve accepting with her email in capitals", errOf(f.c.AcceptInvitation(f.ctx, eve, token(rs.Link))))
	})
}

func TestRevokedAndExpiredInvitationsAreRefused(t *testing.T) {
	each(t, func(f *fixture) {
		revoked := must(f.c.CreateInvitation(f.ctx, bob, f.team, "dave@example.com", ""))
		expectKind(t, "Carol, a member, revoking", f.c.RevokeInvitation(f.ctx, carol, f.team, revoked.Invitation.ID), entity.TeamForbidden)
		expectNotFound(t, "revoking an unknown invitation", f.c.RevokeInvitation(f.ctx, bob, f.team, 424242))
		expectOK(t, "Bob revoking", f.c.RevokeInvitation(f.ctx, bob, f.team, revoked.Invitation.ID))
		expectKind(t, "accepting a revoked invitation", errOf(f.c.AcceptInvitation(f.ctx, dave, token(revoked.Link))), entity.TeamGone)
		if list := must(f.c.ListTeamInvitations(f.ctx, alice, f.team)); len(list) != 0 {
			t.Errorf("a revoked invitation is still listed: %+v", list)
		}

		expired := must(f.c.CreateInvitation(f.ctx, bob, f.team, "eve@example.com", ""))
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
		first := must(f.c.CreateInvitation(f.ctx, alice, f.team, "dave@example.com", ""))
		f.now = t0.Add(time.Minute)
		second := must(f.c.CreateInvitation(f.ctx, eve, other.ID, "DAVE@example.com", model.TeamRoleAdmin))

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
		if got.ID != f.team || got.Role != model.TeamRoleMember {
			t.Errorf("Dave accepting from his inbox got %+v", got)
		}
		expectKind(t, "declining an accepted invitation", f.c.DeclineInvitation(f.ctx, dave, first.Invitation.ID), entity.TeamGone)
		expectOK(t, "Dave declining ops", f.c.DeclineInvitation(f.ctx, dave, second.Invitation.ID))
		expectKind(t, "declining twice", f.c.DeclineInvitation(f.ctx, dave, second.Invitation.ID), entity.TeamGone)
		expectKind(t, "accepting a declined invitation", errOf(f.c.AcceptInvitationByID(f.ctx, dave, second.Invitation.ID)), entity.TeamGone)
		if inbox := must(f.c.ListMyInvitations(f.ctx, dave)); len(inbox) != 0 {
			t.Errorf("Dave's inbox after answering both is %+v", inbox)
		}
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
		expectOK(t, "the 3rd invitation of 3", errOf(f.c.CreateInvitation(f.ctx, alice, f.team, "u3@example.com", "")))
		_, err := f.c.CreateInvitation(f.ctx, alice, f.team, "u4@example.com", "")
		expectKind(t, "the 4th invitation of 3", err, entity.TeamThrottled)
		if err == nil || err.Error() != "this team has reached its limit of 3 invitations per day" {
			t.Errorf("the refusal says %q", err)
		}
		// Revoking gives none back.
		list := must(f.c.ListTeamInvitations(f.ctx, alice, f.team))
		expectOK(t, "revoking", f.c.RevokeInvitation(f.ctx, alice, f.team, list[0].ID))
		expectKind(t, "inviting after a revoke", errOf(f.c.CreateInvitation(f.ctx, alice, f.team, "u4@example.com", "")), entity.TeamThrottled)
		// The window rolls: the cast's two were made at t0.
		f.now = t0.Add(24*time.Hour - time.Second)
		expectKind(t, "inviting a second before the window rolls", errOf(f.c.CreateInvitation(f.ctx, alice, f.team, "u4@example.com", "")), entity.TeamThrottled)
		f.now = t0.Add(24 * time.Hour)
		expectOK(t, "inviting once the window rolls", errOf(f.c.CreateInvitation(f.ctx, alice, f.team, "u4@example.com", "")))
		// Another team has its own cap.
		other := must(f.c.CreateTeam(f.ctx, dave, "ops"))
		expectOK(t, "another team inviting", errOf(f.c.CreateInvitation(f.ctx, dave, other.ID, "u5@example.com", "")))
	}, withCap(3))
}

func TestACapOfZeroTurnsInvitationsOff(t *testing.T) {
	for name, db := range databases(t) {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, db) // the cast joins under the default cap
			f.c.(*controller).maxInvite = 0
			_, err := f.c.CreateInvitation(f.ctx, alice, f.team, "dave@example.com", "")
			expectKind(t, "inviting with a cap of 0", err, entity.TeamForbidden)
			if err == nil || err.Error() != "invitations are turned off on this server" {
				t.Errorf("the refusal says %q", err)
			}
			expectKind(t, "a member inviting with a cap of 0", errOf(f.c.CreateInvitation(f.ctx, carol, f.team, "dave@example.com", "")), entity.TeamForbidden)
		})
	}
}

// Parallel requests never pass the cap together.
func TestTheCapHoldsUnderParallelRequests(t *testing.T) {
	each(t, func(f *fixture) {
		const requests = 20
		var wg sync.WaitGroup
		errs := make([]error, requests)
		for i := range requests {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, errs[i] = f.c.CreateInvitation(f.ctx, alice, f.team, fmt.Sprintf("p%d@example.com", i), "")
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

func TestSharingAWorkspace(t *testing.T) {
	each(t, func(f *fixture) {
		archived := t0
		f.db.Create(&model.Workspace{ID: 100, UserID: alice, Name: "backend"})
		f.db.Create(&model.Workspace{ID: 101, UserID: alice, Name: "fork", ForkOfID: 100})
		f.db.Create(&model.Workspace{ID: 102, UserID: alice, Name: "old", ArchivedAt: &archived})
		f.db.Create(&model.Workspace{ID: 200, UserID: dave, Name: "dave-ws"})
		other := must(f.c.CreateTeam(f.ctx, dave, "ops"))

		s := must(f.c.ShareWorkspace(f.ctx, alice, 100, f.team, ""))
		if s.Group != "backend" || s.TeamName != "core" || s.TeamID != f.team || s.WorkspaceID != 100 {
			t.Errorf("sharing with no group gave %+v, want group backend in core", s)
		}
		s = must(f.c.ShareWorkspace(f.ctx, alice, 100, f.team, "  Platform "))
		if s.Group != "Platform" {
			t.Errorf("sharing again gave group %q, want Platform", s.Group)
		}
		expectNotFound(t, "Carol sharing Alice's workspace", errOf(f.c.ShareWorkspace(f.ctx, carol, 100, f.team, "")))
		expectNotFound(t, "Alice sharing with a team she is not in", errOf(f.c.ShareWorkspace(f.ctx, alice, 100, other.ID, "")))
		expectNotFound(t, "sharing an unknown workspace", errOf(f.c.ShareWorkspace(f.ctx, alice, 424242, f.team, "")))
		expectKind(t, "sharing a fork", errOf(f.c.ShareWorkspace(f.ctx, alice, 101, f.team, "")), entity.TeamInvalid)
		expectKind(t, "sharing an archived workspace", errOf(f.c.ShareWorkspace(f.ctx, alice, 102, f.team, "")), entity.TeamInvalid)
		expectKind(t, "a group too long", errOf(f.c.ShareWorkspace(f.ctx, alice, 100, f.team, strings.Repeat("g", 129))), entity.TeamInvalid)

		shares := must(f.c.ListShares(f.ctx, alice, 100))
		if len(shares) != 1 || shares[0].Group != "Platform" || shares[0].TeamName != "core" {
			t.Errorf("Alice's shares of backend are %+v", shares)
		}
		expectNotFound(t, "Carol listing Alice's shares", errOf(f.c.ListShares(f.ctx, carol, 100)))
		if acts := strings.Join(f.events.actions(), " "); !strings.Contains(acts, "workspace_share:100") {
			t.Errorf("events are %s, want a workspace_share of workspace 100", acts)
		}

		expectNotFound(t, "Carol unsharing Alice's workspace", f.c.UnshareWorkspace(f.ctx, carol, 100, f.team))
		expectNotFound(t, "unsharing from a team it is not shared with", f.c.UnshareWorkspace(f.ctx, alice, 100, other.ID))
		expectOK(t, "Alice unsharing", f.c.UnshareWorkspace(f.ctx, alice, 100, f.team))
		if shares := must(f.c.ListShares(f.ctx, alice, 100)); len(shares) != 0 {
			t.Errorf("shares after unsharing are %+v", shares)
		}
	})
}

func TestTheTeamReadCheck(t *testing.T) {
	each(t, func(f *fixture) {
		f.db.Create(&model.Workspace{ID: 100, UserID: alice, Name: "backend"})
		f.db.Create(&model.Workspace{ID: 101, UserID: alice, Name: "fork", ForkOfID: 100})
		f.db.Create(&model.Workspace{ID: 103, UserID: alice, Name: "private"})
		f.db.Create(&model.Workspace{ID: 200, UserID: carol, Name: "docs"})
		must(f.c.ShareWorkspace(f.ctx, alice, 100, f.team, "Backend"))
		must(f.c.ShareWorkspace(f.ctx, carol, 200, f.team, ""))
		can := func(user, ws int64) bool { return must(f.c.CanReadWorkspaceInTeam(f.ctx, user, f.team, ws)) }
		if !can(bob, 100) || !can(bob, 101) || can(bob, 103) || can(dave, 100) {
			t.Errorf("Bob reads backend %v, its fork %v, private %v; Dave, outside, reads backend %v; want true, true, false, false",
				can(bob, 100), can(bob, 101), can(bob, 103), can(dave, 100))
		}
		list := must(f.c.ListTeamWorkspaces(f.ctx, bob, f.team))
		got := []string{}
		for _, w := range list {
			got = append(got, fmt.Sprintf("%s/%s/%d/%s/%s", w.Group, w.Name, w.ForkOfID, w.OwnerName, w.OwnerPicture))
		}
		want := "Backend/backend/0/Alice/https://img/alice Backend/fork/100/Alice/https://img/alice docs/docs/0/Carol/"
		if strings.Join(got, " ") != want {
			t.Errorf("the team's workspaces are %v, want %s", got, want)
		}
		if list[0].ID != 100 || list[0].OwnerID != alice {
			t.Errorf("the first workspace is %+v, want 100 owned by Alice", list[0])
		}
		expectNotFound(t, "Dave listing the team's workspaces", errOf(f.c.ListTeamWorkspaces(f.ctx, dave, f.team)))
		// Carol leaves: her workspace goes with her.
		expectOK(t, "Carol leaving", f.c.LeaveTeam(f.ctx, carol, f.team))
		if can(bob, 200) {
			t.Error("Bob still reads Carol's workspace after she left")
		}
	})
}
