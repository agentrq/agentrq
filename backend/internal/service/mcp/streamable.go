// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/agentrq/agentrq/backend/internal/service/mcp/jsonrpc"
)

// The Streamable HTTP transport. A POST carries client messages and is
// answered with an SSE stream of the responses, or 202 when it carried no
// call. A stateful session's GET is the stream the server pushes on, and its
// DELETE ends it. A stateless server answers every POST on its own and
// refuses GET and DELETE.

const (
	headerSessionID       = "Mcp-Session-Id"
	headerProtocolVersion = "Mcp-Protocol-Version"
	headerLastEventID     = "Last-Event-ID"
	headerMethod          = "Mcp-Method"
	headerName            = "Mcp-Name"

	// versionWithoutHeader is what a request that names no revision is read
	// as: the last revision before the header existed.
	versionWithoutHeader = "2025-03-26"
)

// postKeepalive is how long a POST waits on an unanswered call before it
// sends an SSE comment, and again after each. A client or proxy that finds
// a connection silent for minutes drops it: Bun's fetch, under Claude Code,
// gives up after five with "The operation timed out.", and an elicit waits
// up to an hour. A call that named a progressToken gets a progress
// notification instead: Claude Code also aborts a call that sends no
// response or progress for five minutes, and a comment is neither.
var postKeepalive = 25 * time.Second

// DefaultMaxRequestBodyBytes caps a POST body unless the options say
// otherwise.
const DefaultMaxRequestBodyBytes = 4 << 20

// StreamableHTTPOptions configures a StreamableHTTPHandler.
type StreamableHTTPOptions struct {
	// Stateless answers every POST on its own: no session, no GET stream, and
	// revision 2026-07-28 on offer.
	Stateless bool
	// SessionIDs mints the id of each stateful session and reads it back, so
	// a session outlives this process. Nil keeps sessions in memory only.
	SessionIDs SessionIDs
	// MaxRequestBodyBytes caps a POST body: 0 is the default, negative none.
	MaxRequestBodyBytes int64
	// CrossOriginProtection, when set, refuses the requests it rejects.
	CrossOriginProtection *http.CrossOriginProtection
	// DisableLocalhostProtection serves a request that reached a loopback
	// address under another host's name, which is refused by default as
	// DNS rebinding.
	DisableLocalhostProtection bool
	// OnStreamOpen, when set, runs each time a session's client opens its
	// push stream, once the stream carries messages: what the session could
	// not be sent while it had none can go out then.
	OnStreamOpen func(ctx context.Context, sessionID string)
	// OnResume, when set, runs each time a session this process did not hold
	// is rebuilt from its id: its client reopened its push stream after a
	// restart, a redeploy or an eviction. r is the request that reopened it.
	OnResume func(r *http.Request, session *ServerSession)
}

// SessionInfo is what a stateful session's id records about it: enough to
// carry on serving it after the process that created it is gone. The
// revision is the one the client asked for in its handshake; the one served
// follows from it.
type SessionInfo struct {
	ProtocolVersion string
	ClientName      string
	ClientVersion   string
}

// maxSessionField caps each field a session id records, so a client cannot
// grow its own session id past what a proxy passes in a header.
const maxSessionField = 64

// sessionInfoOf is what a session's id records of its handshake.
func sessionInfoOf(p *InitializeParams) SessionInfo {
	var info SessionInfo
	if p == nil {
		return info
	}
	if len(p.ProtocolVersion) <= maxSessionField {
		info.ProtocolVersion = p.ProtocolVersion
	}
	if p.ClientInfo != nil {
		info.ClientName = capField(p.ClientInfo.Name)
		info.ClientVersion = capField(p.ClientInfo.Version)
	}
	return info
}

