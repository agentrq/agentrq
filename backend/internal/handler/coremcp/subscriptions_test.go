// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mustafaturan/monoflake"
)

const listenUser = "listen-user"

// listenCrud owns the workspaces in owned, for listenUser alone.
type listenCrud struct {
	crud.Controller
	owned map[int64]bool
}

func (c *listenCrud) CheckWorkspaceAccess(_ context.Context, id int64, userID string) (bool, error) {
	return userID == listenUser && c.owned[id], nil
}

// listenServer serves CoreMCP over HTTP as listenUser, who owns workspaces 1
// and 2, as the authenticating wrapper in coremcp.go would: with the token's
// claims, when there are any.
func listenServer(t *testing.T, claims ...*auth.Claims) (*httptest.Server, *TaskListeners) {
	t.Helper()
	listeners := NewTaskListeners()
	t.Cleanup(listeners.Close)
	s := newServer(&listenCrud{owned: map[int64]bool{1: true, 2: true}}, "https://agentrq.example", nil, listeners)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), "user_id", listenUser)
		for _, c := range claims {
			ctx = context.WithValue(ctx, auth.CtxKeyMCPClaims, c)
		}
		s.Handler().ServeHTTP(w, r.WithContext(ctx))
	}))
	t.Cleanup(srv.Close)
	return srv, listeners
}

// listenStream is an open subscriptions/listen: every JSON-RPC message the
// server sends on it, in order.
type listenStream struct {
	messages chan map[string]any
	cancel   context.CancelFunc
}

// coreRPC is a 2026-07-28 request: no handshake, the revision in _meta.
func coreRPC(id int, method string, params map[string]any) string {
	params["_meta"] = map[string]any{
		"io.modelcontextprotocol/protocolVersion":    "2026-07-28",
		"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "test", "version": "0"},
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	return string(b)
}

func openListen(t *testing.T, srv *httptest.Server, id int, uris ...string) *listenStream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	body := coreRPC(id, "subscriptions/listen", map[string]any{"notifications": map[string]any{"resourceSubscriptions": uris}})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", "subscriptions/listen")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("subscriptions/listen: expected 200, got %d", resp.StatusCode)
	}
	ls := &listenStream{messages: make(chan map[string]any, 16), cancel: cancel}
	go func() {
		defer resp.Body.Close()
		defer close(ls.messages)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			data, ok := bytes.CutPrefix(sc.Bytes(), []byte("data: "))
			if !ok {
				continue
			}
			var msg map[string]any
			if json.Unmarshal(data, &msg) == nil {
				ls.messages <- msg
			}
		}
	}()
	return ls
}

// next is the stream's next message; nil once the stream has ended.
func (ls *listenStream) next(t *testing.T) map[string]any {
	t.Helper()
	select {
	case msg := <-ls.messages:
		return msg
	case <-time.After(5 * time.Second):
		t.Fatal("no message on the listen stream within 5s")
		return nil
	}
}

func subscriptionID(msg map[string]any) any {
	for _, key := range []string{"params", "result"} {
		if body, ok := msg[key].(map[string]any); ok {
			meta, _ := body["_meta"].(map[string]any)
			return meta["io.modelcontextprotocol/subscriptionId"]
		}
	}
	return nil
}

