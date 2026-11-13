// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/mustafaturan/monoflake"
)

// accessResource is the supervisor resource an access token grants, "" when
// the token is no supervisor access token: a refresh token or an
// authorization code names the resource too, and a token with no subject acts
// for no one.
func accessResource(claims *auth.Claims) string {
	if claims == nil || claims.Subject == "" {
		return ""
	}
	for _, aud := range claims.Audience {
		if aud == "refresh" || aud == "authorization_code" {
			return ""
		}
	}
	return supervisorResource(claims.Audience)
}

// userInfo is the OpenID Connect userinfo response (OIDC Core §5.3.2); id is
// sub again, under the name the rest of the API uses.
type userInfo struct {
	Sub     string `json:"sub"`
	ID      string `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

// oauthUserInfoHandler tells a client who a supervisor access token belongs
// to, so it needs no copy of the signing secret to learn it. Only the
// Authorization header is read: a cookie or a query token is not a client
// presenting the token it was granted.
func (h *handler) oauthUserInfoHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Cache-Control", "no-store")

		tokenStr, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || tokenStr == "" {
			invalidToken(w)
			return
		}
		claims, err := h.tokenSvc.ValidateToken(tokenStr)
		if err != nil || accessResource(claims) == "" {
			invalidToken(w)
			return
		}
		user, err := h.crud.FindUserByID(r.Context(), monoflake.IDFromBase62(claims.Subject).Int64())
		if err != nil || user.ID == 0 {
			invalidToken(w)
			return
		}

		id := monoflake.ID(user.ID).String()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(userInfo{Sub: id, ID: id, Email: user.Email, Name: user.Name, Picture: user.Picture})
	})
}

// invalidToken is the RFC 6750 §3 answer to every refusal, whatever the
// reason: telling the caller which check failed helps only someone probing.
func invalidToken(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
	w.WriteHeader(http.StatusUnauthorized)
}
