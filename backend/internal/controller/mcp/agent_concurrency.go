// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package mcp

import (
	"context"
	"errors"

	"github.com/mustafaturan/monoflake"
	zlog "github.com/rs/zerolog/log"

	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
)

// AgentConcurrencyNotificationMethod is the channel notification a gateway
// sends to say how many tasks it will run at once, and what its queue is doing.
//
// It arrives on connect — before any task runs — on every reconnect, and again
// after every set, so the newest one for a connection replaces the previous
// rather than adding to it.
const AgentConcurrencyNotificationMethod = "notifications/claude/channel/concurrency"

// AgentSetConcurrencyNotificationMethod is the notification this server sends
// to ask a gateway to change how many tasks it runs at once.
//
// The third server-to-agent command there is, after cancel and set_model. Like
// those it is this project's own invention rather than anything MCP defines, so
// a gateway not written to listen for it drops it in silence — which is what
// CanSet exists to detect before a control is ever offered.
//
// camelCase in its payload, unlike every older notification on this channel.
// Those are snake_case and each carries its fields twice, having gained a
// second spelling after something was already reading the first; this pair was
// born with one spelling and the gateway reads exactly that one. It
// destructures `maxConcurrency` and nothing else, so any other spelling reads
// as no value at all — and the gateway answers by reporting the limit it never
// changed.
const AgentSetConcurrencyNotificationMethod = "notifications/claude/channel/set_concurrency"

// Reasons a concurrency limit cannot be changed, so the handler can say which
// it was rather than answering every refusal the same way.
var (
	// ErrConcurrencySetUnsupported means nothing connected will act on a
	// change: no gateway has reported a limit, none of the reporting sessions
	// still holds a stream, or the one that does is too old to have said it can
	// be told to change it.
	ErrConcurrencySetUnsupported = errors.New("the connected agent does not support changing its concurrency")
	// ErrConcurrencyInvalid means the value asked for is not a limit at all.
	// Out-of-range is a different thing and is not refused here — see
	// SendSetConcurrencyNotification.
	ErrConcurrencyInvalid = errors.New("the concurrency limit must be a whole number of at least 1")
	// ErrConcurrencySetNotDelivered means the session that offered the control
	// could not be reached to be told of the change.
	ErrConcurrencySetNotDelivered = errors.New("the connected agent could not be reached")
)

// AgentConcurrencyParams is the payload of a concurrency notification.
//
// camelCase, unlike the models and commands payloads beside it. Those are
// snake_case, and each carries its fields twice because a second spelling was
// added once something was already reading the first — a cost they now pay
// forever. This pair has no such history: it was born with one spelling, the
// gateway writes exactly that one, and there is no client anywhere that spells
// it the other way. So there is nothing here to be lenient towards, and
// pretending otherwise would invent the very habit this notification was
// designed to avoid.
//
// Pointers, because zero is a meaningful answer for none of these and an absent
// field has to be told apart from a reported one: a limit of 0 is a gateway
// that said nothing rather than one that will run nothing, and `canSet` absent
// means no.
//
// TaskID and SessionID are always empty on this notification and are declared
// only so the frame round-trips; nothing reads them. The limit belongs to the
// gateway process, not to any one task or ACP session.
type AgentConcurrencyParams struct {
	TaskID    string `json:"taskId"`
	SessionID string `json:"sessionId"`

	MaxConcurrency *int `json:"maxConcurrency"`

	// Active is how many tasks are running and Queued how many are waiting.
	// Both are for display: they are the only way to see that a limit did
	// anything.
	Active int `json:"active"`
	Queued int `json:"queued"`

	// Min and Max are the range the control should offer. Read from the report
	// rather than assumed, because the gateway owns its own ceiling and a
	// number compiled in here would be wrong the first time it moved.
	Min *int `json:"min"`
	Max *int `json:"max"`

	// CanSet is the gateway saying it will act on a set_concurrency
	// notification.
	//
	// Absent means no, which is what makes this safe to add: every gateway ever
	// published reports its limit and the older ones ignore being told to
	// change it, so reading silence as "yes" would offer a control that does
	// nothing on most deployments in the field. The same rule, and the same
	// reason, as the model picker's CanSet.
	CanSet *bool `json:"canSet"`
}

// limit is the ceiling the payload reports, 0 when it reported none.
func (p AgentConcurrencyParams) limit() int {
	if p.MaxConcurrency == nil {
		return 0
	}
	return *p.MaxConcurrency
}

// canSet is whether the payload says the limit can be changed. Absent is no.
func (p AgentConcurrencyParams) canSet() bool {
	return p.CanSet != nil && *p.CanSet
}

