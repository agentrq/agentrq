// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
)

// The gateway lets its concurrency limit be changed while it is running. These
// tests pin the three things that are easy to get wrong about that: that both
// spellings of every field the gateway sends are read, that a gateway too old
// to act on a change is never offered a control, and that nothing about a
// change is believed until the gateway's own next report says so.

func intPtr(v int) *int    { return &v }
func boolPtr(v bool) *bool { return &v }

func connectedConcurrencyServer(t *testing.T) (*WorkspaceServer, string, chan []byte) {
	t.Helper()
	replies := 0
	ps := permissionServer(t, &replies)
	ps.bus = eventbus.New()
	ps.agentConcurrency = make(map[string]AgentConcurrencySnapshot)
	sessionID := connectedServer(t, ps, "acp-gateway")
	streamFor(ps, sessionID)

	ch := ps.bus.Subscribe(ps.workspaceID, "")
	t.Cleanup(func() { ps.bus.Unsubscribe(ps.workspaceID, "", ch) })
	return ps, sessionID, ch
}

// The exact frame acp-gateway puts on the wire (its `sendConcurrencyNotification`),
// so a rename on either side fails here rather than in production.
//
// camelCase throughout, unlike the snake_case siblings on this channel — and
// unlike them it is not doubled, because this pair was born with one spelling.
// taskId and sessionId are always empty: the limit belongs to the gateway
// process, not to any task or session it holds.
const gatewayConcurrencyFrame = `{
  "jsonrpc": "2.0",
  "method": "notifications/claude/channel/concurrency",
  "params": {
    "taskId": "", "sessionId": "",
    "maxConcurrency": 4,
    "active": 2,
    "queued": 3,
    "min": 1,
    "max": 64,
    "canSet": true
  }
}`

func TestAgentConcurrencyParamsMatchesTheGatewayWireFormat(t *testing.T) {
	var frame struct {
		Method string                 `json:"method"`
		Params AgentConcurrencyParams `json:"params"`
	}
	if err := json.Unmarshal([]byte(gatewayConcurrencyFrame), &frame); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if frame.Method != AgentConcurrencyNotificationMethod {
		t.Errorf("method = %q, want %q", frame.Method, AgentConcurrencyNotificationMethod)
	}

	p := frame.Params
	if got := p.limit(); got != 4 {
		t.Errorf("limit() = %d, want 4", got)
	}
	if !p.canSet() {
		t.Error("canSet() = false, want true")
	}
	if p.Active != 2 || p.Queued != 3 {
		t.Errorf("active/queued = %d/%d, want 2/3", p.Active, p.Queued)
	}
	if p.Min == nil || *p.Min != 1 || p.Max == nil || *p.Max != 64 {
		t.Errorf("min/max = %v/%v, want 1/64", p.Min, p.Max)
	}
}

// Absent is not zero. A gateway that named no limit has said nothing, which is
// a different thing from one that will run nothing — and `canSet` absent is the
// rule that keeps a control off every gateway older than this feature.
func TestAgentConcurrencyParamsTreatsAbsentAsUnsaid(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		wantLimit int
		wantSet   bool
	}{
		{"what the gateway sends", `{"maxConcurrency": 8, "canSet": true}`, 8, true},
		{"a limit reported without permission to change it", `{"maxConcurrency": 8}`, 8, false},
		{"permission with no limit behind it", `{"canSet": true}`, 0, true},
		{"neither", `{"active": 1}`, 0, false},
		{
			// The one spelling the gateway does not write. Read as absent
			// rather than quietly accepted: this notification was designed
			// with a single spelling, and tolerating a second here is how the
			// channel's older messages ended up carrying every field twice.
			"the spelling this channel's older messages use",
			`{"max_concurrency": 8, "can_set": true}`, 0, false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var p AgentConcurrencyParams
			if err := json.Unmarshal([]byte(tc.body), &p); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got := p.limit(); got != tc.wantLimit {
				t.Errorf("limit() = %d, want %d", got, tc.wantLimit)
			}
			if got := p.canSet(); got != tc.wantSet {
				t.Errorf("canSet() = %v, want %v", got, tc.wantSet)
			}
		})
	}
}

