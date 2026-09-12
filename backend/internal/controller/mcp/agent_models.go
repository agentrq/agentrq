// Copyright 2026 Contextual, Inc. https://agentrq.com

package mcp

import (
	"context"
	"errors"
	"slices"

	"github.com/mustafaturan/monoflake"
	zlog "github.com/rs/zerolog/log"

	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
)

// AgentModelsNotificationMethod is the channel notification a gateway sends to
// say which models the agent behind it can switch between.
//
// It arrives whenever the agent's session config changes, so the newest one for
// a session replaces the previous rather than adding to it.
const AgentModelsNotificationMethod = "notifications/claude/channel/models"

// AgentSetModelNotificationMethod is the notification this server sends to ask
// a gateway to switch the agent behind it to a different model.
//
// The second server-to-agent command there is, after cancel. Like that one it
// is this project's own invention rather than anything MCP defines, so a client
// not written to listen for it drops it in silence — which is what CanSet
// exists to detect before a picker is ever offered.
//
// snake_case in its payload, like every notification on this channel: the wire
// format belongs to the gateways, and the REST surface's camelCase stops at the
// handler.
const AgentSetModelNotificationMethod = "notifications/claude/channel/set_model"

// Reasons a model cannot be selected, so the handler can say which it was
// rather than answering every refusal the same way.
var (
	// ErrModelSelectUnsupported means nothing connected will act on a
	// selection: no models reported, none of the reporting sessions still
	// holding a stream, or a gateway too old to have said it can set one.
	ErrModelSelectUnsupported = errors.New("the connected agent does not support choosing a model")
	// ErrModelNotOffered means the agent is selectable but was asked for a
	// model it never advertised.
	ErrModelNotOffered = errors.New("the connected agent does not offer that model")
	// ErrModelSetNotDelivered means the session that offered the choice could
	// not be reached to be told of it.
	ErrModelSetNotDelivered = errors.New("the connected agent could not be reached")
)

// AgentModel is one model the connected agent offers.
type AgentModel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Current     bool   `json:"current,omitempty"`
	Group       string `json:"group,omitempty"`
}

// AgentModelsParams is the payload of a models notification.
//
// snake_case, unlike the REST surface: this is the wire format the gateways
// already send (acp-gateway's `sendModelsToWorkspace`), and renaming the fields
// here would break every gateway in the field to satisfy a convention that
// governs the HTTP API rather than this channel.
type AgentModelsParams struct {
	TaskID       string       `json:"task_id"`
	SessionID    string       `json:"session_id"`
	ConfigID     string       `json:"config_id"`
	CurrentModel string       `json:"current_model"`
	Models       []AgentModel `json:"models"`
	// CanSet is the gateway saying it will act on a set_model notification.
	//
	// Absent means no, which is what makes this safe to add: every gateway
	// already in the field reports its models and none of them can be told to
	// change one, so reading silence as "yes" would offer a picker that does
	// nothing on every deployment that exists today.
	//
	// It has to come from the gateway rather than from a list of client names
	// here, because the question is about the gateway's *version*, and the
	// handshake name — "acp-gateway" either way — cannot tell two versions
	// apart. stopCapableClients can be a name list precisely because stopping
	// has been there since that client's first release.
	CanSet bool `json:"can_set,omitempty"`
}

// AgentModelsSnapshot is what one connected session last reported.
//
// ConfigID is carried through because selecting a model means writing back to
// the same session config option the agent advertised it under; a snapshot
// without it describes models nobody can switch to. SessionID is the *agent's*
// session — see the note on keying in HandleAgentModels — and is kept for the
// same reason: a future "use this model" has to name it.
type AgentModelsSnapshot struct {
	SessionID    string
	ConfigID     string
	CurrentModel string
	Models       []AgentModel
	// CanSet reports whether the session that sent this will act on a
	// set_model notification. See the field on AgentModelsParams.
	CanSet bool
}

// Selectable reports whether a choice can actually be written back.
//
// The one definition of that rule, because it is asked in three places — the
// predicate the interface reads, the guard on the notification, and the flag on
// the workspace payload — and three copies of "CanSet && ConfigID != \"\"" would
// eventually disagree about whether a gateway that forgot its config option
// counts.
//
// ConfigID is half of it because a selection is a write to that session config
// option: a snapshot without one describes models nobody can switch to, however
// willing the gateway says it is.
func (s AgentModelsSnapshot) Selectable() bool {
	return s.CanSet && s.ConfigID != ""
}

