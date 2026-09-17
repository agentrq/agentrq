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
	want := "claude --name agentrq-code --dangerously-load-development-channels server:agentrq-workspace"
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
	// Both kinds read it. `make remote-agy` works only because it is run from
	// the repository root, which has a .mcp.json — which is exactly why this
	// was easy to get wrong.
	if !g.NeedsMCPConfig {
		t.Error("acp-gateway does not ask for .mcp.json, and dies without one")
	}
}

// Confirmed by the owner: the daemon always passes it. The flag is what loads
// the `server:<name>` channel, which is how the agent receives tasks — without
// it the process starts and then does nothing, which is a worse failure than
// it sounds because it looks like success.
func TestDevelopmentChannelsFlagIsAlwaysPassedToClaude(t *testing.T) {
	c, err := Resolve(KindClaudeCode, Params{Workspace: "w", ServerName: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(c.Argv, " "), "--dangerously-load-development-channels") {
		t.Error("the flag is required for the channel to load, and was not passed")
	}

	// And only to claude-code: the gateway has no such flag, and passing an
	// unknown one to it would be a startup failure.
	g, err := Resolve(KindACPGateway, Params{Model: "m", Agent: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(g.Argv, " "), "dangerously") {
		t.Error("the flag leaked into the acp-gateway command")
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
func TestIdentifiersThatCouldBecomeFlagsOrWorseAreRefused(t *testing.T) {
	bad := []string{
		"--dangerously-skip-permissions", "-x", // would be read as flags
		"a b", "a;b", "a|b", "a&b", "$(id)", "`id`", // shell-shaped, refused anyway
		"a\nb", "a\x00b", // control characters
		"../escape", "a/b", // path-shaped
		strings.Repeat("a", 200), // absurd length
		"",
	}
	for _, v := range bad {
		if _, err := Resolve(KindACPGateway, Params{Model: v, Agent: "a"}); err == nil {
			t.Errorf("Resolve accepted model=%q", v)
		}
	}
}

// A workspace name is chosen by a person, so the identifier rule is the wrong
// rule for it — the first launch against a real workspace failed on the space
// in "Terminal probe". What is still refused is what could change the meaning
// of the command rather than what is merely unusual.
func TestANameIsAllowedToLookLikeAName(t *testing.T) {
	good := []string{
		"Terminal probe", "Q3 migration", "AgentRQ — daemon", "客户端", "a b c",
		"it's mine", "release/2.0", "10% faster",
	}
	for _, v := range good {
		if _, err := Resolve(KindClaudeCode, Params{Workspace: v, ServerName: "s"}); err != nil {
			t.Errorf("Resolve refused the workspace name %q: %v", v, err)
		}
	}

	bad := map[string]string{
		"a leading dash arrives as a flag":   "--dangerously-skip-permissions",
		"a leading dash, however short":      "-x",
		"a newline can forge a log line":     "a\nb",
		"a NUL truncates":                    "a\x00b",
		"an escape can move a real terminal": "a\x1b[2Jb",
		"an absurd length":                   strings.Repeat("a", 200),
		"nothing at all":                     "",
	}
	for why, v := range bad {
		if _, err := Resolve(KindClaudeCode, Params{Workspace: v, ServerName: "s"}); err == nil {
			t.Errorf("Resolve accepted %q — %s", v, why)
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
