package coremcp

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
	coreToolRe = regexp.MustCompile(`mcp\.AddTool\(s\.server, &mcp\.Tool\{Name:\s*"([^"]+)"`)
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

func TestPluginDocsListExactlyTheRegisteredTools(t *testing.T) {
	root := repoRoot(t)

	tests := []struct {
		plugin string
		server string
		re     *regexp.Regexp
		docs   []string
	}{
		{
			plugin: "agentrq",
			server: "backend/internal/handler/coremcp/server.go",
			re:     coreToolRe,
			docs: []string{
				"plugins/claude/agentrq/README.md",
				"plugins/claude/agentrq/skills/agentrq/SKILL.md",
			},
		},
		{
			plugin: "agentrq-workspace",
			server: "backend/internal/controller/mcp/server.go",
			re:     workspaceToolRe,
			docs: []string{
				"plugins/claude/agentrq-workspace/README.md",
				"plugins/claude/agentrq-workspace/skills/agentrq-workspace/SKILL.md",
			},
		},
	}

	for _, tt := range tests {
		registered := namesIn(t, filepath.Join(root, tt.server), tt.re)

		for _, doc := range tt.docs {
			t.Run(tt.plugin+"/"+filepath.Base(filepath.Dir(doc))+"/"+filepath.Base(doc), func(t *testing.T) {
				documented := namesIn(t, filepath.Join(root, doc), docRowRe)

				for _, name := range registered {
					if !slices.Contains(documented, name) {
						t.Errorf("%s registers %q, but %s has no row for it", tt.server, name, doc)
					}
				}
				for _, name := range documented {
					if !slices.Contains(registered, name) {
						t.Errorf("%s documents %q, which %s does not register", doc, name, tt.server)
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