// currentModelID is the model the payload says is selected.
//
// The top-level field is preferred, falling back to whichever entry carries the
// agent's own `current` flag: gateways send one or the other, and a picker with
// nothing selected is worse than either.
func (p AgentModelsParams) currentModelID() string {
	if p.CurrentModel != "" {
		return p.CurrentModel
	}
	for _, m := range p.Models {
		if m.Current {
			return m.ID
		}
	}
	return ""
}

// HandleAgentModels records the models a session reports it can switch between.
//
// Recorded rather than relayed into the chat: unlike agent telemetry, this is
// not something that happened during a turn — it is a standing fact about the
// connected agent, and it belongs beside the other live capabilities the
// dashboard reads off the workspace.
func (ps *WorkspaceServer) HandleAgentModels(ctx context.Context, sessionID string, p AgentModelsParams) {
	// Keyed by the MCP transport session, deliberately, and *not* by the
	// payload's session_id.
	//
	// The two are different namespaces: the transport session is this server's
	// connection to the gateway, while `session_id` is the gateway's own ACP
	// session with the agent behind it. Keying on the payload would put entries
	// under IDs that liveSessionIDs never reports, so every snapshot would look
	// as though it belonged to a session that had gone and none would ever be
	// advertised. sessionTasks next door keys the same way for the same reason.
	//
	// An empty key is fine rather than something to reject: some transports
	// leave the session ID blank, and the reader filters against the same list
	// this is keyed on, so the two stay consistent either way.
	//
	// An empty model list is recorded rather than dropped: it is how a session
	// says it has stopped offering a choice, and keeping the old list would
	// leave a picker showing models the agent no longer accepts.
	snapshot := AgentModelsSnapshot{
		SessionID:    p.SessionID,
		ConfigID:     p.ConfigID,
		CurrentModel: p.currentModelID(),
		Models:       p.Models,
		CanSet:       p.CanSet,
	}

	ps.agentModelsMu.Lock()
	previous, had := ps.agentModels[sessionID]
	changed := !had || !sameAgentModels(previous, snapshot)
	ps.agentModels[sessionID] = snapshot
	pruneAgentModels(ps.agentModels, ps.liveSessionIDs())
	ps.agentModelsMu.Unlock()

	zlog.Debug().
		Str("session_id", sessionID).
		Int("models", len(p.Models)).
		Str("current_model", snapshot.CurrentModel).
		Bool("changed", changed).
		Msg("recorded the models the agent offers")

	// An agent re-advertises its models on every session config change, so most
	// reports say exactly what the last one said — a thinking-effort switch
	// re-sends the whole model list unchanged. Telling every open tab about
	// those would be a broadcast storm for a picker that did not move.
	if !changed {
		return
	}
	ps.publishAgentModels(snapshot)
}

// sameAgentModels reports whether two snapshots would publish the same thing.
//
// Compared on exactly the fields publishAgentModels sends, rather than on the
// whole snapshot: SessionID is not published, and a gateway that reconnects its
// ACP session while offering an identical list has not changed anything a
// reader can see. Comparing it would put that reader through a needless update.
//
// CurrentModel is part of the comparison and not merely a function of the list.
// A gateway may send the top-level field alone, without the per-model `current`
// flag, and then a model switch changes nothing else — which is precisely the
// event this whole path exists to carry.
func sameAgentModels(a, b AgentModelsSnapshot) bool {
	return a.ConfigID == b.ConfigID &&
		a.CurrentModel == b.CurrentModel &&
		a.CanSet == b.CanSet &&
		slices.Equal(a.Models, b.Models)
}

