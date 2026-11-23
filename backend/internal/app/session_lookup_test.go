// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/agentrq/backend/internal/controller/machine"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	mock_repository "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
)

// The authorisation boundary for the terminal socket, and the only place the
// attach learns which workspace it belongs to.
//
// That last part is why this is tested rather than left to the wiring: the
// workspace reaches telemetry through this struct and nowhere else, so
// dropping it here would leave every terminal view attributed to no workspace
// with nothing failing anywhere.
func TestSessionLookupDescribesTheAttach(t *testing.T) {
	const userID int64 = 3
	userBase62 := monoflake.ID(userID).String()

	ctrl := gomock.NewController(t)
	repo := mock_repository.NewMockRepository(ctrl)
	repo.EXPECT().GetSession(gomock.Any(), int64(500), userID).
		Return(model.Session{ID: 500, UserID: userID, MachineID: 11, WorkspaceID: 70}, nil)
	repo.EXPECT().GetMachine(gomock.Any(), int64(11), userID).
		Return(model.Machine{ID: 11, UserID: userID, InstanceID: "pod-a"}, nil)
	repo.EXPECT().SystemGetUser(gomock.Any(), userID).
		Return(model.User{ID: userID, Name: "Ada"}, nil)

	tokens := newTestTokenService(t)
	l := sessionLookup{repo: repo, tokens: tokens}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/x/terminal", nil)
	r.AddCookie(&http.Cookie{Name: "at", Value: newTestToken(t, tokens, userBase62)})

	at, err := l.LookupSession(r, 500)
	if err != nil {
		t.Fatal(err)
	}

	if at.MachineID != 11 || at.InstanceID != "pod-a" {
		t.Errorf("machine = %d on %q", at.MachineID, at.InstanceID)
	}
	if at.UserID != userID {
		t.Errorf("userID = %d, want %d", at.UserID, userID)
	}
	// The line this test exists for.
	if at.WorkspaceID != 70 {
		t.Errorf("workspaceID = %d, want 70 — a terminal view is counted against it", at.WorkspaceID)
	}
	// A display name, never the email address: presence answers "who else is
	// typing" without handing every viewer somebody's contact details.
	if at.ViewerName != "Ada" {
		t.Errorf("viewerName = %q", at.ViewerName)
	}
}

// A user with no name set is still someone, so they get a placeholder rather
// than an empty entry nobody can interpret.
func TestSessionLookupNamesAnAnonymousViewer(t *testing.T) {
	const userID int64 = 3
	ctrl := gomock.NewController(t)
	repo := mock_repository.NewMockRepository(ctrl)
	repo.EXPECT().GetSession(gomock.Any(), int64(500), userID).
		Return(model.Session{ID: 500, UserID: userID, MachineID: 11, WorkspaceID: 70}, nil)
	repo.EXPECT().GetMachine(gomock.Any(), int64(11), userID).
		Return(model.Machine{ID: 11, UserID: userID}, nil)
	repo.EXPECT().SystemGetUser(gomock.Any(), userID).Return(model.User{}, errors.New("user not found"))

	tokens := newTestTokenService(t)
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.AddCookie(&http.Cookie{Name: "at", Value: newTestToken(t, tokens, monoflake.ID(userID).String())})

	l := sessionLookup{repo: repo, tokens: tokens}
	at, err := l.LookupSession(r, 500)
	if err != nil {
		t.Fatal(err)
	}
	if at.ViewerName != "Someone" {
		t.Errorf("viewerName = %q, want the placeholder", at.ViewerName)
	}
}

