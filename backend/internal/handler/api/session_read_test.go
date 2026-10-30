// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
)

// sessionReadCrud answers the session reads, scoped like the real one: it
// records the scope it was asked with and answers err when set.
type sessionReadCrud struct {
	crud.Controller
	err      error
	session  entity.SessionView
	sessions []entity.SessionView
	asked    []string
}

func (s *sessionReadCrud) GetSession(_ context.Context, req entity.GetSessionRequest) (*entity.GetSessionResponse, error) {
	s.asked = append(s.asked, req.UserID+"/"+req.SessionID)
	if s.err != nil {
		return nil, s.err
	}
	return &entity.GetSessionResponse{Session: s.session}, nil
}

func (s *sessionReadCrud) ListSessions(_ context.Context, req entity.ListSessionsRequest) (*entity.ListSessionsResponse, error) {
	s.asked = append(s.asked, req.UserID+"/"+req.MachineID)
	if s.err != nil {
		return nil, s.err
	}
	return &entity.ListSessionsResponse{Sessions: s.sessions}, nil
}

// sessionReadApp mounts every session route the way the server does.
func sessionReadApp(c crud.Controller, tokens auth.TokenService) *fiber.App {
	app := fiber.New()
	app.Use(func(ctx *fiber.Ctx) error {
		ctx.Locals("user_id", "user-1")
		return ctx.Next()
	})
	h := &handler{router: app.Group(""), crud: c, tokenSvc: tokens}
	h.registerSessionRoutes()
	return app
}

func sessionRequest(t *testing.T, app *fiber.App, method, target string) *http.Response {
	t.Helper()
	res, err := app.Test(httptest.NewRequest(method, target, nil))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestGetSession(t *testing.T) {
	id := monoflake.ID(500).String()

	t.Run("returns the caller's session", func(t *testing.T) {
		c := &sessionReadCrud{session: entity.SessionView{ID: id, MachineID: monoflake.ID(11).String(), Kind: "claude-code"}}
		res := sessionRequest(t, sessionReadApp(c, nil), http.MethodGet, "/sessions/"+id)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", res.StatusCode)
		}
		var body entity.GetSessionResponse
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Session.ID != id || body.Session.Kind != "claude-code" {
			t.Errorf("session = %+v", body.Session)
		}
		if len(c.asked) != 1 || c.asked[0] != "user-1/"+id {
			t.Errorf("read with scope %v, want user-1/%s", c.asked, id)
		}
	})

	t.Run("a session the caller cannot see is not found", func(t *testing.T) {
		c := &sessionReadCrud{err: fmt.Errorf("get session: %w", base.ErrNotFound)}
		res := sessionRequest(t, sessionReadApp(c, nil), http.MethodGet, "/sessions/"+id)
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", res.StatusCode)
		}
	})
}

func TestListSessions(t *testing.T) {
	machine := monoflake.ID(11).String()

	t.Run("returns the machine's sessions", func(t *testing.T) {
		c := &sessionReadCrud{sessions: []entity.SessionView{{ID: "a"}, {ID: "b"}}}
		res := sessionRequest(t, sessionReadApp(c, nil), http.MethodGet, "/machines/"+machine+"/sessions")
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", res.StatusCode)
		}
		var body entity.ListSessionsResponse
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Sessions) != 2 || body.Sessions[0].ID != "a" {
			t.Errorf("sessions = %+v", body.Sessions)
		}
		if len(c.asked) != 1 || c.asked[0] != "user-1/"+machine {
			t.Errorf("listed with scope %v, want user-1/%s", c.asked, machine)
		}
	})

	t.Run("a failed read is an error", func(t *testing.T) {
		c := &sessionReadCrud{err: errors.New("db down")}
		res := sessionRequest(t, sessionReadApp(c, nil), http.MethodGet, "/machines/"+machine+"/sessions")
		if res.StatusCode != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", res.StatusCode)
		}
	})
}

func TestKillSession_ASessionTheCallerCannotSee(t *testing.T) {
	// No machine registry: a refused read must stop before any kill is sent.
	c := &sessionReadCrud{err: fmt.Errorf("get session: %w", base.ErrNotFound)}
	res := sessionRequest(t, sessionReadApp(c, nil), http.MethodDelete, "/sessions/"+monoflake.ID(500).String())
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", res.StatusCode)
	}
}

// failingTicketTokens cannot mint a terminal ticket.
type failingTicketTokens struct{ auth.TokenService }

func (failingTicketTokens) CreateTerminalTicket(string, string) (string, error) {
	return "", errors.New("signing failed")
}

func TestTerminalTicket_MintFailure(t *testing.T) {
	id := monoflake.ID(500).String()
	c := &sessionReadCrud{session: entity.SessionView{ID: id}}
	res := sessionRequest(t, sessionReadApp(c, failingTicketTokens{}), http.MethodPost, "/sessions/"+id+"/terminal/ticket")
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", res.StatusCode)
	}
	var body struct {
		Ticket string `json:"ticket"`
	}
	_ = json.NewDecoder(res.Body).Decode(&body)
	if body.Ticket != "" {
		t.Error("a failed mint still returned a ticket")
	}
}
