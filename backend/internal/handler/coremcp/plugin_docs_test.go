// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package coremcp

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The Claude Code plugins shipped from this repository document the tools their
// MCP server offers, in a markdown table per plugin. Those tables are the only
// description an agent reads before deciding what it can do, and they have gone
// wrong in every direction available: a tool that had been removed
// (`getTaskMessages`) was still listed, six that existed were not, and the task
// statuses were copied from a schema that advertised three values the server
// rejects.
//
// So the tables are checked against the servers here. It is the same shape as
// the parity test guarding the frontend's WebMCP catalogue: documentation that
// cannot drift is worth more than documentation that is merely correct today.

// repoRoot walks up from the test's working directory to the directory holding
// the plugins, so the test does not care where `go test` was invoked from.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot read working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".claude-plugin", "marketplace.json")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find the repository root above %s", dir)
		}
		dir = parent
	}
}

var (
	// `mcp.AddTool(s.server, &mcp.Tool{Name: "x"` — the supervisor server.
	//
	// The whitespace after the brace is not cosmetic. The registrations in
	// `server.go` are written on one line and the ones in `events.go` over
	// several, and a pattern that insisted on `{Name:` matched only the first
	// kind — so a whole file of tools was invisible to this test while looking
	// exactly like the tools it did check.
	coreToolRe = regexp.MustCompile(`mcp\.AddTool\(s\.server, &mcp\.Tool\{\s*Name:\s*"([^"]+)"`)
	// `mcp.AddTool(mcpSrv, &mcp.Tool{\n\tName: "x"` — the per-workspace server.
	workspaceToolRe = regexp.MustCompile(`mcp\.AddTool\(mcpSrv, &mcp\.Tool\{\s*Name:\s*"([^"]+)"`)
	// A leading table cell holding a backticked identifier.
	docRowRe = regexp.MustCompile("(?m)^\\| `([a-zA-Z]+)`")
)

func namesIn(t *testing.T, path string, re *regexp.Regexp) []string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}
	var names []string
	for _, m := range re.FindAllStringSubmatch(string(body), -1) {
		names = append(names, m[1])
	}
	if len(names) == 0 {
		t.Fatalf("found no names in %s — has its format changed?", path)
	}
	return names
}

// namesUnder reads every non-test source file in a package rather than one
// named file.
//
// This started as a single path, and the events tools proved that wrong: they
// were registered in `events.go`, the test only ever read `server.go`, and so
// ten tools sat outside the parity check entirely — documenting them would have
// *failed* the build, which is the exact opposite of the pressure this test
// exists to apply. A directory has no such blind spot: a new file of tools is
// covered the moment it is added.
func namesUnder(t *testing.T, dir string, re *regexp.Regexp) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("cannot read %s: %v", dir, err)
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("cannot read %s: %v", name, err)
		}
		for _, m := range re.FindAllStringSubmatch(string(body), -1) {
			names = append(names, m[1])
		}
	}
	if len(names) == 0 {
		t.Fatalf("found no tool names under %s — has the registration format changed?", dir)
	}
	return names
}

func TestPluginDocsListExactlyTheRegisteredTools(t *testing.T) {
	root := repoRoot(t)

	tests := []struct {
		plugin string
		pkg    string
		re     *regexp.Regexp
		docs   []string
	}{
		{
			plugin: "agentrq",
			pkg:    "backend/internal/handler/coremcp",
			re:     coreToolRe,
			docs: []string{
				"plugins/claude/agentrq/README.md",
				"plugins/claude/agentrq/skills/agentrq/SKILL.md",
			},
		},
		{
			plugin: "agentrq-workspace",
			pkg:    "backend/internal/controller/mcp",
			re:     workspaceToolRe,
			docs: []string{
				"plugins/claude/agentrq-workspace/README.md",
				"plugins/claude/agentrq-workspace/skills/agentrq-workspace/SKILL.md",
			},
		},
	}

	for _, tt := range tests {
		registered := namesUnder(t, filepath.Join(root, tt.pkg), tt.re)

		for _, doc := range tt.docs {
			t.Run(tt.plugin+"/"+filepath.Base(filepath.Dir(doc))+"/"+filepath.Base(doc), func(t *testing.T) {
				documented := namesIn(t, filepath.Join(root, doc), docRowRe)

				for _, name := range registered {
					if !slices.Contains(documented, name) {
						t.Errorf("%s registers %q, but %s has no row for it", tt.pkg, name, doc)
					}
				}
				for _, name := range documented {
					if !slices.Contains(registered, name) {
						t.Errorf("%s documents %q, which %s does not register", doc, name, tt.pkg)
					}
				}
			})
		}
	}
}

// The statuses the plugin docs tell agents to use must be ones the server
// accepts. This is what the supervisor skill got wrong in the way that mattered
// most: its human-in-the-loop rule said to signal being stuck by setting the
// status to `waiting`, which fails, so an agent following the instruction could
// not report being blocked at all.
func TestPluginDocsDoNotNameInvalidStatuses(t *testing.T) {
	root := repoRoot(t)

	// Values that were advertised at some point and are not accepted. Written
	// out rather than derived, because the point is to catch these specific
	// ghosts coming back.
	retired := []string{"waiting", "done", "failed"}
	// `deny` was advertised by respondToTask and has never been handled.
	retiredActions := []string{"deny"}
	// A tool that was folded into getTask.
	retiredTools := []string{"getTaskMessages", "getNextTask"}

	docs := []string{
		"plugins/claude/agentrq/README.md",
		"plugins/claude/agentrq/skills/agentrq/SKILL.md",
		"plugins/claude/agentrq-workspace/README.md",
		"plugins/claude/agentrq-workspace/skills/agentrq-workspace/SKILL.md",
	}

	for _, doc := range docs {
		t.Run(doc, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, doc))
			if err != nil {
				t.Fatalf("cannot read %s: %v", doc, err)
			}
			text := string(body)

			for _, bad := range slices.Concat(retired, retiredActions) {
				// Backticked, so prose like "the task is done" does not trip it.
				if regexp.MustCompile("`" + bad + "`").MatchString(text) {
					t.Errorf("%s offers `%s`, which the server rejects", doc, bad)
				}
			}
			for _, bad := range retiredTools {
				if regexp.MustCompile(`\b` + bad + `\b`).MatchString(text) {
					t.Errorf("%s mentions %s, which no longer exists", doc, bad)
				}
			}
		})
	}
}
