// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"encoding/json"
	"errors"
)

// ProtocolVersion is the only ACP version this package speaks.
const ProtocolVersion = 1

// Methods AgentRQ sends to the agent.
const (
	methodInitialize       = "initialize"
	methodAuthenticate     = "authenticate"
	methodSessionNew       = "session/new"
	methodSessionPrompt    = "session/prompt"
	methodSessionCancel    = "session/cancel"
	methodSessionClose     = "session/close"
	methodSessionSetMode   = "session/set_mode"
	methodSessionSetConfig = "session/set_config_option"
	methodCancelRequest    = "$/cancel_request"
)

// Methods the agent sends to AgentRQ.
const (
	methodSessionUpdate       = "session/update"
	methodRequestPermission   = "session/request_permission"
	methodElicitationCreate   = "elicitation/create"
	methodElicitationComplete = "elicitation/complete"
)

// JSON-RPC and ACP error codes.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
	codeCancelled      = -32800
	codeAuthRequired   = -32000
	codeNotFound       = -32002
)

type implementation struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
}

type initializeRequest struct {
	ProtocolVersion    int                `json:"protocolVersion"`
	ClientCapabilities clientCapabilities `json:"clientCapabilities"`
	ClientInfo         implementation     `json:"clientInfo"`
}

type clientCapabilities struct {
	FS          fsCapabilities          `json:"fs"`
	Terminal    bool                    `json:"terminal"`
	Elicitation elicitationCapabilities `json:"elicitation"`
	Plan        struct{}                `json:"plan"`
	Auth        clientAuthCapabilities  `json:"auth"`
}

type fsCapabilities struct {
	ReadTextFile  bool `json:"readTextFile"`
	WriteTextFile bool `json:"writeTextFile"`
}

type elicitationCapabilities struct {
	Form struct{} `json:"form"`
	URL  struct{} `json:"url"`
}

type clientAuthCapabilities struct {
	Terminal bool `json:"terminal"`
}

// clientCaps is what AgentRQ declares: no file system and no terminal, because
// the server cannot reach the user's machine and the agent's own tools do that
// work; no terminal logins, because nobody can type into one remotely;
// questions in both forms; plans.
var clientCaps = clientCapabilities{}

// initializeResponse is decoded leniently: only protocolVersion is required,
// and a garbled optional field reads as absent (the schema marks them
// x-deserialize-default-on-error) instead of failing the connection.
type initializeResponse struct {
	ProtocolVersion   int
	AgentCapabilities agentCapabilities
	AuthMethods       []authMethod
	AgentInfo         *implementation
}

var errNoProtocolVersion = errors.New("initialize answer has no protocolVersion")