// expectUpdated reads the next message, which must be a resources/updated
// notice of uri on the listen with id.
func (ls *listenStream) expectUpdated(t *testing.T, id int, uri string) {
	t.Helper()
	msg := ls.next(t)
	params, _ := msg["params"].(map[string]any)
	if msg["method"] != "notifications/resources/updated" || params["uri"] != uri {
		t.Fatalf("expected a resources/updated notice of %s, got %v", uri, msg)
	}
	if subscriptionID(msg) != float64(id) {
		t.Errorf("notice of %s carries subscriptionId %v, want %d", uri, subscriptionID(msg), id)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting until %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func heldCount(l *TaskListeners, uri string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.held[uri]
}

// The acknowledgement comes first, carries the listen's id, and names only
// what is kept: the task resources of the caller's own workspaces, each once.
func TestListen_AcknowledgesFirstWithTheOwnedURIs(t *testing.T) {
	srv, _ := listenServer(t)
	owned := taskURI(1, 10)
	ls := openListen(t, srv, 7,
		owned, owned, workspaceTasksURI(2), delegatedTasksURI(1),
		taskURI(3, 10),                   // someone else's workspace
		"agentrq://workspaces/x/tasks/y", // not an id
		"file:///etc/passwd",
	)

	ack := ls.next(t)
	if ack["method"] != "notifications/subscriptions/acknowledged" {
		t.Fatalf("the first message is %v, want the acknowledgement", ack)
	}
	if subscriptionID(ack) != float64(7) {
		t.Errorf("acknowledgement carries subscriptionId %v, want 7", subscriptionID(ack))
	}
	params := ack["params"].(map[string]any)
	got := params["notifications"].(map[string]any)["resourceSubscriptions"]
	want := []any{owned, workspaceTasksURI(2), delegatedTasksURI(1)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("acknowledged %v, want %v", got, want)
	}
}

// A change reaches each URI that names it — the task, its workspace and the
// workspace that delegated it — and nothing reaches a URI nobody follows.
func TestListen_SendsAChangeToEveryURIThatNamesIt(t *testing.T) {
	srv, listeners := listenServer(t)
	ls := openListen(t, srv, 3, taskURI(1, 10), workspaceTasksURI(1), delegatedTasksURI(2))
	ls.next(t) // the acknowledgement

	// Changes nobody follows: another task in another workspace, delegated by
	// nobody, and one delegated by a workspace not followed.
	listeners.TaskChanged(2, 20, 0)
	listeners.TaskChanged(2, 21, 1)
	// The status of the followed task changes, then a task delegated by
	// workspace 2 gets a message.
	listeners.TaskChanged(1, 10, 0)
	listeners.TaskChanged(1, 11, 2)

	ls.expectUpdated(t, 3, taskURI(1, 10))
	ls.expectUpdated(t, 3, workspaceTasksURI(1))
	ls.expectUpdated(t, 3, workspaceTasksURI(1))
	ls.expectUpdated(t, 3, delegatedTasksURI(2))
	select {
	case msg := <-ls.messages:
		t.Errorf("unexpected message %v", msg)
	case <-time.After(100 * time.Millisecond):
	}
}

// Two listens on the same resource each hear of it under their own id, and
// one closing leaves the other listening.
func TestListen_ConcurrentListensAreIndependent(t *testing.T) {
	srv, listeners := listenServer(t)
	uri := taskURI(1, 10)
	a := openListen(t, srv, 1, uri)
	b := openListen(t, srv, 2, uri)
	a.next(t)
	b.next(t)
	waitFor(t, "both listens hold the URI", func() bool { return heldCount(listeners, uri) == 2 })

	listeners.TaskChanged(1, 10, 0)
	a.expectUpdated(t, 1, uri)
	b.expectUpdated(t, 2, uri)

	a.cancel()
	waitFor(t, "the closed listen lets go", func() bool { return heldCount(listeners, uri) == 1 })
	listeners.TaskChanged(1, 10, 0)
	b.expectUpdated(t, 2, uri)
}

// A client that drops its stream leaves nothing behind.
func TestListen_ADisconnectCleansUp(t *testing.T) {
	srv, listeners := listenServer(t)
	ls := openListen(t, srv, 1, taskURI(1, 10), workspaceTasksURI(1))
	ls.next(t)
	waitFor(t, "the listen is open", listeners.Listening)

	ls.cancel()
	waitFor(t, "nobody is listening", func() bool { return !listeners.Listening() })
	listeners.mu.Lock()
	defer listeners.mu.Unlock()
	if len(listeners.held) != 0 {
		t.Errorf("held URIs left after a disconnect: %v", listeners.held)
	}
}

// On shutdown each listen gets its complete result, then its stream ends.
func TestListen_CloseCompletesEveryListen(t *testing.T) {
	srv, listeners := listenServer(t)
	ls := openListen(t, srv, 9, taskURI(1, 10))
	ls.next(t)
	waitFor(t, "the listen is open", listeners.Listening)

	listeners.Close()
	res := ls.next(t)
	result, _ := res["result"].(map[string]any)
	if res["id"] != float64(9) || result["resultType"] != "complete" {
		t.Fatalf("expected the complete result of listen 9, got %v", res)
	}
	if subscriptionID(res) != float64(9) {
		t.Errorf("complete result carries subscriptionId %v, want 9", subscriptionID(res))
	}
	select {
	case msg, open := <-ls.messages:
		if open {
			t.Errorf("unexpected message after the complete result: %v", msg)
		}
	case <-time.After(5 * time.Second):
		t.Error("the stream stayed open after the complete result")
	}
}

// A listen ends with its token, so a client keeps listening only by
// presenting a valid one again.
func TestListen_EndsWhenItsTokenExpires(t *testing.T) {
	for _, tc := range []struct {
		name    string
		claims  *auth.Claims
		expires bool
	}{
		{"expiring token", &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(300 * time.Millisecond))}}, true},
		{"token without expiry", &auth.Claims{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := listenServer(t, tc.claims)
			ls := openListen(t, srv, 4, taskURI(1, 10))
			ls.next(t)
			select {
			case msg := <-ls.messages:
				result, _ := msg["result"].(map[string]any)
				if !tc.expires || msg["id"] != float64(4) || result["resultType"] != "complete" {
					t.Errorf("unexpected message %v", msg)
				}
			case <-time.After(time.Second):
				if tc.expires {
					t.Error("the listen outlived its token")
				}
			}
		})
	}
}