// capField keeps at most maxSessionField bytes of s, whole characters only.
func capField(s string) string {
	if len(s) <= maxSessionField {
		return s
	}
	s = s[:maxSessionField]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// SessionIDs mints and reads back stateful session ids. An id is not a
// credential: the request carrying it has already been authenticated, and
// Resolve refuses an id minted for anyone else.
type SessionIDs interface {
	Issue(ctx context.Context, info SessionInfo) (string, error)
	Resolve(ctx context.Context, id string) (SessionInfo, error)
}

// StreamableHTTPHandler serves a Server over Streamable HTTP.
type StreamableHTTPHandler struct {
	getServer func(*http.Request) *Server
	opts      StreamableHTTPOptions

	mu       sync.Mutex
	sessions map[string]*httpSession
	ended    endedIDs

	keepalive time.Duration // postKeepalive, read once
}

// maxEnded is how many deleted session ids a handler remembers. Past it the
// oldest is forgotten, which, like a restart, only lets the client that
// deleted it reopen it.
var maxEnded = 10000

// endedIDs are the session ids clients deleted, by hash, the most recent
// maxEnded of them.
type endedIDs struct {
	ids  map[[sha256.Size]byte]struct{}
	ring [][sha256.Size]byte
	next int
}

func (e *endedIDs) add(id string) {
	k := sha256.Sum256([]byte(id))
	if _, ok := e.ids[k]; ok {
		return
	}
	if e.ids == nil {
		e.ids = map[[sha256.Size]byte]struct{}{}
	}
	if len(e.ring) < maxEnded {
		e.ring = append(e.ring, k)
	} else {
		delete(e.ids, e.ring[e.next])
		e.ring[e.next] = k
		e.next = (e.next + 1) % len(e.ring)
	}
	e.ids[k] = struct{}{}
}

func (e *endedIDs) has(id string) bool {
	_, ok := e.ids[sha256.Sum256([]byte(id))]
	return ok
}

// NewStreamableHTTPHandler serves the server getServer returns for each
// request.
func NewStreamableHTTPHandler(getServer func(*http.Request) *Server, opts *StreamableHTTPOptions) *StreamableHTTPHandler {
	h := &StreamableHTTPHandler{
		getServer: getServer,
		sessions:  map[string]*httpSession{},
		keepalive: postKeepalive,
	}
	if opts != nil {
		h.opts = *opts
	}
	if h.opts.MaxRequestBodyBytes == 0 {
		h.opts.MaxRequestBodyBytes = DefaultMaxRequestBodyBytes
	}
	return h
}

func (h *StreamableHTTPHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if !h.opts.DisableLocalhostProtection {
		if local, ok := req.Context().Value(http.LocalAddrContextKey).(net.Addr); ok && local != nil {
			if isLoopback(local.String()) && !isLoopback(req.Host) {
				http.Error(w, fmt.Sprintf("Forbidden: invalid Host header %q", req.Host), http.StatusForbidden)
				return
			}
		}
	}
	if h.opts.CrossOriginProtection != nil {
		if err := h.opts.CrossOriginProtection.Check(req); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
	}
	if req.Body != nil && h.opts.MaxRequestBodyBytes > 0 {
		req.Body = http.MaxBytesReader(w, req.Body, h.opts.MaxRequestBodyBytes)
	}

	server := h.getServer(req)
	version := req.Header.Get(headerProtocolVersion)
	supported := supportedProtocolVersions
	if server != nil {
		supported = server.versions
	}
	if version != "" && !slices.Contains(supported, version) && version < ProtocolVersion20260728 {
		http.Error(w, fmt.Sprintf("Bad Request: Unsupported protocol version (supported versions: %s)", strings.Join(supported, ",")), http.StatusBadRequest)
		return
	}

	if h.opts.Stateless {
		if req.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		if !h.checkPOSTHeaders(w, req) {
			return
		}
		if server == nil {
			http.Error(w, "no server available", http.StatusBadRequest)
			return
		}
		h.servePOST(w, req, server, nil)
		return
	}

	switch req.Method {
	case http.MethodGet:
		h.serveGET(w, req)
	case http.MethodPost:
		if !h.checkPOSTHeaders(w, req) {
			return
		}
		var sess *httpSession
		if id := req.Header.Get(headerSessionID); id != "" {
			var ok bool
			if sess, ok = h.lookup(w, req, id); !ok {
				return
			}
			server = sess.session.server
		} else if server == nil {
			http.Error(w, "no server available", http.StatusBadRequest)
			return
		}
		h.servePOST(w, req, server, sess)
	case http.MethodDelete:
		id := req.Header.Get(headerSessionID)
		if id == "" {
			http.Error(w, "Bad Request: DELETE requires an Mcp-Session-Id header", http.StatusBadRequest)
			return
		}
		sess, ok := h.lookup(w, req, id)
		if !ok {
			return
		}
		h.end(sess)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

// checkPOSTHeaders refuses a POST that is not JSON, or whose client cannot
// read both answers a POST may get.
func (h *StreamableHTTPHandler) checkPOSTHeaders(w http.ResponseWriter, req *http.Request) bool {
	if mediaType, _, err := mime.ParseMediaType(req.Header.Get("Content-Type")); err != nil || mediaType != "application/json" {
		http.Error(w, "Content-Type must be 'application/json'", http.StatusUnsupportedMediaType)
		return false
	}
	if jsonOK, streamOK := accepts(req.Header.Values("Accept")); !jsonOK || !streamOK {
		http.Error(w, "Accept must contain both 'application/json' and 'text/event-stream'", http.StatusBadRequest)
		return false
	}
	return true
}

// accepts reports whether an Accept header takes JSON and SSE.
func accepts(values []string) (jsonOK, streamOK bool) {
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			base, _, _ := strings.Cut(strings.TrimSpace(part), ";")
			switch strings.ToLower(strings.TrimSpace(base)) {
			case "application/json", "application/*":
				jsonOK = true
			case "text/event-stream", "text/*":
				streamOK = true
			case "*/*":
				jsonOK, streamOK = true, true
			}
		}
	}
	return jsonOK, streamOK
}

// lookup finds the session id names. One this process does not hold is
// rebuilt from the id only by a GET, its client reopening its push stream,
// and only when SessionIDs minted the id for this server and nobody deleted
// it. A POST or DELETE naming it is 404, as before ids were signed: a client
// whose push stream is gone for good then starts a new session, push stream
// and all, rather than carrying on without one.
func (h *StreamableHTTPHandler) lookup(w http.ResponseWriter, req *http.Request, id string) (*httpSession, bool) {
	h.mu.Lock()
	sess := h.sessions[id]
	deleted := h.ended.has(id)
	h.mu.Unlock()
	if sess != nil {
		return sess, true
	}
	if h.opts.SessionIDs == nil || deleted || req.Method != http.MethodGet {
		http.Error(w, "session not found", http.StatusNotFound)
		return nil, false
	}
	info, err := h.opts.SessionIDs.Resolve(req.Context(), id)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return nil, false
	}
	server := h.getServer(req)
	if server == nil {
		http.Error(w, "no server available", http.StatusBadRequest)
		return nil, false
	}
	sess, rebuilt := h.resume(server, id, info)
	if sess == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return nil, false
	}
	if rebuilt && h.opts.OnResume != nil {
		h.opts.OnResume(req, sess.session)
	}
	return sess, true
}

