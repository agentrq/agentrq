// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

// Package supervisor starts and stops agent processes.
//
// The daemon runs exactly two kinds of process, and this package is where a
// kind becomes a command line. That translation lives here, on the machine,
// rather than coming from the backend — the start request names a *kind* and
// carries validated parameters, never an argv. A backend that has been taken
// over therefore cannot ask for /bin/sh.
//
// Be clear-eyed about the size of that boundary. It genuinely stops "the
// server said run X". It does nothing about typing `sh` into a running
// claude-code session, which can run commands by design. Both halves are true
// and the second is not a reason to skip the first.
package supervisor

import (
	"errors"
	"fmt"
	"regexp"
)

// Kind is an agent this daemon knows how to run.
type Kind string

const (
	// KindClaudeCode mirrors the repository's `remote-claude` make target.
	KindClaudeCode Kind = "claude-code"
	// KindACPGateway mirrors `remote-agy`.
	KindACPGateway Kind = "acp-gateway"
)

// Errors from turning a request into a command.
var (
	ErrUnknownKind  = errors.New("supervisor: unknown agent kind")
	ErrBadParameter = errors.New("supervisor: parameter is not acceptable")
	ErrMissingParam = errors.New("supervisor: required parameter missing")
)

// Params are the values the backend may influence, and the only ones.
type Params struct {
	// Workspace is the workspace name claude-code reports itself as.
	Workspace string
	// ServerName is the MCP server's name in .mcp.json, which is also what
	// `server:<name>` refers to on the command line. One decision, not two.
	ServerName string
	// Model and Agent are the acp-gateway's selections.
	Model string
	Agent string
}

// safeParam is what a parameter may contain.
//
// Deliberately narrow, and checked rather than escaped. These values are
// destined for an argv, and while Go's exec does not go through a shell — so
// there is no quoting to get wrong — a parameter carrying a leading dash would
// be read as a flag by the program being run, which is its own way of turning
// a value into an instruction.
var safeParam = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// checkParam validates one value, naming the field so a refusal is actionable.
func checkParam(field, value string) error {
	if value == "" {
		return fmt.Errorf("%w: %s", ErrMissingParam, field)
	}
	if !safeParam.MatchString(value) {
		return fmt.Errorf("%w: %s=%q", ErrBadParameter, field, value)
	}
	return nil
}

// Command is the process to run.
type Command struct {
	Argv []string
	// NeedsMCPConfig reports whether this kind reads .mcp.json from its
	// working directory, and therefore whether the daemon has to write one
	// before starting it.
	NeedsMCPConfig bool
}

// Resolve turns a kind and its parameters into a command line.
func Resolve(kind Kind, p Params) (Command, error) {
	switch kind {
	case KindClaudeCode:
		if err := checkParam("workspace", p.Workspace); err != nil {
			return Command{}, err
		}
		if err := checkParam("serverName", p.ServerName); err != nil {
			return Command{}, err
		}
		// --dangerously-load-development-channels is always passed, confirmed
		// by the owner on 2026-09-15 when I asked whether a daemon should.
		//
		// It is what loads the `server:<name>` channel, which is the mechanism
		// by which the agent receives tasks at all — so without it this kind
		// starts and then does nothing, which is a worse failure than it
		// sounds because it looks like success.
		//
		// Recording that it was asked and answered rather than leaving it to
		// be rediscovered: a flag named "dangerously" appearing unconditionally
		// in a daemon should make a reader stop, and this comment is the answer
		// to why it is there.
		//
		// `server:<name>` names the MCP server in .mcp.json, which is why the
		// daemon writes that file and passes this argument as one step.
		return Command{
			Argv: []string{
				"claude",
				"--name", p.Workspace,
				"--dangerously-load-development-channels",
				"server:" + p.ServerName,
			},
			NeedsMCPConfig: true,
		}, nil

	case KindACPGateway:
		if err := checkParam("model", p.Model); err != nil {
			return Command{}, err
		}
		if err := checkParam("agent", p.Agent); err != nil {
			return Command{}, err
		}
		return Command{Argv: []string{
			"npx", "@agentrq/acp-gateway@latest",
			"--model", p.Model,
			"--agent", p.Agent,
		}}, nil

	default:
		return Command{}, fmt.Errorf("%w: %q", ErrUnknownKind, kind)
	}
}

// Kinds lists what this daemon will run, for `agentrqd status` and for
// answering the backend when it asks.
func Kinds() []Kind { return []Kind{KindClaudeCode, KindACPGateway} }
