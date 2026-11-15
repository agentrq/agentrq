// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
	zlog "github.com/rs/zerolog/log"
)

// oauth2LoginViews are the sign-in buttons for the providers the operator
// added; never null, so the login page can read it as a list.
func (h *handler) oauth2LoginViews() []view.OAuth2LoginProvider {
	out := make([]view.OAuth2LoginProvider, len(h.oauth2Logins))
	for i, p := range h.oauth2Logins {
		out[i] = view.OAuth2LoginProvider{ID: p.ID, Name: p.Name, IconURL: p.IconURL}
	}
	return out
}

func (h *handler) oauth2Provider(id string) (OAuth2Login, bool) {
	for _, p := range h.oauth2Logins {
		if p.ID == id {
			return p, true
		}
	}
	return OAuth2Login{}, false
}

// oauth2State names the provider a state token was made for, apart from
// Google's and GitHub's, whatever id the operator picks.
func oauth2State(id string) string { return "oauth2:" + id }

func (h *handler) oauth2Login() fiber.Handler {
	return func(c *fiber.Ctx) error {
		p, ok := h.oauth2Provider(c.Params("id"))
		if !ok {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "no such sign-in provider"})
		}
		redirectURL := h.sanitizeRedirectURL(c.Query("redirect_url", h.basePath+"/"))
		state, err := h.tokenSvc.CreateOAuthStateToken(redirectURL, oauth2State(p.ID))
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to generate state"})
		}
		return c.Redirect(p.Service.GetAuthURL(state))
	}
}

// oauth2Callback signs in with the code the provider sent back. The state
// must be the one oauth2Login made for this provider, so a link that starts
// a sign-in somewhere else cannot finish one here.
func (h *handler) oauth2Callback() fiber.Handler {
	return func(c *fiber.Ctx) error {
		p, ok := h.oauth2Provider(c.Params("id"))
		if !ok {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "no such sign-in provider"})
		}
		redirectURL, err := h.tokenSvc.ValidateOAuthStateToken(c.Query("state"), oauth2State(p.ID))
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid state"})
		}
		ctx, cancel := newContext(c)
		defer cancel()

		user, err := p.Service.Exchange(ctx, c.Query("code"))
		if errors.Is(err, auth.ErrEmailNotVerified) {
			return h.refuseSignIn(c, "oauth2EmailUnverified")
		}
		if err != nil {
			zlog.Error().Err(err).Str("provider", p.ID).Msg("OAuth2 exchange failed")
			return sendError(c, err)
		}

		dbUser, err := h.crud.FindOrCreateUser(ctx, entity.FindOrCreateUserRequest{
			Email:   user.Email,
			Name:    user.Name,
			Picture: user.Picture,
		})
		if err != nil {
			zlog.Error().Err(err).Str("provider", p.ID).Msg("Failed to sync OAuth2 user")
			return sendError(c, err)
		}

		userID := monoflake.ID(dbUser.User.ID).String()
		tokenString, err := h.tokenSvc.CreateToken(userID, user.Email, user.Name, dbUser.User.Picture)
		if err != nil {
			zlog.Error().Err(err).Str("provider", p.ID).Msg("Failed to sign OAuth2 token")
			return sendError(c, err)
		}

		h.issueSession(c, tokenString, userID)
		return c.Redirect(h.sanitizeRedirectURL(redirectURL))
	}
}

// sendError answers with the generic mapping of err.
func sendError(c *fiber.Ctx, err error) error {
	c.Set(_headerContentType, _mimeJSON)
	e, status := mapper.FromErrorToHTTPResponse(err)
	return c.Status(status).Send(e)
}
