// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
)

// A controller that either knows the session or does not. "Does not" is how a
// session belonging to somebody else arrives here — the read is scoped to the
// caller, so another person's session is simply not found.
type ticketCrud struct {
	crud.Controller
	session entity.SessionView
	err     error
	// asked records the scope the handler read with, which is the whole
	// authorisation check.
	asked []entity.GetSessionRequest
}

func (k *ticketCrud) GetSession(_ context.Context, req entity.GetSessionRequest) (*entity.GetSessionResponse, error) {
	k.asked = append(k.asked, req)
	if k.err != nil {
		return nil, k.err
	}
	return &entity.GetSessionResponse{Session: k.session}, nil
}

func ticketApp(c crud.Controller, tokens auth.TokenService) *fiber.App {
	h := &handler{crud: c, tokenSvc: tokens}
	app := fiber.New()
	app.Use(func(ctx fiber.Ctx) error {
		ctx.Locals("user_id", "user-1")
		return ctx.Next()
	})
	app.Post(_routePathSessionTerminalTicket, h.terminalTicket())
	return app
}

func ticketTokens(t *testing.T) auth.TokenService {
	t.Helper()
	return auth.NewTokenService(auth.TokenConfig{APISecret: "test-secret", MCPSecret: "test-secret"})
}

func postTicket(t *testing.T, app *fiber.App, sessionID string) *http.Response {
	t.Helper()
	res, err := app.Test(httptest.NewRequest(http.MethodPost, "/sessions/"+sessionID+"/terminal/ticket", nil))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// The ticket has to be valid for the session it was asked for, and for that
// one only. Minting it is pointless if the socket then refuses it, so this
// asserts against the real validator rather than against the string.
func TestTerminalTicketIsValidForThatSession(t *testing.T) {
	tokens := ticketTokens(t)
	id := monoflake.ID(500).String()
	c := &ticketCrud{session: entity.SessionView{ID: id, MachineID: monoflake.ID(11).String()}}

	res := postTicket(t, ticketApp(c, tokens), id)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}

	var body entity.TerminalTicketResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Ticket == "" {
		t.Fatal("no ticket in the response")
	}
	// Seconds, and it has to agree with the policy the ticket was minted
	// under — a caller that trusts this number and is told the wrong one
	// throws away a ticket that was still good, or keeps one that is not.
	if want := int(auth.TerminalTicketTTL.Seconds()); body.ExpiresIn != want {
		t.Errorf("expiresIn = %d, want %d", body.ExpiresIn, want)
	}

	// The numeric spelling, because that is the only form the socket has.
	numeric := strconv.FormatInt(monoflake.IDFromBase62(id).Int64(), 10)
	if _, err := tokens.ValidateTerminalTicket(body.Ticket, numeric); err != nil {
		t.Fatalf("the socket would refuse this ticket: %v", err)
	}
	if _, err := tokens.ValidateTerminalTicket(body.Ticket, "501"); err == nil {
		t.Error("a ticket for session 500 also opened 501")
	}

	// Read scoped to the caller: that read *is* the authorisation, so a
	// handler that minted without it, or with somebody else's id, would hand
	// out a working credential for a session the caller cannot see.
	if len(c.asked) != 1 {
		t.Fatalf("read the session %d times, want exactly 1", len(c.asked))
	}
	if c.asked[0].UserID != "user-1" || c.asked[0].SessionID != id {
		t.Errorf("read with scope %+v", c.asked[0])
	}
}

// A session the caller cannot see mints nothing. The read answers "not found"
// for somebody else's session, and that has to be the end of it.
func TestTerminalTicketRefusesASessionTheCallerCannotSee(t *testing.T) {
	tokens := ticketTokens(t)
	c := &ticketCrud{err: errors.New("not found")}

	res := postTicket(t, ticketApp(c, tokens), monoflake.ID(500).String())
	if res.StatusCode == http.StatusOK {
		t.Fatalf("status = %d — a ticket was minted for a session the caller cannot read", res.StatusCode)
	}

	var body struct {
		Ticket string `json:"ticket"`
	}
	_ = json.NewDecoder(res.Body).Decode(&body)
	if body.Ticket != "" {
		t.Error("the refusal carried a ticket")
	}
}
