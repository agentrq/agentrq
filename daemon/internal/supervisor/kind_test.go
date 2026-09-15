// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package supervisor

import (
	"errors"
	"strings"
	"testing"
)

// The command lines mirror the repository's own make targets, which is what
// keeps the first version honest: the daemon automates exactly what a person
// does by hand today.
func TestResolveMatchesTheMakeTargets(t *testing.T) {
	c, err := Resolve(KindClaudeCode, Params{Workspace: "agentrq-code", ServerName: "agentrq-workspace"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := "claude --name agentrq-code server:agentrq-workspace"
	if got := strings.Join(c.Argv, " "); got != want {
		t.Errorf("argv = %q, want %q", got, want)
	}
	if !c.NeedsMCPConfig {
		t.Error("claude-code reads .mcp.json and must be marked as needing it")
	}

	g, err := Resolve(KindACPGateway, Params{Model: "gemini-3.8-flash-high", Agent: "antigravity-acp"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want = "npx @agentrq/acp-gateway@latest --model gemini-3.8-flash-high --agent antigravity-acp"
	if got := strings.Join(g.Argv, " "); got != want {
		t.Errorf("argv = %q, want %q", got, want)
	}
	if g.NeedsMCPConfig {
		t.Error("acp-gateway does not read .mcp.json")
	}
}

// A flag with "dangerously" in its name should be switched on by somebody who
// meant it, not inherited from a developer's make target.
func TestDevChannelsFlagIsOffUnlessAskedFor(t *testing.T) {
	off, err := Resolve(KindClaudeCode, Params{Workspace: "w", ServerName: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(off.Argv, " "), "dangerously") {
		t.Error("the development-channels flag was passed without being asked for")
	}

	on, err := Resolve(KindClaudeCode, Params{Workspace: "w", ServerName: "s", DevChannels: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(on.Argv, " "), "--dangerously-load-development-channels") {
		t.Error("the flag was asked for and not passed")
	}
}

func TestResolveRefusesAnUnknownKind(t *testing.T) {
	for _, k := range []Kind{"", "bash", "sh", "claude", "acp"} {
		if _, err := Resolve(k, Params{}); !errors.Is(err, ErrUnknownKind) {
			t.Errorf("Resolve(%q) error = %v, want ErrUnknownKind", k, err)
		}
	}
}

func TestResolveRequiresItsParameters(t *testing.T) {
	tests := []struct {
		name string
		kind Kind
		p    Params
	}{
		{"claude without workspace", KindClaudeCode, Params{ServerName: "s"}},
		{"claude without server name", KindClaudeCode, Params{Workspace: "w"}},
		{"gateway without model", KindACPGateway, Params{Agent: "a"}},
		{"gateway without agent", KindACPGateway, Params{Model: "m"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Resolve(tc.kind, tc.p); !errors.Is(err, ErrMissingParam) {
				t.Errorf("error = %v, want ErrMissingParam", err)
			}
		})
	}
}

// The values reach an argv. Go's exec does not go through a shell, so there is
// no quoting to get wrong — but a value starting with a dash would be read as
// a flag by the program being run, which is its own way of turning a parameter
// into an instruction.
func TestParametersThatCouldBecomeFlagsOrWorseAreRefused(t *testing.T) {
	bad := []string{
		"--dangerously-skip-permissions", "-x", // would be read as flags
		"a b", "a;b", "a|b", "a&b", "$(id)", "`id`", // shell-shaped, refused anyway
		"a\nb", "a\x00b", // control characters
		"../escape", "a/b", // path-shaped
		strings.Repeat("a", 200), // absurd length
		"",
	}
	for _, v := range bad {
		if _, err := Resolve(KindClaudeCode, Params{Workspace: v, ServerName: "s"}); err == nil {
			t.Errorf("Resolve accepted workspace=%q", v)
		}
		if _, err := Resolve(KindACPGateway, Params{Model: v, Agent: "a"}); err == nil {
			t.Errorf("Resolve accepted model=%q", v)
		}
	}
}

func TestReasonableParametersAreAccepted(t *testing.T) {
	good := []string{"agentrq-code", "gemini-3.8-flash-high", "antigravity-acp", "a", "A1", "x.y_z-1"}
	for _, v := range good {
		if _, err := Resolve(KindACPGateway, Params{Model: v, Agent: v}); err != nil {
			t.Errorf("Resolve refused a reasonable value %q: %v", v, err)
		}
	}
}

// The refusal has to name the field, or somebody reads "parameter is not
// acceptable" and starts guessing which one.
func TestRefusalNamesTheParameter(t *testing.T) {
	_, err := Resolve(KindACPGateway, Params{Model: "a b", Agent: "ok"})
	if err == nil || !strings.Contains(err.Error(), "model") {
		t.Errorf("error = %v, want it to name the model parameter", err)
	}
}

func TestKindsListsWhatWillRun(t *testing.T) {
	ks := Kinds()
	if len(ks) != 2 {
		t.Fatalf("Kinds() = %v, want exactly the two the daemon runs", ks)
	}
}