// AgentConcurrencySnapshot is what one connected session last reported.
type AgentConcurrencySnapshot struct {
	// MaxConcurrency is how many tasks the gateway will run at once.
	MaxConcurrency int
	// Active is how many it is running, and may legitimately exceed
	// MaxConcurrency for a while: lowering the limit never interrupts a running
	// task, so the queue simply stops handing out new ones until the active
	// count falls below it. A reader that treated that as an error state would
	// be flagging the feature working correctly.
	Active int
	// Queued is how many are waiting for a slot.
	Queued int
	// Min and Max are the range a control may offer, as the gateway reported
	// it. Max is 0 when the gateway named no ceiling.
	Min int
	Max int
	// CanSet reports whether the session that sent this will act on a
	// set_concurrency notification. See the field on AgentConcurrencyParams.
	CanSet bool
}

// Reported says whether this snapshot carries a limit worth showing.
//
// A gateway that named no limit has told the interface nothing it can render —
// there is no number for the control to start from — so such a snapshot is
// skipped in favour of one from another session, exactly as an empty model list
// is.
func (s AgentConcurrencySnapshot) Reported() bool {
	return s.MaxConcurrency > 0
}

// Settable reports whether a change can actually be written back.
//
// The one definition of that rule, because it is asked in three places — the
// predicate the interface reads, the guard on the notification, and the flag on
// the workspace payload — and three copies would eventually disagree.
//
// Reported() is half of it for the same reason ConfigID is half of the models'
// rule: a gateway willing to be told a new limit but never saying what the
// current one is leaves a control with nothing to show and no way to tell
// whether a change landed.
func (s AgentConcurrencySnapshot) Settable() bool {
	return s.CanSet && s.Reported()
}

// concurrencySnapshotFrom turns a report into the snapshot the rest of the
// server reads, putting the range into a shape a control can actually use.
//
// A limit outside its own reported range is the case worth spelling out. The
// range and the limit are independent numbers on the wire, and nothing stops a
// gateway started with `--max-concurrency 100` from also reporting `max: 64` —
// at which point a control clamped to the reported range could not display the
// value it was showing, and merely opening it would propose a change nobody
// asked for. So the range is widened to contain the limit rather than the limit
// being quietly rewritten to fit: what the gateway is actually running is the
// one number here that is certainly true.
//
// The floor is 1 whatever the gateway says, because a limit of 0 is a queue
// that never hands anything out — a stall dressed as a setting, and not
// something the interface should offer a way to ask for.
func concurrencySnapshotFrom(p AgentConcurrencyParams) AgentConcurrencySnapshot {
	limit := p.limit()
	if limit < 0 {
		limit = 0
	}

	min := 1
	if p.Min != nil && *p.Min > 1 {
		min = *p.Min
	}
	max := 0
	if p.Max != nil && *p.Max > 0 {
		max = *p.Max
	}
	if max > 0 && max < min {
		max = min
	}
	if limit > 0 {
		if limit < min {
			min = limit
		}
		if max > 0 && limit > max {
			max = limit
		}
	}

	return AgentConcurrencySnapshot{
		MaxConcurrency: limit,
		Active:         p.Active,
		Queued:         p.Queued,
		Min:            min,
		Max:            max,
		CanSet:         p.canSet(),
	}
}

// HandleAgentConcurrency records the concurrency a session reports.
//
// Recorded rather than relayed into the chat: like the models beside it, this
// is not something that happened during a turn but a standing fact about the
// connected gateway, and it belongs with the other live capabilities the
// interface reads off the workspace. Persisting the latest per connection is
// also what lets a page render a number immediately rather than waiting for the
// gateway to say something.
func (ps *WorkspaceServer) HandleAgentConcurrency(ctx context.Context, sessionID string, p AgentConcurrencyParams) {
	// Keyed by the MCP transport session, deliberately, and *not* by the
	// payload's session_id — which on this notification is always empty anyway.
	// The two are different namespaces: the transport session is this server's
	// connection to the gateway, while session_id is the gateway's own ACP
	// session with the agent behind it. agentModels next door keys the same way
	// for the same reason, and the limit is in any case a property of the
	// gateway process rather than of any session it holds.
	snapshot := concurrencySnapshotFrom(p)

	ps.agentConcurrencyMu.Lock()
	previous, had := ps.agentConcurrency[sessionID]
	changed := !had || previous != snapshot
	ps.agentConcurrency[sessionID] = snapshot
	pruneAgentConcurrency(ps.agentConcurrency, ps.liveSessionIDs())
	ps.agentConcurrencyMu.Unlock()

	zlog.Debug().
		Str("session_id", sessionID).
		Int("max_concurrency", snapshot.MaxConcurrency).
		Int("active", snapshot.Active).
		Int("queued", snapshot.Queued).
		Bool("can_set", snapshot.CanSet).
		Bool("changed", changed).
		Msg("recorded the concurrency the gateway reports")

	// Published every time, even when the numbers did not move — which is where
	// this deliberately parts company with the models notification next door.
	//
	// That one is re-sent on every session config change and says the same
	// thing nearly every time, so suppressing the repeats is the difference
	// between a quiet bus and a broadcast storm. This one arrives three times:
	// on connect, on reconnect, and in answer to a set. The gateway's contract
	// is that a set is *always* answered, accepted or not, precisely so a client
	// that moved a control learns where it landed — and a gateway that refused
	// the value, or clamped it to the limit already in force, answers with
	// numbers identical to the last report. Suppressing that would swallow the
	// one message the waiting control exists to receive, leaving it to time out
	// over a request that was answered immediately.
	ps.publishAgentConcurrency(snapshot)
}

