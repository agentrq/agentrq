// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
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
				"plugins/claude/agentrq-custom-supervisor/README.md",
				"plugins/claude/agentrq-custom-supervisor/skills/agentrq/SKILL.md",
				"plugins/cursor/agentrq/README.md",
				"plugins/cursor/agentrq/skills/agentrq/SKILL.md",
			},
		},
		{
			// The Gemini CLI extension talks to the same server. Its GEMINI.md is
			// loaded into every session, so it names tools in prose rather than
			// repeating the table; only the README carries one.
			plugin: "gemini",
			pkg:    "backend/internal/handler/coremcp",
			re:     coreToolRe,
			docs:   []string{"plugins/gemini/README.md"},
		},
		{
			plugin: "agentrq-workspace",
			pkg:    "backend/internal/controller/mcp",
			re:     workspaceToolRe,
			docs: []string{
				"plugins/claude/agentrq-workspace/README.md",
				"plugins/claude/agentrq-workspace/skills/agentrq-workspace/SKILL.md",
				"plugins/cursor/agentrq-workspace/README.md",
				"plugins/cursor/agentrq-workspace/skills/agentrq-workspace/SKILL.md",
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
		"plugins/claude/agentrq-custom-supervisor/README.md",
		"plugins/claude/agentrq-custom-supervisor/skills/agentrq/SKILL.md",
		"plugins/cursor/agentrq/README.md",
		"plugins/cursor/agentrq/skills/agentrq/SKILL.md",
		"plugins/cursor/agentrq-workspace/README.md",
		"plugins/cursor/agentrq-workspace/skills/agentrq-workspace/SKILL.md",
		"plugins/gemini/README.md",
		"plugins/gemini/GEMINI.md",
	}
	// The supervisor skill's reference files; its other copies are held
	// identical to this one.
	references, err := filepath.Glob(filepath.Join(root, "plugins/claude/agentrq/skills/agentrq/references/*.md"))
	if err != nil || len(references) == 0 {
		t.Fatalf("found no reference files for the agentrq skill: %v", err)
	}
	for _, ref := range references {
		rel, _ := filepath.Rel(root, ref)
		docs = append(docs, rel)
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

func TestClaudePluginManifestStrictRequirements(t *testing.T) {
	root := repoRoot(t)
	pngSig := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

	plugins := []string{
		"plugins/claude/agentrq",
		"plugins/claude/agentrq-workspace",
		"plugins/claude/agentrq-custom-supervisor",
	}

	for _, rel := range plugins {
		t.Run(rel, func(t *testing.T) {
			pluginDir := filepath.Join(root, rel)
			manifestPath := filepath.Join(pluginDir, ".claude-plugin", "plugin.json")
			raw, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatalf("cannot read %s: %v", manifestPath, err)
			}

			var manifest struct {
				Name             string `json:"name"`
				Version          string `json:"version"`
				Icon             string `json:"icon"`
				PrivacyPolicyURL string `json:"privacyPolicyUrl"`
			}
			if err := json.Unmarshal(raw, &manifest); err != nil {
				t.Fatalf("invalid JSON in %s: %v", manifestPath, err)
			}

			if manifest.PrivacyPolicyURL != "https://agentrq.com/privacy" {
				t.Errorf("%s privacyPolicyUrl = %q, want %q", manifestPath, manifest.PrivacyPolicyURL, "https://agentrq.com/privacy")
			}

			readmePath := filepath.Join(pluginDir, "README.md")
			readmeBytes, err := os.ReadFile(readmePath)
			if err != nil {
				t.Fatalf("cannot read %s: %v", readmePath, err)
			}
			if !strings.Contains(string(readmeBytes), "[Privacy Policy](https://agentrq.com/privacy)") {
				t.Errorf("%s does not contain [Privacy Policy](https://agentrq.com/privacy)", readmePath)
			}

			if manifest.Icon != "./.claude-plugin/icon.png" {
				t.Errorf("%s icon = %q, want %q", manifestPath, manifest.Icon, "./.claude-plugin/icon.png")
			}

			iconPath := filepath.Join(pluginDir, ".claude-plugin", "icon.png")
			iconBytes, err := os.ReadFile(iconPath)
			if err != nil {
				t.Fatalf("cannot read %s: %v", iconPath, err)
			}
			if len(iconBytes) >= 2*1024*1024 {
				t.Errorf("%s size = %d bytes, must be under 2 MB", iconPath, len(iconBytes))
			}
			if len(iconBytes) < 24 || !bytes.Equal(iconBytes[:8], pngSig) || string(iconBytes[12:16]) != "IHDR" {
				t.Fatalf("%s is not a valid PNG", iconPath)
			}
			width := binary.BigEndian.Uint32(iconBytes[16:20])
			height := binary.BigEndian.Uint32(iconBytes[20:24])
			if width != height || width < 512 || width > 2048 {
				t.Errorf("%s dimensions = %dx%d, want square 512..2048 px", iconPath, width, height)
			}
		})
	}
}

// agentrq-custom-supervisor is agentrq pointed at a self-hosted server, and
// this server serves the same skill over MCP, so neither copy may drift from
// agentrq's.
func TestPluginDocsCustomSupervisorSkillMatchesTheHostedOne(t *testing.T) {
	root := repoRoot(t)
	hosted := "plugins/claude/agentrq/skills/agentrq"
	for _, copied := range []string{
		"plugins/claude/agentrq-custom-supervisor/skills/agentrq",
		"backend/internal/handler/coremcp/skills/agentrq",
	} {
		checkSameSkill(t, root, copied, hosted)
	}
}

// The Cursor plugins are the Claude plugins packaged for Cursor, so their skills
// must not drift from the Claude plugins' copies either.
func TestPluginDocsCursorSkillsMatchTheClaudeOnes(t *testing.T) {
	root := repoRoot(t)
	for _, skill := range []string{
		"agentrq/skills/agentrq",
		"agentrq-workspace/skills/agentrq-workspace",
	} {
		checkSameSkill(t, root, "plugins/cursor/"+skill, "plugins/claude/"+skill)
	}
}

// checkSameSkill checks the skill folder copied holds exactly the files of
// the one at want, byte for byte.
func checkSameSkill(t *testing.T, root, copied, want string) {
	t.Helper()
	got, wanted := skillFolder(t, filepath.Join(root, copied)), skillFolder(t, filepath.Join(root, want))
	for name, body := range wanted {
		if other, ok := got[name]; !ok {
			t.Errorf("%s has no %s; copy %s over it", copied, name, want)
		} else if !bytes.Equal(other, body) {
			t.Errorf("%s differs from %s; copy %s over it", filepath.Join(copied, name), filepath.Join(want, name), want)
		}
	}
	for name := range got {
		if _, ok := wanted[name]; !ok {
			t.Errorf("%s has %s, which %s does not; copy %s over it", copied, name, want, want)
		}
	}
}

// skillFolder reads every file under dir, by its path inside it.
func skillFolder(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		files[filepath.ToSlash(rel)] = body
		return nil
	})
	if err != nil || len(files) == 0 {
		t.Fatalf("cannot read the skill in %s: %v", dir, err)
	}
	return files
}

