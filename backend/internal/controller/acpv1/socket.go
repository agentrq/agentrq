// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	zlog "github.com/rs/zerolog/log"
)

// upgrader accepts the bridge. CheckOrigin allows every origin, and that is
// correct here rather than lax: the endpoint authenticates with a bearer token,
// never a cookie, so no page can use it on somebody's behalf.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  32 << 10,
	WriteBufferSize: 32 << 10,
	Subprotocols:    []string{bridgeSubprotocol},
	CheckOrigin:     func(*http.Request) bool { return true },
}

// Handler serves GET /api/acp/v1, on the stdlib mux (Fiber cannot hijack the
// connection) and never also as a Fiber route.
type Handler struct {
	Hub       *Hub
	Auth      TokenAuth
	Enabled   func() bool                // acp.v1.enabled
	NewEngine func(c *connection) engine // sets OnFrame, OnControl and OnState
}

// resumeHeader is "<connection id>:<ACP frames the bridge received>". The id is
// what rand.Text mints; the count stops short of overflowing an int.
var resumeHeader = regexp.MustCompile(`^([A-Z2-7]{26}):([0-9]{1,18})$`)

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.Enabled() {
		http.NotFound(w, r)
		return
	}
	// Auth before the upgrade check, on purpose: the bridge's plain-GET
	// preflight then learns everything (404, 401/403, or 426 = go ahead)
	// before opening a socket, whose failed upgrade it cannot read.
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		w.Header().Set("WWW-Authenticate", `Bearer realm="agentrq"`)
		http.Error(w, "a workspace access token is required", http.StatusUnauthorized)
		return
	}
	workspaceID, userID, err := h.Auth.WorkspaceForToken(r.Context(), token)
	switch {
	case errors.Is(err, ErrForbidden):
		http.Error(w, "no access to this workspace", http.StatusForbidden)
		return
	case errors.Is(err, ErrArchived):
		http.Error(w, "workspace is archived", http.StatusForbidden)
		return
	case err != nil:
		w.Header().Set("WWW-Authenticate", `Bearer realm="agentrq"`)
		http.Error(w, "not a workspace access token", http.StatusUnauthorized)
		return
	}
	if !websocket.IsWebSocketUpgrade(r) {
		w.Header().Set("Upgrade", "websocket")
		http.Error(w, "this endpoint only speaks WebSocket", http.StatusUpgradeRequired)
		return
	}
	if !slices.Contains(websocket.Subprotocols(r), bridgeSubprotocol) {
		http.Error(w, "the bridge must offer the "+bridgeSubprotocol+" subprotocol", http.StatusBadRequest)
		return
	}
	meta, err := parseMeta(r.Header)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// A resume that names a connection we hold skips the limits: it is not a
	// new connection. Anything else is a fresh one, never an error (§14.3).
	var resumeFrom, fresh *connection
	var peerReceived int
	if m := resumeHeader.FindStringSubmatch(r.Header.Get("X-AgentRQ-Resume")); m != nil {
		if c, ok := h.Hub.lookup(m[1], workspaceID, userID); ok {
			peerReceived, _ = strconv.Atoi(m[2])
			resumeFrom = c
		}
	}
	if resumeFrom == nil {
		if fresh, err = h.Hub.admit(workspaceID, userID, meta); err != nil {
			http.Error(w, err.Error(), http.StatusTooManyRequests)
			return
		}
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		if fresh != nil {
			fresh.End(err)
		}
		return // Upgrade has answered already
	}

	if resumeFrom != nil {
		if err := resumeFrom.attach(ws, int64(peerReceived), true); err == nil {
			return
		}
		// Too much was missed, or it ended meanwhile: the old connection is
		// over, and the bridge starts its agent again on a fresh one.
		resumeFrom.End(errReplaced)
		fresh = h.Hub.replace(resumeFrom, meta)
	}
	h.start(fresh, ws)
}

// start runs a fresh connection's engine. The engine factory runs before the
// first frame can arrive, so its callbacks miss nothing.
func (h *Handler) start(c *connection, ws *websocket.Conn) {
	e := h.NewEngine(c)
	_ = c.attach(ws, 0, false) // cannot fail: nothing was sent and nothing ended it
	go func() {
		err := e.run(context.Background(), c)
		zlog.Info().Str("connection", c.ID()).Int64("workspace_id", c.WorkspaceID()).Err(err).Msg("acp v1: connection ended")
		c.End(err)
	}()
}

// parseMeta reads the bridge's transport headers (spec §5.1). Every value is
// cloned: header strings must not outlive the request they came in.
func parseMeta(h http.Header) (bridgeMeta, error) {
	m := bridgeMeta{PermissionTimeout: -1}
	for _, f := range []struct {
		name string
		max  int
		to   *string
	}{
		{"X-AgentRQ-Bridge-Version", 32, &m.BridgeVersion},
		{"X-AgentRQ-Agent", 128, &m.Agent},
		{"X-AgentRQ-Agent-Model", 128, &m.Model},
		{"X-AgentRQ-Auth-Method", 128, &m.AuthMethod},
		{"X-AgentRQ-Platform", 64, &m.Platform},
		{"X-AgentRQ-Cwd", 4096, &m.Cwd},
	} {
		v := strings.TrimSpace(h.Get(f.name))
		if len(v) > f.max {
			return m, errors.New(f.name + " is too long")
		}
		*f.to = strings.Clone(v)
	}
	if !isAbsolutePath(m.Cwd) {
		return m, errors.New("X-AgentRQ-Cwd must be the bridge's absolute working directory")
	}
	if n, err := strconv.Atoi(h.Get("X-AgentRQ-Max-Concurrency")); err == nil {
		m.MaxConcurrency = min(max(n, 1), 64) // clamped, as the gateway's flag is
	}
	if n, err := strconv.Atoi(h.Get("X-AgentRQ-Permission-Timeout")); err == nil && n >= 0 && n <= 24*60 {
		m.PermissionTimeout = time.Duration(n) * time.Minute
	}
	return m, nil
}

var windowsAbs = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

func isAbsolutePath(p string) bool {
	return strings.HasPrefix(p, "/") || windowsAbs.MatchString(p) || strings.HasPrefix(p, `\\`)
}