// publishAgentConcurrency tells the human clients what the gateway now reports.
//
// Without this the limit reaches a client only in the workspace payload it
// fetched on load, and the numbers that matter most here — what is running and
// what is waiting — change well after that. It is also what makes a set visible
// at all: the gateway always answers one with a fresh report, and that report
// is the only thing that knows whether the value was accepted, clamped or
// ignored.
//
// What goes out is the report just received, not the answer AgentConcurrency
// would give. The session that reported is connected by definition — it has
// this instant spoken — whereas the session registry only learns of a transport
// when it registers its stream, so asking it here answers a question about
// timing rather than about the gateway.
//
// A withdrawal is the one case that has to look wider: another gateway may
// still be reporting a limit, and taking the control away because this one
// stopped would remove something still live.
func (ps *WorkspaceServer) publishAgentConcurrency(reported AgentConcurrencySnapshot) {
	published := reported
	if !published.Reported() {
		published = AgentConcurrencySnapshot{}
		if snapshot := ps.AgentConcurrency(); snapshot != nil {
			published = *snapshot
		}
	}

	// camelCase because everything a browser reads is, and because these fields
	// land in the same store slot the workspace payload fills — they have to
	// spell themselves the same way or the event would write a second shape
	// into it. That the gateway happens to send camelCase too is a coincidence
	// of this pair being younger than its snake_case neighbours on the channel,
	// not a reason to pass the payload through unexamined.
	//
	// Base62 for the workspace ID, the way every other event on this bus
	// carries one: the REST API only ever names a workspace that way, so a raw
	// int64 here would match nothing the frontend holds.
	ps.bus.Publish(ps.workspaceID, ps.userID, eventbus.Event{
		Type: "agent.concurrency",
		Payload: map[string]any{
			"maxConcurrency": published.MaxConcurrency,
			"active":         published.Active,
			"queued":         published.Queued,
			"min":            published.Min,
			"max":            published.Max,
			// Settable(), not the raw field, so this agrees with the REST
			// payload. A gateway willing to be told a limit but naming none
			// would otherwise light up a control here that the workspace
			// payload says does not exist.
			"canSet":      published.Settable(),
			"workspaceId": monoflake.ID(ps.workspaceID).String(),
		},
	})
}

// AgentConcurrency reports what the connected gateway is running, or nil when
// nothing connected has said.
//
// Only sessions that still hold a stream are considered. The server's session
// list is not enough on its own: an MCP session outlives the stream that
// carried it, so after a gateway goes away its session is still listed and the
// snapshot keyed to it still looks live.
func (ps *WorkspaceServer) AgentConcurrency() *AgentConcurrencySnapshot {
	ps.agentConcurrencyMu.RLock()
	defer ps.agentConcurrencyMu.RUnlock()
	_, snapshot := pickAgentConcurrencySession(ps.agentConcurrency, ps.streamingSessionIDs())
	return snapshot
}