// resume rebuilds a session from what its id records: initialized, speaking
// the revision it negotiated, as the client it said it was, and reports
// whether it did. Another request may have rebuilt it first, and then that
// session is the one returned; nil when the id was deleted while it was being
// read.
func (h *StreamableHTTPHandler) resume(server *Server, id string, info SessionInfo) (*httpSession, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if sess := h.sessions[id]; sess != nil {
		return sess, false
	}
	if h.ended.has(id) {
		return nil, false
	}
	sess := h.newSession(server, id)
	p := &InitializeParams{ProtocolVersion: info.ProtocolVersion, Capabilities: &ClientCapabilities{}}
	if info.ClientName != "" {
		p.ClientInfo = &Implementation{Name: info.ClientName, Version: info.ClientVersion}
	}
	sess.session.state = sessionState{
		initialize:  p,
		initialized: true,
		negotiated:  negotiatedVersion(info.ProtocolVersion, server.versions),
	}
	h.sessions[id] = sess
	server.addSession(sess.session)
	return sess, true
}

// newSession is a session of server with no streams yet. h.mu is held when
// id is set.
func (h *StreamableHTTPHandler) newSession(server *Server, id string) *httpSession {
	sess := &httpSession{
		handler:   h,
		id:        id,
		stateless: h.opts.Stateless,
		byRequest: map[jsonrpc.ID]*stream{},
		done:      make(chan struct{}),
	}
	sess.session = newServerSession(server, id, sess, transportVersions(server.versions, h.opts.Stateless))
	sess.session.onClose = func() { h.forget(sess) }
	return sess
}

// forget drops a closed session from the handler.
func (h *StreamableHTTPHandler) forget(sess *httpSession) {
	id := sess.sessionID()
	if id == "" {
		return
	}
	h.mu.Lock()
	if h.sessions[id] == sess {
		delete(h.sessions, id)
	}
	h.mu.Unlock()
}