func (r *initializeResponse) decode(raw []byte) error {
	var wire struct {
		ProtocolVersion   *int            `json:"protocolVersion"`
		AgentCapabilities json.RawMessage `json:"agentCapabilities"`
		AuthMethods       json.RawMessage `json:"authMethods"`
		AgentInfo         json.RawMessage `json:"agentInfo"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return err
	}
	if wire.ProtocolVersion == nil {
		return errNoProtocolVersion
	}
	r.ProtocolVersion = *wire.ProtocolVersion
	lenient(wire.AgentCapabilities, &r.AgentCapabilities)
	lenient(wire.AuthMethods, &r.AuthMethods)
	var info implementation
	if lenient(wire.AgentInfo, &info) && info.Name != "" {
		r.AgentInfo = &info
	}
	return nil
}

// lenient decodes an optional field, leaving dst untouched when the field is
// absent, null or not the shape it should be. It reports whether it decoded.
func lenient(raw json.RawMessage, dst any) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	return json.Unmarshal(raw, dst) == nil
}

type agentCapabilities struct {
	LoadSession         bool                `json:"loadSession"`
	PromptCapabilities  promptCapabilities  `json:"promptCapabilities"`
	MCPCapabilities     mcpCapabilities     `json:"mcpCapabilities"`
	SessionCapabilities sessionCapabilities `json:"sessionCapabilities"`
}

type promptCapabilities struct {
	Image           bool `json:"image"`
	Audio           bool `json:"audio"`
	EmbeddedContext bool `json:"embeddedContext"`
}

// mcpCapabilities keeps "said no" apart from "said nothing": an agent that
// advertises no MCP capabilities is far more likely to be terse than unable,
// and dropping the workspace's own server on that reading would cost it its
// tools.
type mcpCapabilities struct {
	HTTP *bool `json:"http"`
	SSE  *bool `json:"sse"`
	ACP  *bool `json:"acp"`
}

// sessionCapabilities: an optional capability is supported when the agent
// supplies an object for it ({} means yes).
type sessionCapabilities struct {
	Close  json.RawMessage `json:"close"`
	Resume json.RawMessage `json:"resume"`
	List   json.RawMessage `json:"list"`
}

func supported(raw json.RawMessage) bool {
	s := string(raw)
	return s != "" && s != "null" && s != "false"
}

type authMethod struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Type is "terminal" for a login run in a terminal, absent for one the
	// agent performs itself.
	Type string `json:"type,omitempty"`
}

type httpHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type mcpServerHTTP struct {
	Type    string       `json:"type"`
	Name    string       `json:"name"`
	URL     string       `json:"url"`
	Headers []httpHeader `json:"headers"`
}

type newSessionRequest struct {
	Cwd        string          `json:"cwd"`
	MCPServers []mcpServerHTTP `json:"mcpServers"`
}

// MarshalJSON writes mcpServers as [] when there are none: the field is
// required, and null is not a list.
func (r newSessionRequest) MarshalJSON() ([]byte, error) {
	type plain newSessionRequest
	if r.MCPServers == nil {
		r.MCPServers = []mcpServerHTTP{}
	}
	return json.Marshal(plain(r))
}

type newSessionResponse struct {
	SessionID     string            `json:"sessionId"`
	Modes         *sessionModeState `json:"modes"`
	ConfigOptions []configOption    `json:"configOptions"`
}

type sessionModeState struct {
	CurrentModeID  string        `json:"currentModeId"`
	AvailableModes []sessionMode `json:"availableModes"`
}

type sessionMode struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Meta        json.RawMessage `json:"_meta,omitempty"`
}

// configOption is a session setting the agent offers, such as its model. The
// options stay raw: a select lists options or groups of options.
type configOption struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	Category     string          `json:"category,omitempty"`
	Type         string          `json:"type"`
	CurrentValue json.RawMessage `json:"currentValue"`
	Options      json.RawMessage `json:"options"`
}

type selectOption struct {
	Value       json.RawMessage `json:"value"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Group       string          `json:"-"`
}

// selectOptions flattens a select's options, keeping each one's group name.
func (o configOption) selectOptions() []selectOption {
	var items []struct {
		Value       json.RawMessage `json:"value"`
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Group       string          `json:"group"`
		Options     []selectOption  `json:"options"`
	}
	if !lenient(o.Options, &items) {
		return nil
	}
	var out []selectOption
	for _, it := range items {
		if it.Options != nil {
			for _, opt := range it.Options {
				opt.Group = it.Name
				out = append(out, opt)
			}
			continue
		}
		if len(it.Value) > 0 {
			out = append(out, selectOption{Value: it.Value, Name: it.Name, Description: it.Description})
		}
	}
	return out
}

type setModeRequest struct {
	SessionID string `json:"sessionId"`
	ModeID    string `json:"modeId"`
}

type setConfigOptionRequest struct {
	SessionID string `json:"sessionId"`
	ConfigID  string `json:"configId"`
	Value     string `json:"value"`
}

type setConfigOptionResponse struct {
	ConfigOptions []configOption `json:"configOptions"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type promptRequest struct {
	SessionID string         `json:"sessionId"`
	Prompt    []contentBlock `json:"prompt"`
}

type promptResponse struct {
	StopReason string `json:"stopReason"`
}

type sessionIDParams struct {
	SessionID string `json:"sessionId"`
}

type authenticateRequest struct {
	MethodID string `json:"methodId"`
}

type sessionNotification struct {
	SessionID string          `json:"sessionId"`
	Update    json.RawMessage `json:"update"`
}

// kindOf reads an update's sessionUpdate discriminator.
func kindOf(update json.RawMessage) string {
	var k struct {
		SessionUpdate string `json:"sessionUpdate"`
	}
	_ = json.Unmarshal(update, &k)
	return k.SessionUpdate
}

type contentChunk struct {
	Content contentBlock `json:"content"`
}

type toolCallUpdate struct {
	ToolCallID string          `json:"toolCallId"`
	Title      *string         `json:"title"`
	Name       *string         `json:"name"`
	Kind       *string         `json:"kind"`
	Status     *string         `json:"status"`
	RawInput   json.RawMessage `json:"rawInput"`
}

type planEntry struct {
	Content  string `json:"content"`
	Priority string `json:"priority"`
	Status   string `json:"status"`
}

type legacyPlan struct {
	Entries []planEntry `json:"entries"`
}

type planUpdate struct {
	Plan planContent `json:"plan"`
}

type planContent struct {
	Type    string      `json:"type"` // items | file | markdown
	PlanID  string      `json:"planId"`
	Entries []planEntry `json:"entries"`
	URI     string      `json:"uri"`
	Content string      `json:"content"`
}

type planRemoved struct {
	PlanID string `json:"planId"`
}

type availableCommand struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Input       *struct {
		Hint string `json:"hint"`
	} `json:"input"`
}

type commandsUpdate struct {
	AvailableCommands []availableCommand `json:"availableCommands"`
}

type currentModeUpdate struct {
	CurrentModeID string `json:"currentModeId"`
}

type configOptionUpdate struct {
	ConfigOptions []configOption `json:"configOptions"`
}

type usageUpdate struct {
	Used int64 `json:"used"`
	Size int64 `json:"size"`
	Cost *cost `json:"cost"`
}

type cost struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

type permissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

type requestPermissionRequest struct {
	SessionID string             `json:"sessionId"`
	ToolCall  toolCallUpdate     `json:"toolCall"`
	Options   []permissionOption `json:"options"`
}

type permissionOutcome struct {
	Outcome  string `json:"outcome"` // selected | cancelled
	OptionID string `json:"optionId,omitempty"`
}

type requestPermissionResponse struct {
	Outcome permissionOutcome `json:"outcome"`
}

type createElicitationRequest struct {
	Message         string          `json:"message"`
	Mode            string          `json:"mode"`
	RequestedSchema map[string]any  `json:"requestedSchema,omitempty"`
	ElicitationID   string          `json:"elicitationId,omitempty"`
	URL             string          `json:"url,omitempty"`
	SessionID       string          `json:"sessionId,omitempty"`
	ToolCallID      string          `json:"toolCallId,omitempty"`
	RequestID       json.RawMessage `json:"requestId,omitempty"`
}

type createElicitationResponse struct {
	Action  string         `json:"action"`
	Content map[string]any `json:"content,omitempty"`
}

type completeElicitationNotification struct {
	ElicitationID string `json:"elicitationId"`
}

type cancelRequestNotification struct {
	RequestID json.RawMessage `json:"requestId"`
}
