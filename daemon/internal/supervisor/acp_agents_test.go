// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package supervisor

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/agentrq/agentrq/daemon/wire"
)

// fakeNpx puts an executable called "npx" ahead of the real PATH that prints
// stdout verbatim and exits 0 — standing in for the real gateway so the
// decode path can be tested without a network. The real PATH stays behind it
// so the script's own "#!/bin/sh" and "cat" still resolve; only "npx" itself
// is shadowed.
func fakeNpx(t *testing.T, stdout string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fakeNpx writes a POSIX shell script")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\ncat <<'ACPGATEWAYJSON'\n" + stdout + "\nACPGATEWAYJSON\n"
	path := filepath.Join(dir, "npx")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake npx: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// A missing npx must answer with an empty list, never an error or a panic —
// this backs an autocomplete a person can always bypass by typing.
func TestListAcpAgentsFailsOpenWithNoNpx(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // a directory with nothing runnable in it
	if got := ListAcpAgents(context.Background()); got != nil {
		t.Errorf("ListAcpAgents = %v, want nil with no npx on PATH", got)
	}
}

func TestListAcpModelsFailsOpenWithNoNpx(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if got := ListAcpModels(context.Background(), t.TempDir(), "codex-acp"); got != nil {
		t.Errorf("ListAcpModels = %v, want nil with no npx on PATH", got)
	}
}

func TestListAcpAgentsParsesTheGatewaysJSON(t *testing.T) {
	fakeNpx(t, `{"agents":[{"id":"codex-acp","name":"Codex","runtimes":["npx"]},{"id":"kilo","name":"Kilo","runtimes":["npx","binary"]},{"id":"crow-cli","name":"crow-cli","runtimes":["binary"],"unverified":true}]}`)

	got := ListAcpAgents(context.Background())
	want := []wire.AcpAgent{
		{ID: "codex-acp", Name: "Codex", Runtimes: []string{"npx"}},
		{ID: "kilo", Name: "Kilo", Runtimes: []string{"npx", "binary"}},
		{ID: "crow-cli", Name: "crow-cli", Runtimes: []string{"binary"}, Unverified: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListAcpAgents = %+v, want %+v", got, want)
	}
}

// Output this parser does not recognise — a gateway too old to know --json,
// which prints its table instead — is the same empty-list outcome as any
// other failure, not a crash.
func TestListAcpAgentsFailsOpenOnUnparseableOutput(t *testing.T) {
	fakeNpx(t, "  codex-acp   Codex — npx\n")

	if got := ListAcpAgents(context.Background()); got != nil {
		t.Errorf("ListAcpAgents = %v, want nil for output this daemon cannot parse", got)
	}
}

func TestListAcpModelsParsesTheGatewaysJSON(t *testing.T) {
	fakeNpx(t, `{"agent":"codex-acp","models":[{"id":"gpt-5.6-terra","name":"5.6 Terra","description":"Balanced","current":false},{"id":"gpt-5.5","name":"5.5","description":"","current":true}]}`)

	got := ListAcpModels(context.Background(), t.TempDir(), "codex-acp")
	want := []wire.AcpModel{
		{ID: "gpt-5.6-terra", Name: "5.6 Terra", Description: "Balanced", Current: false},
		{ID: "gpt-5.5", Name: "5.5", Current: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListAcpModels = %+v, want %+v", got, want)
	}
}

// agent reaches an argv exactly as the launch path's own --agent does, so a
// value that could be read as a flag is refused before os/exec ever sees it —
// checked here rather than assumed, because this is the one place besides the
// launch path itself that puts a value there.
func TestListAcpModelsRefusesAnUnsafeAgentWithoutRunningAnything(t *testing.T) {
	t.Setenv("PATH", "") // if this ran anything, there would be nothing to run it with
	for _, agent := range []string{"", "-x", "--dangerously-skip-permissions", "a b"} {
		if got := ListAcpModels(context.Background(), t.TempDir(), agent); got != nil {
			t.Errorf("ListAcpModels(%q) = %v, want nil", agent, got)
		}
	}
}