// end closes a session its client deleted, and keeps its id from being
// rebuilt while this process runs.
func (h *StreamableHTTPHandler) end(sess *httpSession) {
	if h.opts.SessionIDs != nil {
		id := sess.sessionID()
		h.mu.Lock()
		h.ended.add(id)
		h.mu.Unlock()
	}
	_ = sess.session.Close()
}

// serveGET holds open the stream a stateful session is pushed to.
func (h *StreamableHTTPHandler) serveGET(w http.ResponseWriter, req *http.Request) {
	if _, streamOK := accepts(req.Header.Values("Accept")); !streamOK {
		http.Error(w, "Accept must contain 'text/event-stream' for GET requests", http.StatusBadRequest)
		return
	}
	id := req.Header.Get(headerSessionID)
	if id == "" {
		http.Error(w, "Bad Request: GET requires an Mcp-Session-Id header", http.StatusBadRequest)
		return
	}
	sess, ok := h.lookup(w, req, id)
	if !ok {
		return
	}
	if len(req.Header.Values(headerLastEventID)) > 0 {
		eventID := req.Header.Get(headerLastEventID)
		if !validEventID(eventID) {
			http.Error(w, fmt.Sprintf("malformed Last-Event-ID %q", eventID), http.StatusBadRequest)
			return
		}
		http.Error(w, "stream replay unsupported", http.StatusBadRequest)
		return
	}
	st := sess.attachStandalone(w)
	if st == nil {
		http.Error(w, "stream ID conflicts with ongoing stream", http.StatusConflict)
		return
	}
	defer sess.detachStandalone(st)
	// The stream holds what is pushed to it until open, so nothing else
	// writes to w while its headers are set.
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Connection", "keep-alive")
	st.open()
	if h.opts.OnStreamOpen != nil {
		h.opts.OnStreamOpen(req.Context(), id)
	}
	select {
	case <-req.Context().Done():
	case <-sess.done:
	}
}

// validEventID reports whether an SSE event id has the form this transport
// would give one: <stream>_<index>.
func validEventID(id string) bool {
	parts := strings.Split(id, "_")
	if len(parts) != 2 {
		return false
	}
	idx, err := strconv.Atoi(parts[1])
	return err == nil && idx >= 0
}

// post is what one POST carried.
type post struct {
	messages   []jsonrpc.Message
	calls      map[jsonrpc.ID]bool
	progress   map[jsonrpc.ID]any // the progressToken a call named
	initialize *jsonrpc.Request
	listen     bool
	modern     bool // any request on 2026-07-28 or later
	hasInit    bool
	hasInitd   bool
}

