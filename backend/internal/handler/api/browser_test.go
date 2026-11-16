// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/agentrq/agentrq/backend/internal/service/auth"
)

func browserTicketApp(tokens auth.TokenService) *fiber.App {
	h := &handler{tokenSvc: tokens}
	app := fiber.New()
	app.Use(func(ctx fiber.Ctx) error {
		ctx.Locals("user_id", "user-1")
		return ctx.Next()
	})
	app.Post(_routePathBrowserTicket, h.browserTicket())
	return app
}

func TestBrowserTicketOpensTheBrowserSocket(t *testing.T) {
	tokens := ticketTokens(t)
	res, err := browserTicketApp(tokens).Test(httptest.NewRequest(http.MethodPost, "/browser/ticket", nil))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	raw, _ := io.ReadAll(res.Body)
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body["expiresIn"] != float64(60) {
		t.Errorf("expiresIn = %v in %s, want 60", body["expiresIn"], raw)
	}
	ticket, _ := body["ticket"].(string)
	claims, err := tokens.ValidateBrowserTicket(ticket)
	if err != nil {
		t.Fatalf("the ticket does not open the browser socket: %v", err)
	}
	if claims.Subject != "user-1" {
		t.Errorf("subject = %q, want the caller", claims.Subject)
	}
}

type failingBrowserTickets struct{ auth.TokenService }

func (failingBrowserTickets) CreateBrowserTicket(string) (string, error) {
	return "", errors.New("signing failed")
}

func TestBrowserTicketMintFailure(t *testing.T) {
	res, err := browserTicketApp(failingBrowserTickets{}).Test(httptest.NewRequest(http.MethodPost, "/browser/ticket", nil))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode < 500 {
		t.Fatalf("status = %d, want a server error", res.StatusCode)
	}
}