func TestHandleCustomNotificationRoutesConcurrency(t *testing.T) {
	ps := &WorkspaceServer{
		workspaceID:      100,
		bus:              eventbus.New(),
		agentConcurrency: make(map[string]AgentConcurrencySnapshot),
	}
	ps.HandleCustomNotification(context.Background(), "transport-session", []byte(gatewayConcurrencyFrame))

	// Keyed by the MCP transport session — the namespace liveSessionIDs
	// reports. The payload's session_id is empty on this notification anyway,
	// which is precisely why it must not be used as a key.
	got, ok := ps.agentConcurrency["transport-session"]
	if !ok {
		t.Fatalf("concurrency was not recorded; map = %+v", ps.agentConcurrency)
	}
	if _, wrong := ps.agentConcurrency[""]; wrong {
		t.Error("keyed by the payload's empty session_id rather than the transport session")
	}
	if got.MaxConcurrency != 4 || got.Active != 2 || got.Queued != 3 || !got.CanSet {
		t.Errorf("recorded = %+v", got)
	}
}

func TestConcurrencySnapshotFrom(t *testing.T) {
	t.Run("keeps the range the gateway reported", func(t *testing.T) {
		got := concurrencySnapshotFrom(AgentConcurrencyParams{
			MaxConcurrency: intPtr(4), Min: intPtr(2), Max: intPtr(64),
		})
		if got.Min != 2 || got.Max != 64 {
			t.Errorf("range = %d..%d, want 2..64", got.Min, got.Max)
		}
	})

	t.Run("floors the minimum at one", func(t *testing.T) {
		// A limit of 0 is a queue that never hands anything out — a stall
		// dressed as a setting, not something to offer a way to ask for.
		for _, min := range []int{0, -5} {
			got := concurrencySnapshotFrom(AgentConcurrencyParams{
				MaxConcurrency: intPtr(4), Min: intPtr(min),
			})
			if got.Min != 1 {
				t.Errorf("min for a reported %d = %d, want 1", min, got.Min)
			}
		}
	})

	t.Run("leaves an unstated ceiling at zero", func(t *testing.T) {
		// Nothing is invented here. A client reads 0 as "no ceiling of its
		// own", which is honest; a number made up here would be wrong the
		// first time the gateway moved its real one.
		got := concurrencySnapshotFrom(AgentConcurrencyParams{MaxConcurrency: intPtr(4)})
		if got.Max != 0 {
			t.Errorf("max = %d, want 0 for a gateway that named none", got.Max)
		}
		if got.Min != 1 {
			t.Errorf("min = %d, want the floor of 1", got.Min)
		}
	})

	t.Run("widens the range to contain the limit in force", func(t *testing.T) {
		// The range and the limit are independent numbers on the wire: a
		// gateway started with --max-concurrency 100 can report max: 64. A
		// control clamped to the reported range could not then display the
		// value it was showing, and merely opening it would propose a change
		// nobody asked for. What is running is the one number certainly true,
		// so the range moves rather than the limit.
		above := concurrencySnapshotFrom(AgentConcurrencyParams{
			MaxConcurrency: intPtr(100), Min: intPtr(1), Max: intPtr(64),
		})
		if above.MaxConcurrency != 100 || above.Max != 100 {
			t.Errorf("got %+v, want the limit kept at 100 and max widened to it", above)
		}

		below := concurrencySnapshotFrom(AgentConcurrencyParams{
			MaxConcurrency: intPtr(2), Min: intPtr(8), Max: intPtr(64),
		})
		if below.MaxConcurrency != 2 || below.Min != 2 {
			t.Errorf("got %+v, want the limit kept at 2 and min widened down to it", below)
		}
	})

	t.Run("raises an inverted ceiling to the floor", func(t *testing.T) {
		got := concurrencySnapshotFrom(AgentConcurrencyParams{
			MaxConcurrency: intPtr(4), Min: intPtr(4), Max: intPtr(2),
		})
		if got.Min > got.Max {
			t.Errorf("range = %d..%d, which no control can represent", got.Min, got.Max)
		}
	})

	t.Run("treats a negative limit as none reported", func(t *testing.T) {
		got := concurrencySnapshotFrom(AgentConcurrencyParams{MaxConcurrency: intPtr(-3)})
		if got.MaxConcurrency != 0 || got.Reported() {
			t.Errorf("got %+v, want a nonsense limit read as nothing reported", got)
		}
	})
}