// servePOST handles a POST: on sess, or with none on a new session, which
// is kept only when the POST initializes it on a stateful server.
func (h *StreamableHTTPHandler) servePOST(w http.ResponseWriter, req *http.Request, server *Server, sess *httpSession) {
	if len(req.Header.Values(headerLastEventID)) > 0 {
		http.Error(w, "can't send Last-Event-ID for POST request", http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			http.Error(w, fmt.Sprintf("request body exceeds %d bytes", tooBig.Limit), http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	if len(body) == 0 {
		http.Error(w, "POST requires a non-empty body", http.StatusBadRequest)
		return
	}
	messages, isBatch, err := jsonrpc.DecodeBatch(body)
	if err != nil {
		http.Error(w, fmt.Sprintf("malformed payload: %v", err), http.StatusBadRequest)
		return
	}
	header := req.Header.Get(headerProtocolVersion)
	version := header
	if version == "" {
		version = versionWithoutHeader
	}
	if isBatch && version >= ProtocolVersion20250618 {
		http.Error(w, fmt.Sprintf("JSON-RPC batching is not supported in %s and later (request version: %s)", ProtocolVersion20250618, version), http.StatusBadRequest)
		return
	}

	p := post{messages: messages, calls: map[jsonrpc.ID]bool{}, progress: map[jsonrpc.ID]any{}}
	server.mu.Lock()
	methods := server.methods
	server.mu.Unlock()
	for _, msg := range messages {
		r, ok := msg.(*jsonrpc.Request)
		if !ok {
			continue
		}
		if _, err := checkRequest(r, methods); err != nil {
			if version >= ProtocolVersion20260728 && errors.Is(err, jsonrpc.ErrNotHandled) && r.IsCall() {
				writeJSONError(w, http.StatusNotFound, r.ID, &jsonrpc.Error{Code: jsonrpc.CodeMethodNotFound, Message: err.Error()})
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		switch r.Method {
		case methodInitialize:
			p.hasInit = true
			if r.IsCall() && p.initialize == nil {
				p.initialize = r
			}
		case notificationInitialized:
			p.hasInitd = true
		case methodSubscriptionsListen:
			p.listen = true
		}
		if version >= ProtocolVersion20260728 {
			p.modern = true
		}
		metaVersion, _ := paramsMeta(r.Params)[MetaKeyProtocolVersion].(string)
		if (r.IsCall() && version >= ProtocolVersion20260728) || metaVersion != "" {
			if !h.opts.Stateless && r.Method != methodDiscover {
				legacy := slices.DeleteFunc(slices.Clone(server.versions), func(v string) bool { return v >= ProtocolVersion20260728 })
				data, _ := json.Marshal(UnsupportedProtocolVersionData{Supported: legacy, Requested: version})
				writeJSONError(w, http.StatusBadRequest, r.ID, &jsonrpc.Error{
					Code:    CodeUnsupportedProtocolVersion,
					Message: fmt.Sprintf("protocol version %q is not supported by this server", version),
					Data:    data,
				})
				return
			}
			if header == "" {
				writeJSONError(w, http.StatusBadRequest, r.ID, &jsonrpc.Error{Code: CodeHeaderMismatch,
					Message: fmt.Sprintf("%s header is required for requests carrying %q", headerProtocolVersion, MetaKeyProtocolVersion)})
				return
			}
			if metaVersion == "" {
				writeJSONError(w, http.StatusBadRequest, r.ID, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams,
					Message: fmt.Sprintf("missing or invalid _meta field %q", MetaKeyProtocolVersion)})
				return
			}
			if header != metaVersion {
				writeJSONError(w, http.StatusBadRequest, r.ID, &jsonrpc.Error{Code: CodeHeaderMismatch,
					Message: fmt.Sprintf("%s header %q does not match request %s %q", headerProtocolVersion, header, MetaKeyProtocolVersion, metaVersion)})
				return
			}
		}
		r.Extra = &RequestExtra{Header: req.Header}
		if r.IsCall() {
			p.calls[r.ID] = true
			if token := paramsMeta(r.Params)["progressToken"]; token != nil {
				p.progress[r.ID] = token
			}
		}
	}
	if !isBatch && len(messages) == 1 {
		if err := checkStandardHeaders(req.Header, messages[0]); err != nil {
			var id jsonrpc.ID
			if r, ok := messages[0].(*jsonrpc.Request); ok {
				id = r.ID
			}
			writeJSONError(w, http.StatusBadRequest, id, &jsonrpc.Error{Code: CodeHeaderMismatch, Message: err.Error()})
			return
		}
	}

	// Without a session, the POST runs on one of its own: kept when it is a
	// stateful handshake, and otherwise gone with the request. A stateless
	// request that names no handshake is served as an initialized client of
	// the revision it named.
	keep := false
	if sess == nil {
		sess = h.newSession(server, "")
		if h.opts.Stateless {
			if !p.hasInit && !p.modern {
				sess.session.state.initialize = &InitializeParams{ProtocolVersion: version}
			}
			if !p.hasInitd && !p.modern {
				sess.session.state.initialized = true
			}
			if !p.modern {
				sess.session.state.logLevel = "info"
			}
		} else {
			keep = p.initialize != nil
		}
		server.addSession(sess.session)
		if !keep {
			defer sess.session.Close()
		}
	}

	ctx := req.Context()
	if len(p.calls) == 0 {
		for _, msg := range messages {
			if sess.isClosed() {
				http.Error(w, "session is closing", http.StatusNotFound)
				return
			}
			sess.receive(ctx, msg)
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}

	st := newStream(w, p.calls, version >= ProtocolVersion20260728, p.listen)
	st.progress = p.progress
	if !sess.register(st, p.calls) {
		for id := range p.calls {
			writeJSONError(w, http.StatusBadRequest, id, &jsonrpc.Error{Code: jsonrpc.CodeInvalidRequest,
				Message: fmt.Sprintf("duplicate in-flight request ID %v", id.Raw())})
			return
		}
	}
	defer sess.release(st)
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Connection", "keep-alive")
	if keep {
		st.hold()
	} else if id := sess.sessionID(); p.hasInit && id != "" {
		// A handshake on a session that has one is refused, under its id.
		w.Header().Set(headerSessionID, id)
	}

	// A listen ends when its client goes, so its handler runs on the
	// request's context. Any other call runs on, past a dropped connection,
	// unless the client cancels it.
	callCtx := context.WithoutCancel(ctx)
	if p.listen {
		callCtx = ctx
	}
	for _, msg := range messages {
		r, ok := msg.(*jsonrpc.Request)
		if !ok || !r.IsCall() {
			sess.receive(ctx, msg)
			continue
		}
		if r == p.initialize && keep {
			// The handshake is answered first, and its id goes out with the
			// headers, before anything else on the stream.
			resp := sess.session.handleCall(callCtx, r)
			if resp.Error == nil && !h.establish(ctx, w, sess) {
				resp = &jsonrpc.Response{ID: r.ID, Error: fmt.Errorf("%w: could not start a session", jsonrpc.ErrInternal)}
				sess.session.Close()
			}
			if resp.Error != nil {
				sess.session.Close()
			}
			st.unhold()
			_ = sess.send(callCtx, resp)
			continue
		}
		go func() { _ = sess.send(callCtx, sess.session.handleCall(callCtx, r)) }()
	}
	st.unhold()

	// The POST stays open until every call it carried is answered, or the
	// client goes: closing the session does not cut off a call in progress.
	tick := time.NewTicker(h.keepalive)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-st.done:
			return
		case <-tick.C:
			st.keepalive()
		}
	}
}

// establish gives a session that initialized its id, and keeps it.
func (h *StreamableHTTPHandler) establish(ctx context.Context, w http.ResponseWriter, sess *httpSession) bool {
	var id string
	if h.opts.SessionIDs != nil {
		minted, err := h.opts.SessionIDs.Issue(ctx, sessionInfoOf(sess.session.InitializeParams()))
		if err != nil {
			return false
		}
		id = minted
	} else {
		id = rand.Text()
	}
	sess.mu.Lock()
	sess.id = id
	sess.mu.Unlock()
	sess.session.setID(id)
	h.mu.Lock()
	h.sessions[id] = sess
	h.mu.Unlock()
	w.Header().Set(headerSessionID, id)
	return true
}

// checkStandardHeaders checks the Mcp-Method and Mcp-Name headers a
// 2026-07-28 request repeats from its body.
func checkStandardHeaders(header http.Header, msg jsonrpc.Message) error {
	if v := header.Get(headerProtocolVersion); v == "" || v < ProtocolVersion20260728 {
		return nil
	}
	r, ok := msg.(*jsonrpc.Request)
	if !ok {
		return nil
	}
	method := header.Get(headerMethod)
	if method == "" {
		return errors.New("missing required Mcp-Method header")
	}
	if method != r.Method {
		return fmt.Errorf("header mismatch: Mcp-Method header value '%s' does not match body value '%s'", method, r.Method)
	}
	if r.Method != methodCallTool && r.Method != methodReadResource && r.Method != methodGetPrompt {
		return nil
	}
	name := header.Get(headerName)
	if name == "" {
		return fmt.Errorf("missing required Mcp-Name header for method %q", r.Method)
	}
	var body struct {
		Name string `json:"name"`
		URI  string `json:"uri"`
	}
	if json.Unmarshal(r.Params, &body) != nil {
		return fmt.Errorf("failed to extract name from parameters for method %q", r.Method)
	}
	inBody := body.Name
	if r.Method == methodReadResource {
		inBody = body.URI
	}
	if name != inBody {
		return fmt.Errorf("header mismatch: Mcp-Name header value '%s' does not match body value '%s'", name, inBody)
	}
	return nil
}

// writeJSONError answers with a JSON-RPC error as plain JSON.
func writeJSONError(w http.ResponseWriter, status int, id jsonrpc.ID, err *jsonrpc.Error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data, merr := jsonrpc.Encode(&jsonrpc.Response{ID: id, Error: err}); merr == nil {
		_, _ = w.Write(data)
	}
}

// httpSession is a session over Streamable HTTP: the streams its client
// has open.
type httpSession struct {
	handler   *StreamableHTTPHandler
	session   *ServerSession
	id        string
	stateless bool

	mu         sync.Mutex
	standalone *stream
	byRequest  map[jsonrpc.ID]*stream
	streams    []*stream
	closed     bool
	done       chan struct{}
}

// receive runs a notification, or hands over a response to a call the
// server made.
func (s *httpSession) receive(ctx context.Context, msg jsonrpc.Message) {
	switch m := msg.(type) {
	case *jsonrpc.Response:
		s.session.deliverResponse(m)
	case *jsonrpc.Request:
		s.session.handleNotification(ctx, m)
	}
}

func (s *httpSession) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// register attaches a POST's stream to the calls it carries. A call whose id
// is already in flight on the session is refused.
func (s *httpSession) register(st *stream, calls map[jsonrpc.ID]bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range calls {
		if _, busy := s.byRequest[id]; busy {
			return false
		}
	}
	for id := range calls {
		s.byRequest[id] = st
	}
	s.streams = append(s.streams, st)
	return true
}

// release detaches a POST's stream once its request is over: whatever is
// still to come for it has nowhere to go.
func (s *httpSession) release(st *stream) {
	st.release()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, x := range s.byRequest {
		if x == st {
			delete(s.byRequest, id)
		}
	}
	s.streams = slices.DeleteFunc(s.streams, func(x *stream) bool { return x == st })
}

// attachStandalone claims the session's push stream for a GET; nil when
// another GET holds it. The stream holds what it is sent until it is open.
func (s *httpSession) attachStandalone(w http.ResponseWriter) *stream {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.standalone != nil {
		return nil
	}
	st := newStream(w, nil, false, false)
	st.held = true
	s.standalone = st
	return st
}

func (s *httpSession) detachStandalone(st *stream) {
	st.release()
	s.mu.Lock()
	if s.standalone == st {
		s.standalone = nil
	}
	s.mu.Unlock()
}

func (s *httpSession) canCall() bool { return !s.stateless && s.sessionID() != "" }

// sessionID is the session's id, "" until a handshake gives it one.
func (s *httpSession) sessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.id
}

// send delivers a message to the stream it belongs on: a response to the
// POST that asked, anything else to the POST whose call it was made in, and
// otherwise to the listen or GET stream.
func (s *httpSession) send(ctx context.Context, msg jsonrpc.Message) error {
	data, err := jsonrpc.Encode(msg)
	if err != nil {
		return err
	}
	var related, answers jsonrpc.ID
	resp, isResponse := msg.(*jsonrpc.Response)
	if isResponse {
		related, answers = resp.ID, resp.ID
	} else if id, ok := requestIDFrom(ctx, s.session); ok {
		related = id
	}

	s.mu.Lock()
	var st *stream
	if related.IsValid() {
		st = s.byRequest[related]
	} else {
		for _, x := range s.streams {
			if x.listen {
				st = x
				break
			}
		}
		if st == nil {
			st = s.standalone
		}
	}
	if answers.IsValid() {
		delete(s.byRequest, answers)
	}
	closed := s.closed
	s.mu.Unlock()

	if st == nil {
		if !related.IsValid() {
			return fmt.Errorf("%w: undelivered message: stream not connected or already closed", errRejected)
		}
		return fmt.Errorf("%w: write to closed stream", errRejected)
	}
	if closed && !isResponse {
		return errors.New("session is closed")
	}
	status := 0
	if isResponse && st.modern {
		status = errorStatus(resp.Error)
	}
	if err := st.write(data, answers, status); err != nil {
		return fmt.Errorf("%w: undelivered message: %v", errRejected, err)
	}
	return nil
}

// errorStatus is the HTTP status 2026-07-28 gives a protocol error, 0 for
// one that travels as an ordinary response.
func errorStatus(err error) int {
	if err == nil {
		return 0
	}
	switch jsonrpc.ToWire(err).Code {
	case jsonrpc.CodeMethodNotFound:
		return http.StatusNotFound
	case jsonrpc.CodeInvalidParams, CodeUnsupportedProtocolVersion, CodeMissingRequiredClientCapabilities:
		return http.StatusBadRequest
	}
	return 0
}

// close ends the session's streams.
func (s *httpSession) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.done)
}

