// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	mcpevent "github.com/agentrq/agentrq/backend/internal/controller/mcp"
)

// The example extensions declare the MCP tools they intend to call, and the
// install screen shows that list to the user as what the extension may do. A
// manifest can name anything; only the server says what exists.
//
// The `standup` example named `listTasks` on the workspace server, which has
// never had one — an agent connected to a workspace can act on the task it was
// given and read what the workspace remembers, but not enumerate the board.
// Nothing caught it: the example's own tests stubbed the call and passed, and
// `checkCompatibility` would only have refused the install once a real tool list
// was in front of it, which happens on somebody's machine rather than in CI.
//
// So the manifests are checked against the servers here, the same way the plugin
// documentation is in plugin_docs_test.go. These examples are the tutorial —
// somebody writing their fourth extension copies one — and an example asking for
// a tool that does not exist teaches an extension that cannot be installed.

// A tool, or one action of a tool that takes them: `task.create`.
var manifestToolRe = regexp.MustCompile(`^[a-zA-Z]+(\.[a-zA-Z]+)?$`)

// actionsOf reads the actions an action tool offers from its params' enum.
func actionsOf(t *testing.T, params any) []string {
	t.Helper()
	f, ok := reflect.TypeOf(params).FieldByName("Action")
	enum, isEnum := strings.CutPrefix(f.Tag.Get("jsonschema"), "enum: ")
	if !ok || !isEnum {
		t.Fatalf("%T has no Action enum", params)
	}
	return strings.Split(enum, ", ")
}

type exampleManifest struct {
	Name string `json:"name"`
	MCP  struct {
		Workspace  []string `json:"workspace"`
		Supervisor []string `json:"supervisor"`
	} `json:"mcp"`
}

func TestExampleExtensionsAskForToolsThatExist(t *testing.T) {
	root := repoRoot(t)

	workspaceTools := namesUnder(t, filepath.Join(root, "backend/internal/controller/mcp"), workspaceToolRe)
	supervisorTools := namesUnder(t, filepath.Join(root, "backend/internal/handler/coremcp"), coreToolRe)
	workspaceActions := map[string][]string{"task": actionsOf(t, mcpevent.TaskParams{}), "pano": actionsOf(t, mcpevent.PanoParams{})}
	supervisorActions := map[string][]string{"task": actionsOf(t, TaskParams{}), "pano": actionsOf(t, mcpevent.PanoParams{})}

	dir := filepath.Join(root, "examples/extensions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("cannot read %s: %v", dir, err)
	}

	found := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name(), "agentrq-extension.json")
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("cannot read %s: %v", path, err)
		}

		var manifest exampleManifest
		if err := json.Unmarshal(body, &manifest); err != nil {
			t.Fatalf("%s is not valid JSON: %v", path, err)
		}
		found++

		t.Run(entry.Name(), func(t *testing.T) {
			if manifest.Name != entry.Name() {
				// The name is an address — it appears in routes, registry keys
				// and the install directory — so a folder disagreeing with it
				// means half the wiring points somewhere else.
				t.Errorf("the folder is %q but the manifest calls itself %q", entry.Name(), manifest.Name)
			}

			check := func(asked, offered []string, actions map[string][]string, surface string) {
				for _, entry := range asked {
					if !manifestToolRe.MatchString(entry) {
						t.Errorf("%q is not a tool name", entry)
						continue
					}
					tool, action, one := strings.Cut(entry, ".")
					if !slices.Contains(offered, tool) {
						t.Errorf("asks for %q, which the %s server does not offer", tool, surface)
					} else if one && !slices.Contains(actions[tool], action) {
						t.Errorf("asks for %q, but the %s server's %s has no action %q", entry, surface, tool, action)
					}
				}
			}
			check(manifest.MCP.Workspace, workspaceTools, workspaceActions, "workspace")
			check(manifest.MCP.Supervisor, supervisorTools, supervisorActions, "supervisor")
		})
	}

	if found == 0 {
		t.Fatalf("found no example extensions under %s — has the layout changed?", dir)
	}
}

// The specific ghost, named so it cannot come back quietly.
//
// `listTasks` exists on the supervisor server and not on the per-workspace one,
// which makes it exactly the kind of tool somebody reaches for by memory. The
// asymmetry is deliberate: the supervisor is the account, and a workspace agent
// is not.
func TestWorkspaceServerStillOffersNoTaskListing(t *testing.T) {
	root := repoRoot(t)
	workspace := namesUnder(t, filepath.Join(root, "backend/internal/controller/mcp"), workspaceToolRe)

	for _, tool := range []string{"listTasks", "listAllTasks"} {
		if slices.Contains(workspace, tool) {
			t.Errorf(
				"the workspace server now offers %q. That was deliberately not there: an agent connected to a "+
					"workspace acts on the task it was given rather than enumerating the board. If this is a "+
					"considered change, delete this test and say why in the commit.",
				tool,
			)
		}
	}
}
