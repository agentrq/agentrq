// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package wire

import (
	"encoding/json"
	"fmt"
)

// Op names a control message. Control is the only part of the protocol that is
// JSON: it is low-rate, it benefits from being readable in a log, and unlike
// terminal traffic it is not arbitrary bytes.
type Op string

const (
	OpHello           Op = "hello"           // daemon → backend, first message
	OpHeartbeat       Op = "heartbeat"       // daemon → backend, liveness + metrics
	OpStartSession    Op = "startSession"    // backend → daemon
	OpKillSession     Op = "killSession"     // backend → daemon
	OpRemoveForkDir   Op = "removeForkDir"   // backend → daemon, a merge asked for the folder to go
	OpSessionState    Op = "sessionState"    // daemon → backend
	OpAttach          Op = "attach"          // backend → daemon
	OpDetach          Op = "detach"          // backend → daemon
	OpUpdateAvailable Op = "updateAvailable" // daemon → backend
	OpUpdateNow       Op = "updateNow"       // backend → daemon, the user approved
	OpRestart         Op = "restart"         // backend → daemon, the user asked
	OpPresence        Op = "presence"        // backend → viewer, who else is watching
	OpError           Op = "error"           // either way, always correlated
	OpListAcpAgents   Op = "listAcpAgents"   // backend → daemon, correlated by ID
	OpAcpAgents       Op = "acpAgents"       // daemon → backend, reply
	OpListAcpModels   Op = "listAcpModels"   // backend → daemon, correlated by ID
	OpAcpModels       Op = "acpModels"       // daemon → backend, reply
)

// Control is the envelope every control message shares.
//
// ID correlates a reply with its request. It is set by whichever side is
// asking; a message that is not a reply leaves it empty. Errors carry the ID of
// whatever provoked them, because an uncorrelated error is a log line rather
// than something a caller can act on.
type Control struct {
	ID string `json:"id,omitempty"`
	Op Op     `json:"op"`
	// Body is the op-specific payload, left as raw JSON so this package does
	// not have to know every message shape — and so a daemon can receive an op
	// from a newer backend without failing to parse the envelope.
	Body json.RawMessage `json:"body,omitempty"`
}