// stream is one HTTP response the server writes SSE events to: a POST's, for
// the responses to its calls, or a GET's, for pushes.
type stream struct {
	mu       sync.Mutex
	w        http.ResponseWriter // nil once the request is over
	started  bool
	held     bool
	queue    [][]byte
	pending  map[jsonrpc.ID]bool // calls still to answer; nil for a GET
	progress map[jsonrpc.ID]any  // the progressToken a call named
	ticks    float64             // keepalives sent: progress must only grow
	modern   bool
	listen   bool
	done     chan struct{}
	closed   bool
}

func newStream(w http.ResponseWriter, calls map[jsonrpc.ID]bool, modern, listen bool) *stream {
	var pending map[jsonrpc.ID]bool
	if calls != nil {
		pending = make(map[jsonrpc.ID]bool, len(calls))
		for id := range calls {
			pending[id] = true
		}
	}
	return &stream{w: w, pending: pending, modern: modern, listen: listen, done: make(chan struct{})}
}

// open starts a GET's stream: headers out at once, with a comment, so a
// proxy that holds headers until it has a body still forwards them. What
// was sent to it before follows.
func (st *stream) open() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.started = true
	st.w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(st.w, ": ok\n\n")
	_ = http.NewResponseController(st.w).Flush()
	st.held = false
	for _, data := range st.queue {
		_ = st.event(data)
	}
	st.queue = nil
}