func TestListen_RefusesTooManyURIs(t *testing.T) {
	srv, _ := listenServer(t)
	uris := make([]string, maxListenURIs+1)
	for i := range uris {
		uris[i] = taskURI(1, int64(i+1))
	}
	_, out, _ := corePost(t, srv.URL, map[string]string{"MCP-Protocol-Version": "2026-07-28", "Mcp-Method": "subscriptions/listen"},
		coreRPC(1, "subscriptions/listen", map[string]any{"notifications": map[string]any{"resourceSubscriptions": uris}}))
	if !bytes.Contains(out, []byte(`"code":-32602`)) || !bytes.Contains(out, []byte("at most 100")) {
		t.Errorf("expected an invalid-params error, got %s", out)
	}
}

// The 2026-07-28 revision has no resources/subscribe, and the SDK says so. An
// older revision's would subscribe a session that ends with its own request,
// so it is turned down too.
func TestListen_RefusesTheLegacySubscribe(t *testing.T) {
	srv, _ := listenServer(t)
	for _, method := range []string{"resources/subscribe", "resources/unsubscribe"} {
		status, out, _ := corePost(t, srv.URL, map[string]string{"MCP-Protocol-Version": "2025-06-18"},
			fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":%q,"params":{"uri":%q}}`, method, taskURI(1, 10)))
		if status != http.StatusOK || !bytes.Contains(out, []byte(`"code":-32601`)) {
			t.Errorf("%s: expected a method-not-found error, got %d %s", method, status, out)
		}
	}
}

// Other methods pass through untouched.
func TestListen_LeavesOtherMethodsAlone(t *testing.T) {
	srv, _ := listenServer(t)
	status, out, _ := corePost(t, srv.URL, map[string]string{"MCP-Protocol-Version": "2026-07-28", "Mcp-Method": "resources/templates/list"},
		coreRPC(1, "resources/templates/list", map[string]any{}))
	if status != http.StatusOK {
		t.Fatalf("resources/templates/list: %d %s", status, out)
	}
	for _, name := range []string{`"name":"task"`, `"name":"workspace-tasks"`, `"name":"delegated-tasks"`} {
		if !bytes.Contains(out, []byte(name)) {
			t.Errorf("resource templates are missing %s: %s", name, out)
		}
	}
}

// Clients see they may listen to resources.
func TestListen_SubscribeIsAdvertised(t *testing.T) {
	srv := newStatelessTestServer(t)
	status, out, _ := corePost(t, srv.URL, nil,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`)
	if status != http.StatusOK || !bytes.Contains(out, []byte(`"subscribe":true`)) {
		t.Errorf("initialize does not advertise resource subscriptions: %d %s", status, out)
	}
}

// A full queue drops a change rather than wait for the notifier.
func TestTaskChanged_NeverWaits(t *testing.T) {
	l := NewTaskListeners() // never served, so nothing drains the queue
	for i := range taskChangeQueue + 1 {
		l.TaskChanged(1, int64(i), 0)
	}
	if len(l.changes) != taskChangeQueue {
		t.Errorf("queue holds %d changes, want %d", len(l.changes), taskChangeQueue)
	}
}

func TestParseTaskURI(t *testing.T) {
	ws, task := monoflake.ID(1).String(), monoflake.ID(10).String()
	cases := map[string]taskURIRef{
		taskURI(1, 10):                                                  {kind: taskURITask, workspaceID: 1, taskID: 10},
		workspaceTasksURI(1):                                            {kind: taskURIWorkspaceTasks, workspaceID: 1},
		delegatedTasksURI(1):                                            {kind: taskURIDelegatedTasks, workspaceID: 1},
		delegatedURIPrefix + "x":                                        {},
		delegatedURIPrefix:                                              {},
		"agentrq://guides/new-workspace":                                {},
		workspacesURIPrefix + ws:                                        {},
		workspacesURIPrefix + ws + "/memory":                            {},
		workspacesURIPrefix + "x/tasks":                                 {},
		workspacesURIPrefix + ws + "/tasks/x":                           {},
		workspacesURIPrefix + ws + "/tasks/" + task + "/messages":       {},
		workspacesURIPrefix + "1/tasks/" + task:                         {}, // not how monoflake writes 1
		workspacesURIPrefix + strings.Repeat("z", 12) + "/tasks":        {}, // overflows
		workspacesURIPrefix + monoflake.ID(0).String() + "/tasks":       {},
		workspacesURIPrefix + ws + "/tasks/" + monoflake.ID(0).String(): {},
	}
	for uri, want := range cases {
		if got := parseTaskURI(uri); got != want {
			t.Errorf("parseTaskURI(%q) = %+v, want %+v", uri, got, want)
		}
	}
}
