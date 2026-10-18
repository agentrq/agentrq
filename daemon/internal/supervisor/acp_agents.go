// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package supervisor

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"time"

	"github.com/agentrq/agentrq/daemon/wire"
)

// listAgentsTimeout and listModelsTimeout bound the one-shot npx calls below.
//
// Both can fetch a package over the network on a cold cache — --list-models
// has been seen taking most of a minute the first time an agent is asked
// about — and both back a UI autocomplete that must eventually give up and
// let free text through rather than hold a form open.
const (
	listAgentsTimeout = 30 * time.Second
	listModelsTimeout = 45 * time.Second
)

// jsonAgents and jsonModels are `acp-gateway --json`'s own output shapes for
// --list-agents and --list-models — the gateway's, not this daemon's, so a
// version skew shows up as a JSON field this struct simply leaves at its zero
// value rather than a parse failure.
type jsonAgents struct {
	Agents []wire.AcpAgent `json:"agents"`
}

type jsonModels struct {
	Models []wire.AcpModel `json:"models"`
}

// ListAcpAgents asks the gateway what it can run.
//
// --list-agents needs no workspace — confirmed against the gateway's own
// source, which answers it before ever looking for a .mcp.json — so unlike
// [ListAcpModels] this runs from wherever the daemon happens to be.
//
// A missing npx, no network, a non-zero exit or output this daemon's
// acp-gateway is too old to have produced as JSON are all the same outcome
// here: an empty list, never an error. This backs an autocomplete a person
// can always bypass by typing, and there is nothing an error could tell them
// that dropping into free text does not already say.
func ListAcpAgents(ctx context.Context) []wire.AcpAgent {
	ctx, cancel := context.WithTimeout(ctx, listAgentsTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "npx", "-y", "@agentrq/acp-gateway@latest", "--list-agents", "--json").Output()
	if err != nil {
		return nil
	}
	var parsed jsonAgents
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil
	}
	return parsed.Agents
}

// ListAcpModels asks the gateway what one agent supports.
//
// Unlike --list-agents, --list-models opens a real agent session and so
// needs a workspace: the gateway loads .mcp.json from dir before it ever gets
// to the agent, and refuses without one — confirmed against a real run, which
// otherwise fails with "Could not find .mcp.json" before printing anything
// this could parse. dir is therefore the workspace's own working directory on
// this machine, the same one a launch would use, not a scratch directory —
// its *content* does not matter to this call (the gateway opens the listing
// session with no MCP servers attached), only that a real .mcp.json is
// findable there. A workspace that has never been launched from has none yet,
// which is why this is a lookup behind a free-text field and not a gate: an
// empty dir, or one with no config, means an empty list, exactly like any
// other failure here.
//
// agent reaches an argv exactly as the launch path's own --agent does, so it
// is checked the same way before os/exec sees it — refusing anything that is
// not a plain identifier is what stops a value being read as a flag by the
// process being run.
func ListAcpModels(ctx context.Context, dir, agent string) []wire.AcpModel {
	if err := checkParam("agent", agent); err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, listModelsTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "npx", "-y", "@agentrq/acp-gateway@latest", "--list-models", "--agent", agent, "--json")
	if home, err := os.UserHomeDir(); err == nil {
		dir = expandTilde(dir, home)
	}
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var parsed jsonModels
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil
	}
	return parsed.Models
}
