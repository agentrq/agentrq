package mcp

import (
	"context"

	zlog "github.com/rs/zerolog/log"
)

// AgentModelsNotificationMethod is the channel notification a gateway sends to
// say which models the agent behind it can switch between.
//
// It arrives whenever the agent's session config changes, so the newest one for
// a session replaces the previous rather than adding to it.
const AgentModelsNotificationMethod = "notifications/claude/channel/models"

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
	}

	ps.agentModelsMu.Lock()
	ps.agentModels[sessionID] = snapshot
	pruneAgentModels(ps.agentModels, ps.liveSessionIDs())
	ps.agentModelsMu.Unlock()

	zlog.Debug().
		Str("session_id", sessionID).
		Int("models", len(p.Models)).
		Str("current_model", snapshot.CurrentModel).
		Msg("recorded the models the agent offers")
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
	for _, id := range live {
		snapshot, ok := snapshots[id]
		if !ok || len(snapshot.Models) == 0 {
			continue
		}
		return &snapshot
	}
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
