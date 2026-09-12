// Copyright 2026 Contextual, Inc. https://agentrq.com

package mcp

import (
	"context"
	"slices"

	"github.com/mustafaturan/monoflake"
	zlog "github.com/rs/zerolog/log"

	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
)

// AgentCommandsNotificationMethod is the channel notification a gateway sends
// to say which slash commands the agent behind it offers.
//
// An ACP agent advertises them whenever context makes them relevant, so the
// newest list for a session replaces the previous one rather than adding to it.
const AgentCommandsNotificationMethod = "notifications/claude/channel/commands"

// AgentCommand is one slash command the connected agent offers.
type AgentCommand struct {
	// Name is typed as /<name> at the start of a message.
	Name string `json:"name"`
	// Description says what the command does, and may be empty: the name is
	// what gets invoked, and an agent is not obliged to explain itself.
	Description string `json:"description,omitempty"`
	// Hint describes the argument, for the commands that take one.
	Hint string `json:"hint,omitempty"`
}

// AgentCommandsParams is the payload of a commands notification.
//
// snake_case, unlike the REST surface: this is the wire format the gateways
// already send (acp-gateway's `sendCommandsToWorkspace`), and renaming the
// fields here would break every gateway in the field to satisfy a convention
// that governs the HTTP API rather than this channel.
type AgentCommandsParams struct {
	TaskID    string         `json:"task_id"`
	SessionID string         `json:"session_id"`
	Commands  []AgentCommand `json:"commands"`
}

// AgentCommandsSnapshot is what one connected session last reported.
//
// SessionID is the *agent's* session — see the note on keying in
// HandleAgentCommands — and is kept because it is the only thing tying a
// snapshot back to the conversation it came from when more than one session is
// connected.
type AgentCommandsSnapshot struct {
	SessionID string
	Commands  []AgentCommand
}

// HandleAgentCommands records the slash commands a session reports it offers.
//
// Recorded rather than relayed into the chat, like the models beside it: this
// is not something that happened during a turn, it is a standing fact about the
// connected agent, and it belongs with the other live capabilities the
// interface reads off the workspace.
func (ps *WorkspaceServer) HandleAgentCommands(ctx context.Context, sessionID string, p AgentCommandsParams) {
	// Keyed by the MCP transport session, deliberately, and *not* by the
	// payload's session_id.
	//
	// The two are different namespaces: the transport session is this server's
	// connection to the gateway, while `session_id` is the gateway's own ACP
	// session with the agent behind it. Keying on the payload would put entries
	// under IDs that liveSessionIDs never reports, so every snapshot would look
	// as though it belonged to a session that had gone and none would ever be
	// advertised. agentModels next door keys the same way for the same reason.
	//
	// An empty key is fine rather than something to reject: some transports
	// leave the session ID blank, and the reader filters against the same list
	// this is keyed on, so the two stay consistent either way.
	//
	// An empty command list is recorded rather than dropped: it is how an agent
	// says it has withdrawn its commands, and keeping the old list would leave
	// a menu offering commands the agent no longer accepts.
	snapshot := AgentCommandsSnapshot{
		SessionID: p.SessionID,
		Commands:  p.Commands,
	}

	ps.agentCommandsMu.Lock()
	previous, had := ps.agentCommands[sessionID]
	changed := !had || !slices.Equal(previous.Commands, p.Commands)
	ps.agentCommands[sessionID] = snapshot
	pruneAgentCommands(ps.agentCommands, ps.liveSessionIDs())
	ps.agentCommandsMu.Unlock()

	zlog.Debug().
		Str("session_id", sessionID).
		Int("commands", len(p.Commands)).
		Bool("changed", changed).
		Msg("recorded the slash commands the agent offers")

	// An agent that re-advertises the same list — which it does whenever the
	// commands survive a context change — is not news, and telling every open
	// tab about it would be a broadcast storm for a menu that did not move.
	if !changed {
		return
	}
	ps.publishAgentCommands(snapshot)
}

// publishAgentCommands tells the human clients what the workspace now offers.
//
// The commands arrive long after any page load — an agent advertises them once
// its session is up, which is after it has taken a task — so a client that only
// ever read them from the workspace payload would show an empty menu for the
// rest of its life. This is the same reason publishAgentConnected exists.
//
// What goes out is the list just reported, not the answer AgentCommands would
// give. The session that reported is connected by definition — it has this
// instant spoken — whereas the session registry only learns about a transport
// when it registers its stream, so asking it here answers a question about
// timing rather than about the agent.
//
// A withdrawal is the one case that has to look wider: another session may
// still be offering commands, and taking the menu away because this one stopped
// would remove something still live. So an empty report falls back to whatever
// the workspace would serve, which is an empty list when the answer is nothing.
func (ps *WorkspaceServer) publishAgentCommands(reported AgentCommandsSnapshot) {
	commands := reported.Commands
	if len(commands) == 0 {
		commands = []AgentCommand{}
		if snapshot := ps.AgentCommands(); snapshot != nil {
			commands = snapshot.Commands
		}
	}

	// Base62, the way every other event on this bus carries a workspace ID: the
	// REST API only ever names a workspace that way (see view.Workspace), so a
	// raw int64 here would match nothing the frontend holds and the menu would
	// never move off whatever the last page load fetched.
	ps.bus.Publish(ps.workspaceID, ps.userID, eventbus.Event{
		Type: "agent.commands",
		Payload: map[string]any{
			"commands":    commands,
			"workspaceId": monoflake.ID(ps.workspaceID).String(),
		},
	})
}

// AgentCommands reports the slash commands the connected agent offers, or nil
// when nothing connected has said.
//
// Only sessions that still hold a stream are considered, for the reason
// AgentModels records: a session outlives its stream, so the session list alone
// would keep a departed agent's commands on offer. Every one of them would fail
// — and worse, a reply that opens with one is delivered stripped of the
// envelope that gives it context, on the strength of a menu nobody is behind.
func (ps *WorkspaceServer) AgentCommands() *AgentCommandsSnapshot {
	ps.agentCommandsMu.RLock()
	defer ps.agentCommandsMu.RUnlock()
	return pickAgentCommands(ps.agentCommands, ps.streamingSessionIDs())
}

// pickAgentCommands chooses the snapshot to report from what sessions have said.
//
// Only live sessions count. The snapshot arrives by notification and is
// therefore cached, so without this filter a workspace would go on offering the
// commands of an agent that disconnected hours ago, and every one of them would
// fail.
//
// A session that reported an empty list is skipped rather than returned: it has
// withdrawn its commands, and another session may still have some worth
// showing. `live` is iterated rather than the map so the answer follows the
// session order the server reports rather than Go's randomised map order, which
// would otherwise make the choice differ between two identical calls.
func pickAgentCommands(snapshots map[string]AgentCommandsSnapshot, live []string) *AgentCommandsSnapshot {
	for _, id := range live {
		snapshot, ok := snapshots[id]
		if !ok || len(snapshot.Commands) == 0 {
			continue
		}
		return &snapshot
	}
	return nil
}

// pruneAgentCommands drops the snapshots of sessions that have gone away.
//
// There is no disconnect hook to hang this on, and pickAgentCommands already
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
func pruneAgentCommands(snapshots map[string]AgentCommandsSnapshot, live []string) {
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