// publishAgentModels tells the human clients what the workspace now offers.
//
// Without this the models reach a client only in the workspace payload it
// fetched on load, and an agent reports them well after that — on connecting,
// and again whenever its session config changes. So the name on the Overview
// card was fixed at page load and a model switch never showed. This is the same
// reason publishAgentCommands exists beside it.
//
// What goes out is the report just received, not the answer AgentModels would
// give. The session that reported is connected by definition — it has this
// instant spoken — whereas the session registry only learns of a transport when
// it registers its stream, so asking it here answers a question about timing
// rather than about the agent.
//
// A withdrawal is the one case that has to look wider: another session may
// still be offering models, and taking the picker away because this one stopped
// would remove something still live. So an empty report falls back to whatever
// the workspace would serve, which is an empty list when the answer is nothing.
func (ps *WorkspaceServer) publishAgentModels(reported AgentModelsSnapshot) {
	published := reported
	if len(published.Models) == 0 {
		published = AgentModelsSnapshot{}
		if snapshot := ps.AgentModels(); snapshot != nil {
			published = *snapshot
		}
	}

	// Never null, always a list: a client that reads the payload straight into
	// its store would otherwise have to tell "no models" apart from "the field
	// was absent", and the two mean the same thing here.
	models := published.Models
	if models == nil {
		models = []AgentModel{}
	}

	// camelCase, unlike the notification this came from. The wire format the
	// gateways send is snake_case and stays that way, but what goes to a browser
	// is the REST surface's convention, and these fields are read straight into
	// the same store slot the workspace payload fills.
	//
	// Base62 for the workspace ID, the way every other event on this bus carries
	// one: the REST API only ever names a workspace that way (see view.Workspace),
	// so a raw int64 here would match nothing the frontend holds and the picker
	// would never move off whatever the last page load fetched.
	ps.bus.Publish(ps.workspaceID, ps.userID, eventbus.Event{
		Type: "agent.models",
		Payload: map[string]any{
			"configId":     published.ConfigID,
			"currentModel": published.CurrentModel,
			// Selectable(), not the raw field, so this agrees with the REST
			// payload. A gateway willing to switch but naming no config option
			// to switch through would otherwise light up a picker here that the
			// workspace payload says does not exist — appearing on an event,
			// failing on use, and vanishing on the next reload.
			"canSet":      published.Selectable(),
			"models":      models,
			"workspaceId": monoflake.ID(ps.workspaceID).String(),
		},
	})
}

// AgentModels reports what the connected agent can switch between, or nil when
// nothing connected has said.
//
// Only sessions that still hold a stream are considered. The server's session
// list is not enough on its own: an MCP session outlives the stream that
// carried it, so after a gateway goes away its session is still listed and the
// snapshot keyed to it still looks live — which is exactly what the filter
// below exists to prevent, and what it cannot see by itself.
//
// Asking per session rather than "is anything connected" is what makes this
// right when two gateways are attached: one of them leaving must take its own
// models with it and leave the other's alone.
func (ps *WorkspaceServer) AgentModels() *AgentModelsSnapshot {
	ps.agentModelsMu.RLock()
	defer ps.agentModelsMu.RUnlock()
	return pickAgentModels(ps.agentModels, ps.streamingSessionIDs())
}

// liveSessionIDs lists the sessions currently connected to this workspace.
func (ps *WorkspaceServer) liveSessionIDs() []string {
	if ps.mcpServer == nil {
		return nil
	}
	var ids []string
	for sess := range ps.mcpServer.Sessions() {
		ids = append(ids, sess.ID())
	}
	return ids
}

// pickAgentModels chooses the snapshot to report from what sessions have said.
//
// Only live sessions count. The snapshot arrives by notification and is
// therefore cached, so without this filter a workspace would go on advertising
// the models of an agent that disconnected hours ago, and a picker built on it
// would offer a choice nothing could act on.
//
// A session that reported an empty list is skipped rather than returned: it has
// withdrawn its offer, and another session may still have one worth showing.
// `live` is iterated rather than the map so the answer follows the session
// order the server reports rather than Go's randomised map order, which would
// otherwise make the choice differ between two identical calls.
func pickAgentModels(snapshots map[string]AgentModelsSnapshot, live []string) *AgentModelsSnapshot {
	_, snapshot := pickAgentModelsSession(snapshots, live)
	return snapshot
}