// pickAgentConcurrencySession chooses the snapshot to report and names the
// transport session that sent it.
//
// A change has to reach *that* session and no other, so the two answers come
// back together — the snapshot alone cannot say where to send anything.
//
// A gateway that can be told a new limit wins over one that can only report
// its own. Two gateways on one workspace is a case this package contemplates
// throughout, and without the preference the answer is decided by session
// order: an older gateway would mask a newer one attached beside it, and the
// workspace would show a read-only number while something perfectly capable was
// connected. The fallback is what keeps that read-only number on display when
// it is all there is, which is what most deployments look like today.
//
// `live` is iterated rather than the map so the answer follows the session
// order the server reports rather than Go's randomised map order, which would
// otherwise make two identical calls disagree.
func pickAgentConcurrencySession(snapshots map[string]AgentConcurrencySnapshot, live []string) (string, *AgentConcurrencySnapshot) {
	var fallbackID string
	var fallback *AgentConcurrencySnapshot

	for _, id := range live {
		snapshot, ok := snapshots[id]
		if !ok || !snapshot.Reported() {
			continue
		}
		if snapshot.Settable() {
			return id, &snapshot
		}
		if fallback == nil {
			fallbackID, fallback = id, &snapshot
		}
	}
	return fallbackID, fallback
}

// SendSetConcurrencyNotification asks the connected gateway to run a different
// number of tasks at once.
//
// Addressed to the one session that offered the control, deliberately, and not
// broadcast the way a stop is. A stop is broadcast because any session may hold
// the turn and the point of the button is that it works; this is the opposite —
// it reconfigures one gateway's queue, and sending it to a second gateway would
// change the throughput of an agent the human was not looking at.
//
// Out of range is *not* refused here. The gateway clamps a value to its own
// min/max and answers with a fresh report either way, so forwarding is both
// what the contract says and the honest thing to do: refusing on a range this
// server merely caches would reject a perfectly good value whenever a gateway
// had widened its ceiling since the last report. What is refused is a value
// that is not a limit at all — a limit below 1 is a queue that never hands
// anything out, and asking for one would be asking a gateway to stall.
//
// Nothing is recorded optimistically. The gateway answers every set with a
// concurrency notification — accepted, clamped or ignored — and that report is
// the only thing that knows which of the three it was.
func (ps *WorkspaceServer) SendSetConcurrencyNotification(ctx context.Context, limit int) error {
	if limit < 1 {
		return ErrConcurrencyInvalid
	}

	ps.agentConcurrencyMu.RLock()
	sessID, snapshot := pickAgentConcurrencySession(ps.agentConcurrency, ps.streamingSessionIDs())
	ps.agentConcurrencyMu.RUnlock()

	if snapshot == nil || !snapshot.Settable() {
		return ErrConcurrencySetUnsupported
	}

	params := setConcurrencyParams(limit)
	// Narrow but real: streamingSessionIDs and notifySession read the same
	// session list, so a session named by the first is addressable by the
	// second — unless the gateway disconnects between the two, which is exactly
	// when a human is most likely to be clicking. Reported rather than
	// swallowed, so the control reverts instead of appearing to have worked.
	if !ps.notifySession(ctx, sessID, AgentSetConcurrencyNotificationMethod, params) {
		return ErrConcurrencySetNotDelivered
	}

	zlog.Info().
		Int64("workspace_id", ps.workspaceID).
		Str("session_id", sessID).
		Int("max_concurrency", limit).
		Msg("asked the connected gateway to change its concurrency")
	return nil
}

// setConcurrencyParams is the payload of a set_concurrency notification.
//
// camelCase, and only camelCase. The gateway destructures `maxConcurrency` and
// nothing else, so any other spelling reads as no value at all — and the
// gateway then answers by reporting the limit it had not changed, which from
// the interface is indistinguishable from a control that does nothing.
//
// Its own function so that spelling can be pinned by a test without reaching
// through a live session: this is a mistake that fails silently, and a silent
// failure deserves an assertion that cannot be skipped.
func setConcurrencyParams(limit int) map[string]any {
	return map[string]any{"maxConcurrency": limit}
}

// pruneAgentConcurrency drops the snapshots of sessions that have gone away.
//
// There is no disconnect hook to hang this on, and pickAgentConcurrencySession
// already ignores dead sessions, so this is about the map rather than about
// correctness: a long-lived workspace whose gateway reconnects repeatedly would
// otherwise accumulate one snapshot per session for the life of the process.
//
// No live sessions at all means the server is between connections rather than
// that every snapshot is stale — and dropping them there would discard the very
// notification being recorded, since a test or an early call can observe the
// map before the session is visible.
//
// The caller must hold the write lock.
func pruneAgentConcurrency(snapshots map[string]AgentConcurrencySnapshot, live []string) {
	if len(live) == 0 {
		return
	}

	keep := make(map[string]bool, len(live))
	for _, id := range live {
		keep[id] = true
	}
	for id := range snapshots {
		if !keep[id] {
			delete(snapshots, id)
		}
	}
}