// Cursor reads the marketplace from the repository root and each plugin from
// the folder its entry names, and lists nothing whose names disagree or whose
// files are missing. Nothing here runs Cursor, so this is the check that they
// are there.
func TestCursorPluginManifest(t *testing.T) {
	root := repoRoot(t)
	marketPath := filepath.Join(root, ".cursor-plugin", "marketplace.json")
	raw, err := os.ReadFile(marketPath)
	if err != nil {
		t.Fatalf("cannot read %s: %v", marketPath, err)
	}
	var market struct {
		Name  string `json:"name"`
		Owner struct {
			Name string `json:"name"`
		} `json:"owner"`
		Plugins []struct {
			Name   string `json:"name"`
			Source string `json:"source"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(raw, &market); err != nil {
		t.Fatalf("invalid JSON in %s: %v", marketPath, err)
	}
	kebab := regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$`)
	if !kebab.MatchString(market.Name) || market.Owner.Name == "" {
		t.Errorf("%s needs a kebab-case name and an owner.name, got %q and %q", marketPath, market.Name, market.Owner.Name)
	}

	// The MCP server each plugin declares. The workspace one has no fixed URL:
	// it is the variable its manifest declares, filled in by Cursor.
	servers := map[string][2]string{
		"agentrq":           {"agentrq", "https://mcp.agentrq.com/mcp"},
		"agentrq-workspace": {"agentrq-workspace", "${AGENTRQ_WORKSPACE_MCP_URL}"},
	}
	var names []string
	for _, entry := range market.Plugins {
		names = append(names, entry.Name)
	}
	if !slices.Equal(names, []string{"agentrq", "agentrq-workspace"}) {
		t.Fatalf("%s lists %v, want [agentrq agentrq-workspace]", marketPath, names)
	}

	for _, entry := range market.Plugins {
		t.Run(entry.Name, func(t *testing.T) {
			pluginDir := filepath.Join(root, entry.Source)
			manifestPath := filepath.Join(pluginDir, ".cursor-plugin", "plugin.json")
			raw, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatalf("cannot read %s: %v", manifestPath, err)
			}
			var manifest struct {
				Name       string `json:"name"`
				Version    string `json:"version"`
				License    string `json:"license"`
				Logo       string `json:"logo"`
				MCPServers string `json:"mcpServers"`
				Variables  struct {
					Properties map[string]json.RawMessage `json:"properties"`
				} `json:"variables"`
			}
			if err := json.Unmarshal(raw, &manifest); err != nil {
				t.Fatalf("invalid JSON in %s: %v", manifestPath, err)
			}
			if manifest.Name != entry.Name {
				t.Errorf("%s calls itself %q; the marketplace calls it %q", manifestPath, manifest.Name, entry.Name)
			}
			if manifest.Version == "" || manifest.License != "Apache-2.0" {
				t.Errorf("%s version = %q, license = %q; want a version and Apache-2.0", manifestPath, manifest.Version, manifest.License)
			}
			if logo, err := os.ReadFile(filepath.Join(pluginDir, manifest.Logo)); err != nil || !bytes.Contains(logo, []byte("<svg")) {
				t.Errorf("%s logo %q is not an SVG in the plugin folder", manifestPath, manifest.Logo)
			}
			if license, err := os.ReadFile(filepath.Join(pluginDir, "LICENSE")); err != nil || !bytes.Contains(license, []byte("Apache License")) {
				t.Errorf("%s has no Apache LICENSE beside its manifest", entry.Source)
			}

			if manifest.MCPServers != "./mcp.json" {
				t.Errorf("%s mcpServers = %q, want ./mcp.json", manifestPath, manifest.MCPServers)
			}
			mcpPath := filepath.Join(pluginDir, "mcp.json")
			raw, err = os.ReadFile(mcpPath)
			if err != nil {
				t.Fatalf("cannot read %s: %v", mcpPath, err)
			}
			var mcp struct {
				MCPServers map[string]struct {
					Type string `json:"type"`
					URL  string `json:"url"`
				} `json:"mcpServers"`
			}
			if err := json.Unmarshal(raw, &mcp); err != nil {
				t.Fatalf("invalid JSON in %s: %v", mcpPath, err)
			}
			want := servers[entry.Name]
			if got := mcp.MCPServers[want[0]]; got.Type != "http" || got.URL != want[1] || len(mcp.MCPServers) != 1 {
				t.Errorf("%s = %v, want only %s, over http at %s", mcpPath, mcp.MCPServers, want[0], want[1])
			}
			// Every ${VAR} the server uses must be a variable the manifest
			// declares, or Cursor has nothing to put there.
			for _, m := range regexp.MustCompile(`\$\{([A-Z0-9_]+)\}`).FindAllStringSubmatch(string(raw), -1) {
				if _, ok := manifest.Variables.Properties[m[1]]; !ok {
					t.Errorf("%s uses ${%s}, which %s does not declare under variables", mcpPath, m[1], manifestPath)
				}
			}

			readme, err := os.ReadFile(filepath.Join(pluginDir, "README.md"))
			if err != nil {
				t.Fatalf("cannot read README.md: %v", err)
			}
			if !strings.Contains(string(readme), "[Privacy Policy](https://agentrq.com/privacy)") {
				t.Errorf("%s README.md does not contain [Privacy Policy](https://agentrq.com/privacy)", entry.Source)
			}
		})
	}
}