// Every refusal, and all of them empty: the reason is the caller's to decide,
// not this function's to leak.
func TestSessionLookupRefusals(t *testing.T) {
	const userID int64 = 3
	userBase62 := monoflake.ID(userID).String()

	t.Run("no cookie", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		l := sessionLookup{repo: mock_repository.NewMockRepository(ctrl), tokens: newTestTokenService(t)}
		if _, err := l.LookupSession(httptest.NewRequest(http.MethodGet, "/x", nil), 500); err == nil {
			t.Fatal("expected a refusal")
		}
	})

	t.Run("unreadable token", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		l := sessionLookup{repo: mock_repository.NewMockRepository(ctrl), tokens: newTestTokenService(t)}
		r := httptest.NewRequest(http.MethodGet, "/x", nil)
		r.AddCookie(&http.Cookie{Name: "at", Value: "not-a-token"})
		if _, err := l.LookupSession(r, 500); err == nil {
			t.Fatal("expected a refusal")
		}
	})

	// Scoped to the user by the query itself: a session belonging to somebody
	// else is not found, which is also the right thing to tell the caller.
	t.Run("somebody else's session", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := mock_repository.NewMockRepository(ctrl)
		repo.EXPECT().GetSession(gomock.Any(), int64(500), userID).
			Return(model.Session{}, errors.New("not found"))
		tokens := newTestTokenService(t)
		r := httptest.NewRequest(http.MethodGet, "/x", nil)
		r.AddCookie(&http.Cookie{Name: "at", Value: newTestToken(t, tokens, userBase62)})
		l := sessionLookup{repo: repo, tokens: tokens}
		if _, err := l.LookupSession(r, 500); err == nil {
			t.Fatal("expected a refusal")
		}
	})

	t.Run("machine gone", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := mock_repository.NewMockRepository(ctrl)
		repo.EXPECT().GetSession(gomock.Any(), int64(500), userID).
			Return(model.Session{ID: 500, UserID: userID, MachineID: 11}, nil)
		repo.EXPECT().GetMachine(gomock.Any(), int64(11), userID).
			Return(model.Machine{}, errors.New("not found"))
		tokens := newTestTokenService(t)
		r := httptest.NewRequest(http.MethodGet, "/x", nil)
		r.AddCookie(&http.Cookie{Name: "at", Value: newTestToken(t, tokens, userBase62)})
		l := sessionLookup{repo: repo, tokens: tokens}
		if _, err := l.LookupSession(r, 500); err == nil {
			t.Fatal("expected a refusal")
		}
	})
}

// The credential the desktop app can actually present.
//
// The cookie half of this is covered above; this is the other half, and the
// reason the endpoint has two. A terminal socket from the desktop app is a
// cross-site request from an app:// page, so the browser withholds the
// SameSite=Lax `at` cookie from it — which is why that build sat on
// "connecting" while the browser's terminal worked.
func TestSessionLookupAcceptsATerminalTicket(t *testing.T) {
	const userID int64 = 3
	userBase62 := monoflake.ID(userID).String()

	ctrl := gomock.NewController(t)
	repo := mock_repository.NewMockRepository(ctrl)
	repo.EXPECT().GetSession(gomock.Any(), int64(500), userID).
		Return(model.Session{ID: 500, UserID: userID, MachineID: 11, WorkspaceID: 70}, nil)
	repo.EXPECT().GetMachine(gomock.Any(), int64(11), userID).
		Return(model.Machine{ID: 11, UserID: userID, InstanceID: "pod-a"}, nil)
	repo.EXPECT().SystemGetUser(gomock.Any(), userID).
		Return(model.User{ID: userID, Name: "Ada"}, nil)

	tokens := newTestTokenService(t)
	ticket, err := tokens.CreateTerminalTicket(userBase62, "500")
	if err != nil {
		t.Fatal(err)
	}

	// No cookie at all, which is the situation being reproduced.
	r := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/x/terminal?ticket="+ticket, nil)

	at, err := sessionLookup{repo: repo, tokens: tokens}.LookupSession(r, 500)
	if err != nil {
		t.Fatalf("a ticket-authenticated attach was refused: %v", err)
	}
	if at.UserID != userID || at.WorkspaceID != 70 || at.ViewerName != "Ada" {
		t.Errorf("attach = %+v — a ticket must describe the same viewer a cookie does", at)
	}
}