// keepalive writes an SSE comment, which clients ignore, so a call that
// takes long is not taken for a dead connection; or, for each unanswered
// call that named a progressToken, a progress notification, so it is not
// taken for a stalled call either. The headers go out with the first.
func (st *stream) keepalive() {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.w == nil || st.closed || st.held {
		return
	}
	st.ticks++
	sent := false
	for id, token := range st.progress {
		if !st.pending[id] {
			continue
		}
		params, _ := json.Marshal(&ProgressNotificationParams{ProgressToken: token, Progress: st.ticks})
		data, _ := jsonrpc.Encode(&jsonrpc.Request{Method: notificationProgress, Params: params})
		_ = st.event(data)
		sent = true
	}
	if sent {
		return
	}
	st.started = true
	_, _ = io.WriteString(st.w, ": ok\n\n")
	_ = http.NewResponseController(st.w).Flush()
}

// hold queues what is written until unhold, so a handshake can set its
// session id before anything goes out.
func (st *stream) hold() {
	st.mu.Lock()
	st.held = true
	st.mu.Unlock()
}

func (st *stream) unhold() {
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.held {
		return
	}
	st.held = false
	for _, data := range st.queue {
		if st.w != nil {
			st.event(data)
		}
	}
	st.queue = nil
	st.finishIfDone()
}