// Resize is the payload of a [TypeResize] frame.
type Resize struct {
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// Hello is the first message a daemon sends.
//
// It is informational: the token has already decided who this is, and nothing
// here is trusted for authorisation. What it is for is the control panel being
// able to say which version a machine is running without waiting for an
// update check, and the log line that records a reconnection being useful.
//
// Sessions lists what this daemon believes is still running. After a restart
// it is empty, which is how the backend learns that rows it thinks are running
// are not.
type Hello struct {
	Version  string   `json:"version"`
	OS       string   `json:"os,omitempty"`
	Arch     string   `json:"arch,omitempty"`
	Hostname string   `json:"hostname,omitempty"`
	Sessions []uint64 `json:"sessions,omitempty"`
	// Capabilities names what this daemon can do beyond the protocol's
	// baseline. A daemon ignores a StartSession field it does not know, so a
	// launch that needs one is refused by the backend unless it is listed here.
	Capabilities []string `json:"capabilities,omitempty"`
}

// CapabilityFork is a daemon that honours [StartSession.Fork]. Without it a
// fork would run in its parent's folder and connect as the parent.
const CapabilityFork = "fork"

// CapabilityForkCleanup is a daemon that honours [OpRemoveForkDir]. An older
// one ignores the op, so a merge that asks for the folder to go is refused
// unless this is listed.
const CapabilityForkCleanup = "removeForkDir"

// CapabilityClaudeOptions is a daemon that passes [StartSession.Model] and
// [StartSession.Effort] to claude-code. An older one drops both and starts
// Claude's defaults.
const CapabilityClaudeOptions = "claudeOptions"

// CapabilityRestart is a daemon that honours [OpRestart], and CapabilityUpdate
// one that can also install an [OpUpdateNow]. An older daemon ignores the op,
// so the backend refuses to send either unless it is listed here and the
// daemon is at least [MinRemoteControlVersion].
const (
	CapabilityRestart = "restart"
	CapabilityUpdate  = "update"
)

// UpdateAvailable is the daemon telling the panel there is a newer release.
//
// An offer and nothing more. The daemon never updates on its own initiative,
// because approving one means "kill every session on this machine and restart
// them", and only a person can mean that.
type UpdateAvailable struct {
	Version string `json:"version"`
}

// UpdateNow is the approval coming back.
//
// It carries the version that was approved, and the daemon installs that or
// nothing: between the offer and the approval a newer release could appear,
// and somebody who agreed to lose their sessions for 0.7.1 did not agree to
// lose them for whatever landed since.
type UpdateNow struct {
	Version string `json:"version"`
}

// Disk is one filesystem's space.
//
// Per mount, never one number for the machine. A single "free space" figure is
// a lie on any box with more than one filesystem: it can be 2% full and still
// fail to check out a repository, because the full one is the one that matters.
type Disk struct {
	Mount string `json:"mount"`
	Total int64  `json:"total"`
	Free  int64  `json:"free"`
}

// Heartbeat is what a daemon reports about the machine it is on.
//
// Enough to answer "can this box take another agent?" without opening a
// terminal on it, and nothing more: this is a liveness and capacity signal,
// not an inventory.
type Heartbeat struct {
	// MemAvailable is what a new process could actually get, not what is
	// unused. On Linux "free" excludes the page cache and reads alarmingly low
	// on a perfectly healthy machine, which would make every box look full.
	MemTotal     int64 `json:"memTotal"`
	MemAvailable int64 `json:"memAvailable"`

	// CPUPercent is a rate measured over an interval, not a reading taken at
	// an instant — there is no such thing as an instantaneous CPU percentage.
	CPUPercent float64 `json:"cpuPercent"`

	// LoadAvg is Unix-only and simply absent on Windows.
	//
	// Nil rather than zeroes, and the difference matters: three zeroes render
	// as a perfectly idle machine, which is the most misleading thing this
	// payload could say about a box that never reports load at all.
	LoadAvg []float64 `json:"loadAvg,omitempty"`

	UptimeSec int64  `json:"uptimeSec"`
	Disks     []Disk `json:"disks,omitempty"`

	// Sessions is what this daemon is still supervising, so a backend that
	// restarted can correct rows it believes are running.
	Sessions []uint64 `json:"sessions,omitempty"`
}

// Presence names everyone attached to a session.
//
// Two browsers on one terminal is allowed — that is how one person shows
// another what is happening — but it must never be a surprise. Keystrokes
// arriving from nowhere while somebody is typing are indistinguishable from a
// machine that has gone wrong, so the UI names the other viewer instead.
//
// This op only ever travels backend → viewer. The daemon has no interest in
// who is watching, and telling it would be telling a machine something about
// the people using it that it has no need to know.
type Presence struct {
	SessionID uint64 `json:"sessionId"`
	// Viewers are display names, sorted, one per attached browser. The same
	// person in two tabs is two entries, which is the honest answer: it is
	// two terminals that can both type.
	Viewers []string `json:"viewers"`
	// You is the recipient's own index into Viewers, which is why this message
	// is built per viewer rather than broadcast.
	//
	// Without it a browser cannot tell which entry is itself — two tabs
	// belonging to the same person produce two identical names, and a UI that
	// guessed would tell somebody they are sharing a terminal with themselves
	// or, worse, that they are alone when they are not.
	You int `json:"you"`
}

// Exit is the payload of a [TypeExit] frame.
type Exit struct {
	Code int `json:"code"`
}

// ControlFrame builds a control frame, which by definition has session 0.
func ControlFrame(c Control) (Frame, error) {
	if c.Op == "" {
		return Frame{}, fmt.Errorf("wire: control message needs an op")
	}
	b, err := json.Marshal(c)
	if err != nil {
		return Frame{}, fmt.Errorf("wire: marshal control: %w", err)
	}
	return Frame{Type: TypeControl, Payload: b}, nil
}

// ParseControl reads the control message out of a frame.
func ParseControl(f Frame) (Control, error) {
	if f.Type != TypeControl {
		return Control{}, fmt.Errorf("wire: not a control frame: %s", f.Type)
	}
	var c Control
	if err := json.Unmarshal(f.Payload, &c); err != nil {
		return Control{}, fmt.Errorf("wire: parse control: %w", err)
	}
	if c.Op == "" {
		return Control{}, fmt.Errorf("wire: control message has no op")
	}
	return c, nil
}

// SessionFrame builds a frame that belongs to a session, which by definition
// has a non-zero session id.
func SessionFrame(t Type, sessionID uint64, payload []byte) (Frame, error) {
	f := Frame{Type: t, SessionID: sessionID, Payload: payload}
	if err := f.validate(); err != nil {
		return Frame{}, err
	}
	return f, nil
}

// ── Control payloads ────────────────────────────────────────────────────────
//
// These live here, beside the frame format, for the same reason it does: the
// relay and the daemon must agree, and the only way to guarantee that is for
// there to be one definition that both import.

// CoreMCPServerName is what the account-wide server is called in .mcp.json
// and in a permission rule.
//
// Shared rather than agreed twice: the backend decides whether that server is
// configured at all (by sending a [StartSession.CoreMCPURL]) and the daemon
// writes the entry, so the name has to be one constant both import — it is
// also what the setup tab tells people to paste by hand.
const CoreMCPServerName = "agentrq"

// StartSession asks the daemon to run an agent.
//
// It names a KIND and carries validated parameters. There is deliberately no
// argv and no environment: the daemon resolves a kind to a command from its
// own configuration, so a backend that has been taken over cannot ask for a
// shell.
type StartSession struct {
	SessionID uint64 `json:"sessionId"`
	Kind      string `json:"kind"`
	// Dir is the workspace's folder on that machine. The daemon checks it
	// exists and is writable before reporting the session started — the server
	// cannot, because the agent runs somewhere else.
	Dir string `json:"dir"`
	// MCPURL points the agent at its workspace.
	//
	// The credential is a query parameter inside this URL, which is why it
	// travels in a frame rather than an argv and must never be logged: a token
	// on a command line is visible in `ps` to every user on that machine.
	MCPURL string `json:"mcpUrl,omitempty"`
	// ServerName is the entry the daemon writes into .mcp.json, and also what
	// `server:<name>` refers to on the command line. One decision, not two.
	ServerName string `json:"serverName,omitempty"`
	// CoreMCPURL points the agent at the account-wide server as well, under
	// [CoreMCPServerName].
	//
	// Sent only for the "supervisor" workspace, and that decision is the
	// backend's alone: the daemon writes the entry when it is given a URL and
	// nothing else about the workspace tells it to. It carries no credential,
	// because that server authenticates over its own OAuth flow rather than
	// the minted token the per-workspace URL holds — so unlike MCPURL, this
	// one is safe to log.
	CoreMCPURL string `json:"coreMcpUrl,omitempty"`
	// Workspace is what claude-code reports itself as.
	Workspace string `json:"workspace,omitempty"`
	// Model is either kind's model, blank for the agent's own default, and
	// Effort is claude-code's effort level; either for claude-code needs
	// [CapabilityClaudeOptions]. Agent is the acp-gateway's selection.
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
	Agent  string `json:"agent,omitempty"`

	// Fork runs this session in a folder of its own, made from Dir, for a
	// workspace fork. Nil for every other workspace.
	Fork *ForkSpec `json:"fork,omitempty"`

	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

// ForkSpec names a workspace fork's folder: ID (base62) keys it under the
// daemon's own forks directory, From is the parent's folder it is made from.
type ForkSpec struct {
	ID   string `json:"id"`
	From string `json:"from"`
}

// Redacted returns a copy safe to log.
//
// The MCP URL carries the workspace token, and start requests are exactly the
// thing somebody turns logging up on when a launch is not working. Having to
// remember to strip it at each log site is how it eventually reaches a log, so
// the type carries its own redaction.
func (s StartSession) Redacted() StartSession {
	out := s
	if out.MCPURL != "" {
		out.MCPURL = "<redacted>"
	}
	return out
}

// KillSession asks the daemon to end one.
type KillSession struct {
	SessionID uint64 `json:"sessionId"`
}

// RemoveForkDir asks the daemon to delete a merged fork's folder. It names the
// fork and nothing else: the daemon works out the folder itself, so the server
// can never point it at another one.
type RemoveForkDir struct {
	ForkID string `json:"forkId"`
}

// AcpAgent is one entry from `acp-gateway --list-agents --json`: an id the
// launch form's Agent field accepts as-is, a display name, and how it runs on
// this machine (npx, binary, uvx — an agent can publish more than one, and
// this is empty rather than absent for one with no build for this platform).
type AcpAgent struct {
	ID       string   `json:"id"`
	Name     string   `json:"name,omitempty"`
	Runtimes []string `json:"runtimes,omitempty"`
}

// AcpAgentsList answers [OpListAcpAgents].
//
// Agents is empty rather than the request failing outright when the gateway
// could not be asked — no npx on the machine, no network, output this
// daemon's parser does not recognise. This backs an autocomplete with a
// free-text field behind it, and an error would have nothing more useful to
// say than an empty list already does.
type AcpAgentsList struct {
	Agents []AcpAgent `json:"agents,omitempty"`
}

// ListAcpModels asks the gateway what one agent supports.
//
// Dir is the workspace's own working directory on this machine — the same
// one a launch would use. --list-models opens a real agent session, and the
// gateway loads .mcp.json from its working directory before it gets that
// far, refusing without one; Dir's *content* does not matter to this call
// (the listing session carries no MCP servers), only that a real .mcp.json is
// findable there. A workspace nothing has launched from yet has none, which
// is answered the same way any other failure here is: an empty list.
type ListAcpModels struct {
	Agent string `json:"agent"`
	Dir   string `json:"dir,omitempty"`
}

// AcpModel is one entry from `acp-gateway --list-models --agent <a> --json`.
type AcpModel struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Current     bool   `json:"current,omitempty"`
}

// AcpModelsList answers [OpListAcpModels], empty for the same reasons
// [AcpAgentsList] is.
type AcpModelsList struct {
	Agent  string     `json:"agent"`
	Models []AcpModel `json:"models,omitempty"`
}

// SessionState reports a session's lifecycle to the backend.
type SessionState struct {
	SessionID uint64 `json:"sessionId"`
	State     string `json:"state"`
	ExitCode  *int   `json:"exitCode,omitempty"`
	// Error explains a failed start in terms the person can act on — a missing
	// working directory naming the path, rather than "failed to start".
	Error string `json:"error,omitempty"`
	// Restored marks a session re-spawned after an update, so the UI can say
	// why the scrollback is empty.
	Restored bool `json:"restored,omitempty"`
	// Dir is the folder the session runs in, sent with "running". For a fork
	// it is the one the daemon made, which the backend records as the fork's.
	Dir string `json:"dir,omitempty"`
}