// What a ticket must not do. Each of these would be a way to watch a terminal
// the holder was never given.
func TestSessionLookupTicketRefusals(t *testing.T) {
	const userID int64 = 3
	userBase62 := monoflake.ID(userID).String()

	// The repository is never reached in any of these: the credential is
	// refused before anything is read, so a strict mock with no expectations
	// is itself the assertion.
	newLookup := func(t *testing.T) (sessionLookup, auth.TokenService) {
		t.Helper()
		tokens := newTestTokenService(t)
		repo := mock_repository.NewMockRepository(gomock.NewController(t))
		return sessionLookup{repo: repo, tokens: tokens}, tokens
	}

	t.Run("a ticket for another session", func(t *testing.T) {
		lookup, tokens := newLookup(t)
		ticket, err := tokens.CreateTerminalTicket(userBase62, "501")
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodGet, "/x?ticket="+ticket, nil)
		if _, err := lookup.LookupSession(r, 500); err == nil {
			t.Fatal("a ticket for session 501 attached to 500")
		}
	})

	// The access token is what a page holds already. If it worked here, the
	// cookie's contents would become a URL parameter that opens any terminal
	// on the account.
	t.Run("an access token in the ticket parameter", func(t *testing.T) {
		lookup, tokens := newLookup(t)
		r := httptest.NewRequest(http.MethodGet, "/x?ticket="+newTestToken(t, tokens, userBase62), nil)
		if _, err := lookup.LookupSession(r, 500); err == nil {
			t.Fatal("an access token was accepted in the URL")
		}
	})

	// A present-but-unusable ticket must not fall through to the cookie. It
	// would turn every refusal above into "try the other credential", and the
	// narrow scope of a ticket would stop meaning anything.
	t.Run("a bad ticket does not fall back to a good cookie", func(t *testing.T) {
		lookup, tokens := newLookup(t)
		wrong, err := tokens.CreateTerminalTicket(userBase62, "501")
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodGet, "/x?ticket="+wrong, nil)
		r.AddCookie(&http.Cookie{Name: "at", Value: newTestToken(t, tokens, userBase62)})
		if _, err := lookup.LookupSession(r, 500); err == nil {
			t.Fatal("a refused ticket was rescued by the cookie")
		}
	})

	t.Run("nonsense", func(t *testing.T) {
		lookup, _ := newLookup(t)
		r := httptest.NewRequest(http.MethodGet, "/x?ticket=not-a-token", nil)
		if _, err := lookup.LookupSession(r, 500); err == nil {
			t.Fatal("expected a refusal")
		}
	})
}

// A stub that records what it was asked to count.
type stubTerminalCounter struct {
	got []entity.RecordTerminalViewRequest
}

func (s *stubTerminalCounter) RecordTerminalView(_ context.Context, req entity.RecordTerminalViewRequest) {
	s.got = append(s.got, req)
}

// The hook the terminal socket calls, and the two things it can get wrong:
// which id goes where, and which end of the visit it is.
func TestCountTerminalViewCarriesTheAttachThrough(t *testing.T) {
	for _, tc := range []struct {
		name string
		open bool
	}{
		{name: "attach", open: true},
		{name: "detach", open: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &stubTerminalCounter{}
			at := machine.Attachment{MachineID: 11, UserID: 3, WorkspaceID: 70, ViewerName: "Ada"}

			countTerminalView(rec, tc.open)(httptest.NewRequest(http.MethodGet, "/x", nil), 500, at)

			if len(rec.got) != 1 {
				t.Fatalf("counted %d times, want 1", len(rec.got))
			}
			got := rec.got[0]
			if got.UserID != 3 || got.WorkspaceID != 70 {
				t.Errorf("scope = user %d / workspace %d", got.UserID, got.WorkspaceID)
			}
			// The session comes from the socket, not the attachment — mixing
			// it up with the machine id is the mistake this guards.
			if got.SessionID != 500 {
				t.Errorf("sessionID = %d, want 500", got.SessionID)
			}
			if got.Open != tc.open {
				t.Errorf("open = %v, want %v", got.Open, tc.open)
			}
		})
	}
}