func TestAgentConcurrencySettable(t *testing.T) {
	cases := []struct {
		name     string
		snapshot AgentConcurrencySnapshot
		want     bool
	}{
		{"willing and reporting", AgentConcurrencySnapshot{MaxConcurrency: 4, CanSet: true}, true},
		{
			// Every gateway ever published reports a limit and the older ones
			// ignore being told to change it. Silence is no.
			"reporting but silent about setting",
			AgentConcurrencySnapshot{MaxConcurrency: 4}, false,
		},
		{
			// Willingness without a current value leaves a control with
			// nothing to show and no way to tell whether a change landed.
			"willing but reporting no limit",
			AgentConcurrencySnapshot{CanSet: true}, false,
		},
		{"nothing at all", AgentConcurrencySnapshot{}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.snapshot.Settable(); got != tc.want {
				t.Errorf("Settable() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAgentConcurrencyIgnoresDepartedSessions(t *testing.T) {
	// The trap #504 fixed for the Stop button: a session outlives the stream
	// that carried it, so a snapshot keyed to a departed gateway still looks
	// live to anything reading the session list alone.
	ps, sessionID, _ := connectedConcurrencyServer(t)
	ps.HandleAgentConcurrency(context.Background(), sessionID, AgentConcurrencyParams{
		MaxConcurrency: intPtr(4), CanSet: boolPtr(true),
	})

	if ps.AgentConcurrency() == nil {
		t.Fatal("AgentConcurrency() = nil while the session is still streaming")
	}

	ps.removeStreamingSession(sessionID)
	if got := ps.AgentConcurrency(); got != nil {
		t.Errorf("AgentConcurrency() = %+v after the stream dropped, want nil", got)
	}
}

// Two gateways on one workspace is a case this file contemplates throughout,
// and it has to be tested against the map directly rather than by attaching a
// second client: the in-memory transport the other tests use hands every
// session an empty ID, so two "gateways" built that way collide on one key and
// the second silently overwrites the first. A test written that way passes
// without ever exercising the choice.
func TestPickAgentConcurrencySession(t *testing.T) {
	settable := AgentConcurrencySnapshot{MaxConcurrency: 8, CanSet: true}
	readOnly := AgentConcurrencySnapshot{MaxConcurrency: 2}
	silent := AgentConcurrencySnapshot{}

	cases := []struct {
		name      string
		snapshots map[string]AgentConcurrencySnapshot
		live      []string
		wantID    string
		wantLimit int
	}{
		{
			// Without the preference the answer is decided by session order,
			// so an older gateway listed first would mask a newer one attached
			// beside it and the workspace would show a read-only number while
			// something perfectly capable was connected.
			name:      "prefers the gateway that can be set, whatever the order",
			snapshots: map[string]AgentConcurrencySnapshot{"old": readOnly, "new": settable},
			live:      []string{"old", "new"},
			wantID:    "new", wantLimit: 8,
		},
		{
			// What every deployment running an older gateway looks like: the
			// number is worth showing even though nothing can be done about it.
			name:      "falls back to a gateway that can only report",
			snapshots: map[string]AgentConcurrencySnapshot{"old": readOnly},
			live:      []string{"old"},
			wantID:    "old", wantLimit: 2,
		},
		{
			name:      "keeps the first read-only gateway when several only report",
			snapshots: map[string]AgentConcurrencySnapshot{"a": readOnly, "b": {MaxConcurrency: 5}},
			live:      []string{"a", "b"},
			wantID:    "a", wantLimit: 2,
		},
		{
			// A session that named no limit has told the interface nothing it
			// can render, so it is passed over rather than returned.
			name:      "skips a session that reported no limit",
			snapshots: map[string]AgentConcurrencySnapshot{"quiet": silent, "loud": readOnly},
			live:      []string{"quiet", "loud"},
			wantID:    "loud", wantLimit: 2,
		},
		{
			// The whole point of filtering on the live list: a snapshot
			// outlives the stream that carried it, so a departed gateway would
			// otherwise go on advertising a limit nothing can act on.
			name:      "ignores a session that is no longer live",
			snapshots: map[string]AgentConcurrencySnapshot{"gone": settable},
			live:      []string{"here"},
			wantID:    "", wantLimit: 0,
		},
		{
			name:      "answers nothing when nothing is live",
			snapshots: map[string]AgentConcurrencySnapshot{"gone": settable},
			live:      nil,
			wantID:    "", wantLimit: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotID, got := pickAgentConcurrencySession(tc.snapshots, tc.live)
			if gotID != tc.wantID {
				t.Errorf("session = %q, want %q", gotID, tc.wantID)
			}
			if tc.wantLimit == 0 {
				if got != nil {
					t.Errorf("snapshot = %+v, want nil", got)
				}
				return
			}
			if got == nil || got.MaxConcurrency != tc.wantLimit {
				t.Errorf("snapshot = %+v, want a limit of %d", got, tc.wantLimit)
			}
		})
	}
}

func TestAgentConcurrencyFallsBackToAReadOnlyGateway(t *testing.T) {
	// What every deployment running an older gateway looks like: the number is
	// worth showing even though nothing can be done about it.
	ps, sessionID, _ := connectedConcurrencyServer(t)
	ps.HandleAgentConcurrency(context.Background(), sessionID, AgentConcurrencyParams{
		MaxConcurrency: intPtr(2), Active: 1,
	})

	got := ps.AgentConcurrency()
	if got == nil || got.MaxConcurrency != 2 {
		t.Fatalf("AgentConcurrency() = %+v, want the read-only gateway's 2", got)
	}
	if got.Settable() {
		t.Error("a gateway that never said it can be set was reported as settable")
	}
}

func TestHandleAgentConcurrencyPublishesTheChange(t *testing.T) {
	// waitForEvent reads one event, or fails: a silent timeout here would look
	// like a passing test that asserts nothing.
	waitForEvent := func(t *testing.T, ch chan []byte) map[string]any {
		t.Helper()
		select {
		case raw := <-ch:
			// The bus frames events for SSE, so what arrives is
			// "data: {...}\n\n" rather than bare JSON.
			var evt struct {
				Type    string         `json:"type"`
				Payload map[string]any `json:"payload"`
			}
			body := strings.TrimSpace(strings.TrimPrefix(string(raw), "data: "))
			if err := json.Unmarshal([]byte(body), &evt); err != nil {
				t.Fatalf("unmarshal event %q: %v", raw, err)
			}
			if evt.Type != "agent.concurrency" {
				t.Fatalf("event type = %q, want agent.concurrency", evt.Type)
			}
			return evt.Payload
		case <-time.After(time.Second):
			t.Fatal("no agent.concurrency event was published")
			return nil
		}
	}

	t.Run("announces the numbers in camelCase with a base62 workspace id", func(t *testing.T) {
		// Base62 because that is the only form the frontend ever holds; a raw
		// int64 would match nothing and the control would never move.
		ps, sessionID, ch := connectedConcurrencyServer(t)

		ps.HandleAgentConcurrency(context.Background(), sessionID, AgentConcurrencyParams{
			MaxConcurrency: intPtr(4), Active: 2, Queued: 3,
			Min: intPtr(1), Max: intPtr(64), CanSet: boolPtr(true),
		})

		payload := waitForEvent(t, ch)
		want := monoflake.ID(ps.workspaceID).String()
		if got, ok := payload["workspaceId"].(string); !ok || got != want {
			t.Errorf("workspaceId = %v, want the base62 form %q", payload["workspaceId"], want)
		}
		for field, wantValue := range map[string]float64{
			"maxConcurrency": 4, "active": 2, "queued": 3, "min": 1, "max": 64,
		} {
			if got, ok := payload[field].(float64); !ok || got != wantValue {
				t.Errorf("%s = %v, want %v", field, payload[field], wantValue)
			}
		}
		if canSet, ok := payload["canSet"].(bool); !ok || !canSet {
			t.Errorf("canSet = %v, want true", payload["canSet"])
		}
	})

	t.Run("says canSet false for a gateway that only reports", func(t *testing.T) {
		// Settable(), not the raw field, so the event agrees with the REST
		// payload. Disagreeing would light up a control on an event that then
		// vanished on the next page load.
		ps, sessionID, ch := connectedConcurrencyServer(t)

		ps.HandleAgentConcurrency(context.Background(), sessionID, AgentConcurrencyParams{
			MaxConcurrency: intPtr(4),
		})

		if canSet, ok := waitForEvent(t, ch)["canSet"].(bool); !ok || canSet {
			t.Error("canSet was true for a gateway that never said it can be set")
		}
	})

	t.Run("announces a report even when nothing moved", func(t *testing.T) {
		// The case that makes this different from the models notification next
		// door, where a repeat is suppressed. The gateway always answers a set,
		// accepted or not — so a value it refused, or clamped to the limit
		// already in force, comes back as numbers identical to the last report.
		// That is the one message a waiting control exists to receive, and
		// swallowing it would leave the control timing out over a request that
		// was answered at once.
		ps, sessionID, ch := connectedConcurrencyServer(t)
		report := AgentConcurrencyParams{
			MaxConcurrency: intPtr(4), Active: 2, Queued: 3, CanSet: boolPtr(true),
		}

		ps.HandleAgentConcurrency(context.Background(), sessionID, report)
		waitForEvent(t, ch)

		ps.HandleAgentConcurrency(context.Background(), sessionID, report)
		if got, ok := waitForEvent(t, ch)["maxConcurrency"].(float64); !ok || got != 4 {
			t.Errorf("maxConcurrency = %v, want the unchanged 4 announced again", got)
		}
	})

	t.Run("announces a queue that moved under an unchanged limit", func(t *testing.T) {
		// active and queued are the only way to see that a limit did anything,
		// so a report that changes nothing but those is still news.
		ps, sessionID, ch := connectedConcurrencyServer(t)

		ps.HandleAgentConcurrency(context.Background(), sessionID, AgentConcurrencyParams{
			MaxConcurrency: intPtr(4), Active: 2, Queued: 3, CanSet: boolPtr(true),
		})
		waitForEvent(t, ch)

		ps.HandleAgentConcurrency(context.Background(), sessionID, AgentConcurrencyParams{
			MaxConcurrency: intPtr(4), Active: 4, Queued: 1, CanSet: boolPtr(true),
		})
		payload := waitForEvent(t, ch)
		if got, ok := payload["active"].(float64); !ok || got != 4 {
			t.Errorf("active = %v, want 4", payload["active"])
		}
	})

	t.Run("falls back to what the workspace still serves when one withdraws", func(t *testing.T) {
		// A withdrawal is the one case that has to look wider than the report
		// in hand: another gateway may still be reporting, and taking the
		// control away because this one stopped would remove something live.
		//
		// Driven through publishAgentConcurrency directly because a second
		// gateway cannot be faked over the in-memory transport — it hands every
		// session an empty ID, so both would land on one map key.
		ps, sessionID, ch := connectedConcurrencyServer(t)
		ps.agentConcurrency[sessionID] = AgentConcurrencySnapshot{MaxConcurrency: 2, Min: 1}

		ps.publishAgentConcurrency(AgentConcurrencySnapshot{})

		payload := waitForEvent(t, ch)
		if got, ok := payload["maxConcurrency"].(float64); !ok || got != 2 {
			t.Errorf("maxConcurrency = %v, want the surviving gateway's 2", payload["maxConcurrency"])
		}
	})

	t.Run("announces nothing left when the last gateway withdraws", func(t *testing.T) {
		// Zero is how a client is told the control has no subject any more,
		// which is what its store reads as "there is no number here".
		ps, sessionID, ch := connectedConcurrencyServer(t)
		ps.HandleAgentConcurrency(context.Background(), sessionID, AgentConcurrencyParams{
			MaxConcurrency: intPtr(4), CanSet: boolPtr(true),
		})
		waitForEvent(t, ch)

		ps.HandleAgentConcurrency(context.Background(), sessionID, AgentConcurrencyParams{})

		payload := waitForEvent(t, ch)
		if got, ok := payload["maxConcurrency"].(float64); !ok || got != 0 {
			t.Errorf("maxConcurrency = %v, want 0", payload["maxConcurrency"])
		}
		if canSet, ok := payload["canSet"].(bool); !ok || canSet {
			t.Errorf("canSet = %v, want false once nothing reports a limit", payload["canSet"])
		}
	})

	t.Run("announces an active count above the limit rather than hiding it", func(t *testing.T) {
		// Lowering never interrupts a running task: the queue just stops
		// handing out new ones until the active count falls below the new
		// limit. This state is the feature working, not an error.
		ps, sessionID, ch := connectedConcurrencyServer(t)

		ps.HandleAgentConcurrency(context.Background(), sessionID, AgentConcurrencyParams{
			MaxConcurrency: intPtr(1), Active: 4, Queued: 2, CanSet: boolPtr(true),
		})

		payload := waitForEvent(t, ch)
		active, _ := payload["active"].(float64)
		limit, _ := payload["maxConcurrency"].(float64)
		if active != 4 || limit != 1 {
			t.Errorf("active/max = %v/%v, want 4/1 reported as they are", active, limit)
		}
	})
}

func TestSendSetConcurrencyNotification(t *testing.T) {
	settable := AgentConcurrencyParams{
		MaxConcurrency: intPtr(4), Min: intPtr(1), Max: intPtr(64), CanSet: boolPtr(true),
	}

	t.Run("refuses when nothing has reported a limit", func(t *testing.T) {
		ps, _, _ := connectedConcurrencyServer(t)

		if err := ps.SendSetConcurrencyNotification(context.Background(), 4); !errors.Is(err, ErrConcurrencySetUnsupported) {
			t.Errorf("err = %v, want ErrConcurrencySetUnsupported", err)
		}
	})

	t.Run("refuses a gateway that never said it can be set", func(t *testing.T) {
		// Reading silence as consent is what would offer a control that does
		// nothing on most deployments in the field today.
		ps, sessionID, _ := connectedConcurrencyServer(t)
		params := settable
		params.CanSet = nil
		ps.HandleAgentConcurrency(context.Background(), sessionID, params)

		if err := ps.SendSetConcurrencyNotification(context.Background(), 4); !errors.Is(err, ErrConcurrencySetUnsupported) {
			t.Errorf("err = %v, want ErrConcurrencySetUnsupported", err)
		}
	})

	t.Run("refuses a limit below one", func(t *testing.T) {
		// Not a range question: a limit of zero or less is a queue that never
		// hands anything out, which is a stall rather than a setting.
		ps, sessionID, _ := connectedConcurrencyServer(t)
		ps.HandleAgentConcurrency(context.Background(), sessionID, settable)

		for _, limit := range []int{0, -1} {
			if err := ps.SendSetConcurrencyNotification(context.Background(), limit); !errors.Is(err, ErrConcurrencyInvalid) {
				t.Errorf("err for %d = %v, want ErrConcurrencyInvalid", limit, err)
			}
		}
	})

	t.Run("forwards a value above the reported ceiling", func(t *testing.T) {
		// The gateway clamps it and answers with what it settled on. Refusing
		// against a range this server only holds a cached copy of would reject
		// a good value whenever the gateway had widened its real ceiling since
		// the last report.
		ps, sessionID, _ := connectedConcurrencyServer(t)
		ps.HandleAgentConcurrency(context.Background(), sessionID, settable)

		if err := ps.SendSetConcurrencyNotification(context.Background(), 9000); err != nil {
			t.Errorf("err = %v, want an out-of-range value forwarded for the gateway to clamp", err)
		}
	})

	t.Run("sends to the session that offered the control", func(t *testing.T) {
		ps, sessionID, _ := connectedConcurrencyServer(t)
		ps.HandleAgentConcurrency(context.Background(), sessionID, settable)

		if err := ps.SendSetConcurrencyNotification(context.Background(), 8); err != nil {
			t.Fatalf("SendSetConcurrencyNotification() = %v, want nil", err)
		}
	})

	t.Run("does not record the change it only asked for", func(t *testing.T) {
		// The gateway answers every set with a fresh report — accepted,
		// clamped, or ignored because the value was not a number — and that
		// report is the only thing that knows which. Recording the request
		// would make the interface claim a change that may not have happened.
		ps, sessionID, _ := connectedConcurrencyServer(t)
		ps.HandleAgentConcurrency(context.Background(), sessionID, settable)

		if err := ps.SendSetConcurrencyNotification(context.Background(), 8); err != nil {
			t.Fatalf("SendSetConcurrencyNotification() = %v", err)
		}

		if got := ps.AgentConcurrency().MaxConcurrency; got != 4 {
			t.Errorf("MaxConcurrency = %d, want the gateway's last report of 4", got)
		}
	})

	t.Run("stops offering the control once the session drops its stream", func(t *testing.T) {
		ps, sessionID, _ := connectedConcurrencyServer(t)
		ps.HandleAgentConcurrency(context.Background(), sessionID, settable)
		ps.removeStreamingSession(sessionID)

		if err := ps.SendSetConcurrencyNotification(context.Background(), 8); !errors.Is(err, ErrConcurrencySetUnsupported) {
			t.Errorf("err = %v, want ErrConcurrencySetUnsupported", err)
		}
	})
}

// The gateway destructures `maxConcurrency` and nothing else. Under any other
// spelling the value reads as absent, and the gateway answers by reporting the
// limit it did not change — from the interface, indistinguishable from a
// control that does nothing. Worth its own test precisely because it is a
// mistake that fails in silence.
func TestSetConcurrencyParams(t *testing.T) {
	params := setConcurrencyParams(8)

	if got, ok := params["maxConcurrency"].(int); !ok || got != 8 {
		t.Errorf("params = %+v, want maxConcurrency: 8", params)
	}
	if _, wrong := params["max_concurrency"]; wrong {
		t.Errorf("params carry the snake_case spelling the gateway cannot read: %+v", params)
	}
	if len(params) != 1 {
		t.Errorf("params = %+v, want nothing but the limit", params)
	}
}

func TestPruneAgentConcurrency(t *testing.T) {
	t.Run("drops what departed sessions reported", func(t *testing.T) {
		snapshots := map[string]AgentConcurrencySnapshot{
			"gone": {MaxConcurrency: 2},
			"here": {MaxConcurrency: 4},
		}
		pruneAgentConcurrency(snapshots, []string{"here"})

		if _, ok := snapshots["gone"]; ok {
			t.Error("a departed session's snapshot survived")
		}
		if _, ok := snapshots["here"]; !ok {
			t.Error("a live session's snapshot was dropped")
		}
	})

	t.Run("keeps everything when no session is live", func(t *testing.T) {
		// No live sessions means the server is between connections rather than
		// that every snapshot is stale — and dropping them there would discard
		// the very notification being recorded, since an early call can observe
		// the map before its own session is visible.
		snapshots := map[string]AgentConcurrencySnapshot{"sess": {MaxConcurrency: 4}}
		pruneAgentConcurrency(snapshots, nil)

		if len(snapshots) != 1 {
			t.Errorf("snapshots = %+v, want the recording kept", snapshots)
		}
	})
}

func TestManagerAgentConcurrency(t *testing.T) {
	m := NewManager(func(workspaceID int64, userID string) *WorkspaceServer {
		return &WorkspaceServer{workspaceID: workspaceID, userID: userID}
	})

	t.Run("nothing to report for a workspace with no server running", func(t *testing.T) {
		// Asked for a workspace nobody has connected to, which is the state
		// every workspace is in until a gateway attaches. nil rather than a
		// zeroed snapshot, so the field is omitted from the payload entirely.
		if got := m.AgentConcurrency(999); got != nil {
			t.Errorf("AgentConcurrency(999) = %+v, want nil", got)
		}
	})

	t.Run("asks the workspace's own server", func(t *testing.T) {
		ps, sessionID, _ := connectedConcurrencyServer(t)
		ps.HandleAgentConcurrency(context.Background(), sessionID, AgentConcurrencyParams{
			MaxConcurrency: intPtr(4),
			Active:         2,
			Queued:         3,
			Min:            intPtr(1),
			Max:            intPtr(64),
			CanSet:         boolPtr(true),
		})

		m2 := NewManager(func(int64, string) *WorkspaceServer { return ps })
		m2.Get(7, "user")

		got := m2.AgentConcurrency(7)
		if got == nil || got.MaxConcurrency != 4 || got.Active != 2 || !got.CanSet {
			t.Errorf("AgentConcurrency(7) = %+v", got)
		}
	})
}