// write sends one message. status non-zero sends it instead as a plain JSON
// error with that status, as 2026-07-28 wants a protocol error sent.
func (st *stream) write(data []byte, answers jsonrpc.ID, status int) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if answers.IsValid() && st.pending != nil {
		delete(st.pending, answers)
	}
	defer st.finishIfDone()
	if st.w == nil || st.closed {
		return errors.New("stream not connected or already closed")
	}
	if status != 0 && !st.started {
		st.started = true
		st.w.Header().Set("Content-Type", "application/json")
		st.w.WriteHeader(status)
		_, err := st.w.Write(data)
		return err
	}
	if st.held {
		st.queue = append(st.queue, data)
		return nil
	}
	return st.event(data)
}

// event writes one SSE message event and flushes it. st.mu is held.
func (st *stream) event(data []byte) error {
	st.started = true
	buf := make([]byte, 0, len(data)+len("event: message\ndata: \n\n"))
	buf = append(buf, "event: message\ndata: "...)
	buf = append(buf, data...)
	buf = append(buf, "\n\n"...)
	_, err := st.w.Write(buf)
	_ = http.NewResponseController(st.w).Flush()
	return err
}

// finishIfDone ends a POST's stream once every call is answered and
// nothing is held back. st.mu is held.
func (st *stream) finishIfDone() {
	if st.pending != nil && len(st.pending) == 0 && !st.held && !st.closed {
		st.closed = true
		close(st.done)
	}
}

// release detaches the stream from its finished request.
func (st *stream) release() {
	st.mu.Lock()
	st.w = nil
	st.mu.Unlock()
}

// isLoopback reports whether a host, or host:port, is this machine.
func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = strings.Trim(addr, "[]")
	}
	if host == "localhost" {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}
