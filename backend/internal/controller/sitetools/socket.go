// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package sitetools

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mustafaturan/monoflake"
	"github.com/rs/zerolog/log"
)

// Store is where shares live, narrowed for tests.
type Store interface {
	OwnsWorkspace(ctx context.Context, userID, workspaceID int64) (bool, error)
	// Upsert reports changed when the share is new or moved to another workspace.
	Upsert(ctx context.Context, s Share) (changed bool, err error)
	// Delete reports the workspace the deleted share was in.
	Delete(ctx context.Context, userID int64, origin string) (workspaceID int64, deleted bool, err error)
	ListForUser(ctx context.Context, userID int64) ([]Share, error)
}

// Share is one site shared into a workspace.
type Share struct {
	UserID, WorkspaceID           int64
	Origin, BrowserID, InstanceID string
	LastURL                       string
	Tools                         []Tool
	AlwaysAllow                   []string
}

// Handler serves the extension's browser socket.
type Handler struct {
	Hub   *Hub
	Store Store
	// Auth validates the ?ticket= and says whose browser this is.
	Auth      func(r *http.Request) (userID int64, err error)
	OnShare   func(userID, workspaceID int64, origin string)
	OnUnshare func(userID, workspaceID int64, origin string)
	// PingPeriod defaults to 30 s.
	PingPeriod time.Duration
}

const (
	writeWait = 10 * time.Second
	storeWait = 10 * time.Second
	// maxFrame fits the largest result plus its envelope.
	maxFrame = MaxResult + 64<<10
)

const defaultPingPeriod = 30 * time.Second

var browserIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

// upgrader allows every origin, as machine's does: the socket authenticates
// with a ticket in the URL, not a cookie, so there is no ambient credential
// for a foreign page to borrow.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(*http.Request) bool { return true },
}

// wsConn adapts a WebSocket to [Conn]. gorilla allows one writer at a time,
// and calls, refusals and pings all write.
type wsConn struct {
	ws *websocket.Conn

	mu     sync.Mutex
	closed bool
}

func (c *wsConn) Send(f Frame) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("sitetools: socket closed")
	}
	_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
	return c.ws.WriteJSON(f)
}

func (c *wsConn) ping() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("sitetools: socket closed")
	}
	return c.ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait))
}

func (c *wsConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	return c.ws.Close()
}

// ServeHTTP authenticates a browser and runs its connection.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	userID, err := h.Auth(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	browserID := r.URL.Query().Get("browser")
	if !browserIDPattern.MatchString(browserID) {
		http.Error(w, "browser must be 1-64 characters of A-Z, a-z, 0-9 and -", http.StatusBadRequest)
		return
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade has written its own response.
	}
	conn := &wsConn{ws: ws}
	if old := h.Hub.Add(userID, browserID, conn); old != nil {
		_ = old.Close()
	}
	defer func() {
		h.Hub.Remove(userID, browserID, conn)
		_ = conn.Close()
	}()

	// Outlives the request's context, which ends when ServeHTTP would.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := h.sendShares(ctx, conn, userID); err != nil {
		// An empty list would make the extension forget its shares.
		log.Warn().Err(err).Int64("user_id", userID).Msg("[sitetools] shares")
		return
	}

	pingPeriod := h.PingPeriod
	if pingPeriod == 0 {
		pingPeriod = defaultPingPeriod
	}
	ws.SetReadLimit(maxFrame)
	pongWait := 2 * pingPeriod
	_ = ws.SetReadDeadline(time.Now().Add(pongWait))
	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(pongWait))
	})
	go keepalive(ctx, conn, pingPeriod)

	s := &socket{h: h, conn: conn, userID: userID, browserID: browserID}
	for {
		var f Frame
		if err := ws.ReadJSON(&f); err != nil {
			return
		}
		s.handle(ctx, f)
	}
}

func (h *Handler) sendShares(ctx context.Context, conn *wsConn, userID int64) error {
	ctx, cancel := context.WithTimeout(ctx, storeWait)
	defer cancel()
	shares, err := h.Store.ListForUser(ctx, userID)
	if err != nil {
		return err
	}
	states := make([]ShareState, 0, len(shares))
	for _, s := range shares {
		allow := s.AlwaysAllow
		if allow == nil {
			allow = []string{}
		}
		states = append(states, ShareState{Origin: s.Origin, WorkspaceID: monoflake.ID(s.WorkspaceID).String(), AlwaysAllow: allow})
	}
	return conn.Send(Frame{Type: FrameShares, Shares: states})
}

func keepalive(ctx context.Context, conn *wsConn, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if conn.ping() != nil {
				return
			}
		}
	}
}

// socket is one connection's state.
type socket struct {
	h         *Handler
	conn      *wsConn
	userID    int64
	browserID string
}

func (s *socket) handle(ctx context.Context, f Frame) {
	ctx, cancel := context.WithTimeout(ctx, storeWait)
	defer cancel()
	switch f.Type {
	case FrameAnnounce:
		if err := s.announce(ctx, f); err != nil {
			s.refuse(f.Origin, err)
		}
	case FrameWithdraw:
		ws, deleted, err := s.h.Store.Delete(ctx, s.userID, f.Origin)
		if err != nil {
			s.refuse(f.Origin, errTryAgain(err))
			return
		}
		if deleted {
			s.h.OnUnshare(s.userID, ws, f.Origin)
		}
	case FrameResult:
		s.h.Hub.Deliver(f)
	}
}

func (s *socket) announce(ctx context.Context, f Frame) error {
	if err := checkOrigin(f.Origin); err != nil {
		return err
	}
	workspaceID := monoflake.IDFromBase62(f.WorkspaceID).Int64()
	owns, err := s.h.Store.OwnsWorkspace(ctx, s.userID, workspaceID)
	if err != nil {
		return errTryAgain(err)
	}
	if !owns || workspaceID == 0 {
		return errors.New("no such workspace")
	}
	if err := ValidateTools(f.Tools); err != nil {
		return err
	}
	changed, err := s.h.Store.Upsert(ctx, Share{
		UserID:      s.userID,
		WorkspaceID: workspaceID,
		Origin:      f.Origin,
		BrowserID:   s.browserID,
		InstanceID:  s.h.Hub.InstanceID(),
		LastURL:     f.LastURL,
		Tools:       f.Tools,
	})
	if err != nil {
		return errTryAgain(err)
	}
	if changed {
		s.h.OnShare(s.userID, workspaceID, f.Origin)
	}
	return nil
}

func (s *socket) refuse(origin string, err error) {
	_ = s.conn.Send(Frame{Type: FrameRefused, Origin: origin, Error: err.Error()})
}

// errTryAgain logs a storage failure and tells the browser only to retry.
func errTryAgain(err error) error {
	log.Warn().Err(err).Msg("[sitetools] store")
	return errors.New("could not save the share; try again")
}

// checkOrigin accepts an origin as `new URL(url).origin` spells it:
// https://host[:port], or http://localhost[:port], and nothing after.
func checkOrigin(origin string) error {
	u, err := url.Parse(origin)
	if err != nil || u.Hostname() == "" || origin != u.Scheme+"://"+u.Host || u.Host != strings.ToLower(u.Host) {
		return errors.New("not an origin: want https://host[:port] or http://localhost[:port]")
	}
	if u.Scheme != "https" && (u.Scheme != "http" || u.Hostname() != "localhost") {
		return errors.New("only https sites, and http://localhost, can be shared")
	}
	return nil
}
