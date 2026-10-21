// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/backend/internal/service/security"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	zlog "github.com/rs/zerolog/log"
)

const (
	// _adminCookie is the operator's session, apart from any user's: it is
	// scoped to /admin/v1 and is never accepted as a user session.
	_adminCookie     = "adm"
	_adminSessionTTL = 30 * time.Minute
	// _adminAudience marks an admin token, so no other token of the server's
	// could pass for one even if it shared the key.
	_adminAudience = "agentrq-admin"
	// _adminLoginWindow is how often one client may try the root token: every
	// try counts, right or wrong, since it is the only place it is accepted.
	_adminLoginWindow = 10 * time.Minute
)

// loginAttempts remembers each client's last admin sign-in attempt. The zero
// value is ready to use.
type loginAttempts struct {
	mu        sync.Mutex
	last      map[string]time.Time
	nextPrune time.Time
}

// allow records an attempt from key at now, and reports whether it is the
// first in the window. Entries are pruned at most once a window, so a flood
// of addresses costs one sweep per window, not one per attempt.
func (l *loginAttempts) allow(key string, now time.Time, window time.Duration) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.last == nil {
		l.last = make(map[string]time.Time)
	}
	if !now.Before(l.nextPrune) {
		for k, t := range l.last {
			if now.Sub(t) >= window {
				delete(l.last, k)
			}
		}
		l.nextPrune = now.Add(window)
	}
	if t, ok := l.last[key]; ok && now.Sub(t) < window {
		return false, window - now.Sub(t)
	}
	l.last[key] = now
	return true, 0
}

// clientIP names the client the sign-in throttle counts tries for.
func clientIP(c *fiber.Ctx) string {
	return proxiedClient(c.Context().RemoteIP(), c.Get("X-Real-IP"))
}

// proxiedClient believes X-Real-IP only from a proxy on a loopback or private
// address, which sets it itself; from anyone else it is whatever the client
// sent. X-Forwarded-For is never read: the client writes its first entries,
// and trusting them gave every try a fresh key.
func proxiedClient(peer net.IP, realIP string) string {
	if realIP != "" && (peer.IsLoopback() || peer.IsPrivate()) {
		return realIP
	}
	return peer.String()
}

// registerAdminRoutes adds the operator's routes, apart from the users' API:
// the sign-in under /auth/admin, and everything else under /admin/v1 behind
// the admin session. The user session middleware is on /api/v1 alone, so
// neither ever reaches it.
func (h *handler) registerAdminRoutes(root fiber.Router) {
	login := root.Group("/auth/admin")
	login.Post("/login", h.adminLogin())
	login.Post("/logout", h.adminLogout())

	r := root.Group("/admin/v1", h.adminMiddleware())
	r.Get("/stats", h.getAdminStats())
}

// adminMiddleware refuses a request without an admin session.
func (h *handler) adminMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if !h.isAdmin(c) {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "admin session required"})
		}
		return c.Next()
	}
}

func (h *handler) adminCookiePath() string {
	return h.basePath + "/admin/v1"
}

// adminKey signs admin tokens. Derived from the root access token rather than
// the users' JWT secret, so a user token can never verify as an admin one, and
// rotating the root token ends every admin session. Stateless, so any
// instance can check a token another issued.
func (h *handler) adminKey() []byte {
	mac := hmac.New(sha256.New, []byte(h.rootToken))
	mac.Write([]byte(_adminAudience))
	return mac.Sum(nil)
}

// adminToken mints a short-lived admin JWT expiring at expiresAt.
func (h *handler) adminToken(expiresAt time.Time) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Audience:  jwt.ClaimStrings{_adminAudience},
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
	})
	// HS256 with a byte key cannot fail to sign.
	signed, _ := token.SignedString(h.adminKey())
	return signed
}

// isAdmin reports whether the request carries an unexpired admin token, as a
// bearer or as the admin cookie. Never true when no root token is configured.
func (h *handler) isAdmin(c *fiber.Ctx) bool {
	if h.rootToken == "" {
		return false
	}
	token, ok := strings.CutPrefix(c.Get(fiber.HeaderAuthorization), "Bearer ")
	if !ok {
		token = c.Cookies(_adminCookie)
	}
	_, err := jwt.Parse(token, func(*jwt.Token) (any, error) { return h.adminKey(), nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithAudience(_adminAudience),
		jwt.WithExpirationRequired(),
	)
	return err == nil
}

// adminLogin exchanges the root access token for a short-lived admin JWT, set
// as the admin cookie and returned for scripts to send as a bearer.
func (h *handler) adminLogin() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if ok, wait := h.adminLogins.allow(clientIP(c), time.Now(), _adminLoginWindow); !ok {
			c.Set(fiber.HeaderRetryAfter, strconv.Itoa(int(wait.Seconds())+1))
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{"error": "too many attempts"})
		}
		var req struct {
			RootToken string `json:"rootToken"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid payload"})
		}
		if h.rootToken == "" || !security.SecureCompare(req.RootToken, h.rootToken) {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid root token"})
		}
		expiresAt := time.Now().Add(_adminSessionTTL)
		token := h.adminToken(expiresAt)
		c.Cookie(h.sessionCookie(_adminCookie, token, expiresAt, h.adminCookiePath()))
		return c.JSON(fiber.Map{"token": token})
	}
}

// adminLogout ends the admin session in this browser.
func (h *handler) adminLogout() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Cookie(h.sessionCookie(_adminCookie, "", time.Now().Add(-time.Hour), h.adminCookiePath()))
		return c.JSON(fiber.Map{"status": "ok"})
	}
}

// getAdminStats serves the operator's per-user figures: the MCP streams open
// on this instance, and the machines online.
func (h *handler) getAdminStats() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		ctx, cancel := newContext(c)
		defer cancel()
		machines, err := h.crud.CountOnlineMachinesPerUser(ctx)
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to count online machines")
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		return c.Send(mapper.FromAdminStatsToHTTPResponse(h.mcpManager.ConnectionsByUser(), machines))
	}
}