// pickAgentModelsSession is pickAgentModels, also naming the transport session
// that reported the answer.
//
// Selecting a model has to reach *that* session and no other. The snapshot
// alone cannot say where to send it: its SessionID is the gateway's own ACP
// session with the agent, a different namespace from the transport session this
// server would address — the trap HandleAgentModels documents at length.
// A session that can actually be switched wins over one that merely has models
// to show. Two gateways on one workspace is a case this file contemplates
// throughout, and without the preference the answer is decided by session
// order: an older gateway that reports its models and ignores being told to
// change one would mask a newer one attached beside it, so the workspace would
// report canSet false and refuse the switch while something perfectly capable
// was connected.
//
// The fallback keeps a read-only gateway's models on display when that is all
// there is, which is what every deployment looks like today.
func pickAgentModelsSession(snapshots map[string]AgentModelsSnapshot, live []string) (string, *AgentModelsSnapshot) {
	var fallbackID string
	var fallback *AgentModelsSnapshot

	for _, id := range live {
		snapshot, ok := snapshots[id]
		if !ok || len(snapshot.Models) == 0 {
			continue
		}
		if snapshot.Selectable() {
			return id, &snapshot
		}
		if fallback == nil {
			fallbackID, fallback = id, &snapshot
		}
	}
	return fallbackID, fallback
}

// SendSetModelNotification asks the connected agent to switch to a model.
//
// Addressed to the one session that offered the choice, deliberately, and not
// broadcast the way a stop is. A stop is broadcast because any session may be
// the one holding the turn and the point of the button is that it works; a
// model selection is the opposite — it is a write against one gateway's session
// config, and sending it to a second gateway would change a model nobody asked
// about on an agent the human was not looking at.
//
// The model is checked against what that session actually advertised. Refusing
// here is what keeps a stale picker honest: a tab that loaded before the agent
// narrowed its list would otherwise write an ID the agent no longer accepts,
// and the failure would surface as silence rather than as a message.
func (ps *WorkspaceServer) SendSetModelNotification(ctx context.Context, modelID string) error {
	ps.agentModelsMu.RLock()
	sessID, snapshot := pickAgentModelsSession(ps.agentModels, ps.streamingSessionIDs())
	ps.agentModelsMu.RUnlock()

	if snapshot == nil || !snapshot.Selectable() {
		return ErrModelSelectUnsupported
	}
	if !slices.ContainsFunc(snapshot.Models, func(m AgentModel) bool { return m.ID == modelID }) {
		return ErrModelNotOffered
	}

	// snake_case, the wire format every gateway already speaks on this channel.
	// session_id is the gateway's own ACP session, which is what it needs to
	// write the config option against; sessID above is this server's transport
	// to that gateway, which is how the notification finds it. The two are
	// different namespaces and both are needed.
	params := map[string]any{
		"session_id": snapshot.SessionID,
		"config_id":  snapshot.ConfigID,
		"model_id":   modelID,
	}
	// Narrow but real: streamingSessionIDs and notifySession read the same
	// session list, so a session named by the first is addressable by the
	// second — unless the gateway disconnects between the two, which is exactly
	// when a human is most likely to be clicking. Reported rather than
	// swallowed, so the picker reverts instead of appearing to have worked.
	if !ps.notifySession(ctx, sessID, AgentSetModelNotificationMethod, params) {
		return ErrModelSetNotDelivered
	}

	// Deliberately not recorded as the new current model here. The agent
	// answers with a models notification once the switch has actually happened,
	// and that report is the only thing that knows whether it did. Writing the
	// snapshot optimistically would make the interface claim a switch that the
	// agent may have refused.
	zlog.Info().
		Int64("workspace_id", ps.workspaceID).
		Str("session_id", sessID).
		Str("model_id", modelID).
		Msg("asked the connected agent to switch model")
	return nil
}

// pruneAgentModels drops the snapshots of sessions that have gone away.
//
// There is no disconnect hook to hang this on, and pickAgentModels already
// ignores dead sessions, so this is about the map rather than about
// correctness: a long-lived workspace whose agent reconnects repeatedly would
// otherwise accumulate one snapshot per session for the life of the process.
//
// No live sessions at all means the server is between connections rather than
// that every snapshot is stale — and dropping them there would discard the very
// notification being recorded, since a test or an early call can observe the
// map before the session is visible.
//
// The caller must hold the write lock.
func pruneAgentModels(snapshots map[string]AgentModelsSnapshot, live []string) {
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
