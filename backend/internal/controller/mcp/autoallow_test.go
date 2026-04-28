package mcp

import (
	"testing"
)

func TestIsMCPTool(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		want     bool
	}{
		{"mcp tool", "mcp_agentrq_createTask", true},
		{"mcp tool with prefix", "mcp_server_tool", true},
		{"Bash tool", "Bash", false},
		{"Read tool", "Read", false},
		{"empty string", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isMCPTool(tt.toolName); got != tt.want {
				t.Errorf("isMCPTool(%q) = %v, want %v", tt.toolName, got, tt.want)
			}
		})
	}
}

func TestIsShellTool(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		want     bool
	}{
		{"Bash", "Bash", true},
		{"shell_execute", "shell_execute", true},
		{"execute_command", "execute_command", true},
		{"Read", "Read", false},
		{"Write", "Write", false},
		{"mcp tool", "mcp_server_tool", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isShellTool(tt.toolName); got != tt.want {
				t.Errorf("isShellTool(%q) = %v, want %v", tt.toolName, got, tt.want)
			}
		})
	}
}

func TestExtractCommandFromPreview(t *testing.T) {
	tests := []struct {
		name         string
		inputPreview string
		wantCmd      string
		wantOK       bool
	}{
		{"simple command", `{"command": "git status"}`, "git status", true},
		{"npm command", `{"command": "npm run build"}`, "npm run build", true},
		{"empty command", `{"command": ""}`, "", false},
		{"no command field", `{"path": "/foo"}`, "", false},
		{"invalid JSON", `not json`, "", false},
		{"empty input", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, ok := extractCommandFromPreview(tt.inputPreview)
			if cmd != tt.wantCmd || ok != tt.wantOK {
				t.Errorf("extractCommandFromPreview(%q) = (%q, %v), want (%q, %v)", tt.inputPreview, cmd, ok, tt.wantCmd, tt.wantOK)
			}
		})
	}
}

func TestExtractBaseCommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    string
	}{
		{"simple command", "git status --short", "git"},
		{"npm command", "npm run build", "npm"},
		{"absolute path", "/usr/bin/ls -la", "/usr/bin/ls"},
		{"single command", "ls", "ls"},
		{"env var prefix", "NODE_ENV=production npm run build", "npm"},
		{"multiple env vars", "FOO=1 BAR=2 node index.js", "node"},
		{"empty string", "", ""},
		{"just spaces", "   ", ""},
		{"sudo", "sudo apt install", "sudo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractBaseCommand(tt.command); got != tt.want {
				t.Errorf("extractBaseCommand(%q) = %q, want %q", tt.command, got, tt.want)
			}
		})
	}
}

func TestSplitShellOperators(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    []string
	}{
		{"single command", "git status", []string{"git status"}},
		{"and operator", "cd /tmp && ls", []string{"cd /tmp", "ls"}},
		{"or operator", "cmd1 || cmd2", []string{"cmd1", "cmd2"}},
		{"semicolon", "cmd1 ; cmd2", []string{"cmd1", "cmd2"}},
		{"pipe", "cat file | grep foo", []string{"cat file", "grep foo"}},
		{"multiple operators", "cd /tmp && npm install && npm run build", []string{"cd /tmp", "npm install", "npm run build"}},
		{"empty", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitShellOperators(tt.command)
			if len(got) != len(tt.want) {
				t.Errorf("splitShellOperators(%q) = %v, want %v", tt.command, got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitShellOperators(%q)[%d] = %q, want %q", tt.command, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestMatchesBashPattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		command string
		want    bool
	}{
		{"exact match", "git status", "git status", true},
		{"glob wildcard", "git *", "git status", true},
		{"glob wildcard multi arg", "git *", "git push origin main", true},
		{"glob no match", "git *", "npm install", false},
		{"no wildcard no match", "git status", "git push", false},
		{"npm glob", "npm *", "npm run build", true},
		{"npm glob install", "npm *", "npm install", true},
		{"ls exact", "ls", "ls", true},
		{"ls no match", "ls", "ls -la", false},
		{"ls glob", "ls *", "ls -la", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesBashPattern(tt.pattern, tt.command); got != tt.want {
				t.Errorf("matchesBashPattern(%q, %q) = %v, want %v", tt.pattern, tt.command, got, tt.want)
			}
		})
	}
}

func TestIsShellCommandAllowed(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		command string
		want    bool
	}{
		{"single matching", "git *", "git status", true},
		{"chained matching all git", "git *", "git add . && git commit -m fix", true}, // both subcommands are git commands
		{"single no match", "npm *", "git status", false},
		{"pipe match", "git *", "git log | head", false}, // head doesn't match git *
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isShellCommandAllowed(tt.pattern, tt.command); got != tt.want {
				t.Errorf("isShellCommandAllowed(%q, %q) = %v, want %v", tt.pattern, tt.command, got, tt.want)
			}
		})
	}
}

func TestBuildAutoAllowRule(t *testing.T) {
	ps := &WorkspaceServer{}

	tests := []struct {
		name     string
		toolName string
		params   *PermissionRequestParams
		want     string
	}{
		{
			"MCP tool",
			"mcp_agentrq_createTask",
			&PermissionRequestParams{InputPreview: `{"title": "test"}`},
			"mcp_agentrq_createTask",
		},
		{
			"Bash with git command",
			"Bash",
			&PermissionRequestParams{InputPreview: `{"command": "git status --short"}`},
			"Bash:git *",
		},
		{
			"Bash with npm command",
			"Bash",
			&PermissionRequestParams{InputPreview: `{"command": "npm run build"}`},
			"Bash:npm *",
		},
		{
			"Bash with chained command",
			"Bash",
			&PermissionRequestParams{InputPreview: `{"command": "cd /tmp && ls -la"}`},
			"Bash:cd *",
		},
		{
			"shell_execute with ls",
			"shell_execute",
			&PermissionRequestParams{InputPreview: `{"command": "ls -la"}`},
			"shell_execute:ls *",
		},
		{
			"Read tool (no params needed)",
			"Read",
			nil,
			"Read",
		},
		{
			"Write tool",
			"Write",
			&PermissionRequestParams{InputPreview: `{"path": "/foo/bar.txt"}`},
			"Write",
		},
		{
			"Bash with nil params",
			"Bash",
			nil,
			"Bash:*",
		},
		{
			"Bash with invalid JSON",
			"Bash",
			&PermissionRequestParams{InputPreview: "not json"},
			"Bash:*",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ps.buildAutoAllowRule(tt.toolName, tt.params); got != tt.want {
				t.Errorf("buildAutoAllowRule(%q, ...) = %q, want %q", tt.toolName, got, tt.want)
			}
		})
	}
}

func TestCheckAutoAllow(t *testing.T) {
	tests := []struct {
		name         string
		allowedTools []string
		toolName     string
		inputPreview string
		want         bool
	}{
		{
			"MCP tool exact match",
			[]string{"mcp_server_tool"},
			"mcp_server_tool",
			`{"arg": "val"}`,
			true,
		},
		{
			"MCP tool no match",
			[]string{"mcp_server_tool"},
			"mcp_server_other",
			`{"arg": "val"}`,
			false,
		},
		{
			"MCP tool doesnt match non-mcp pattern",
			[]string{"Read", "Write"},
			"mcp_server_tool",
			`{"arg": "val"}`,
			false,
		},
		{
			"Bash git glob match",
			[]string{"Bash:git *"},
			"Bash",
			`{"command": "git status"}`,
			true,
		},
		{
			"Bash git glob different git command",
			[]string{"Bash:git *"},
			"Bash",
			`{"command": "git push origin main"}`,
			true,
		},
		{
			"Bash npm no match for git rule",
			[]string{"Bash:git *"},
			"Bash",
			`{"command": "npm install"}`,
			false,
		},
		{
			"Direct tool name match",
			[]string{"Read"},
			"Read",
			`{"path": "/foo"}`,
			true,
		},
		{
			"Direct tool name no match",
			[]string{"Read"},
			"Write",
			`{"path": "/foo"}`,
			false,
		},
		{
			"Multiple rules - one matches",
			[]string{"Bash:git *", "Bash:npm *", "Read"},
			"Bash",
			`{"command": "npm run build"}`,
			true,
		},
		{
			"Bash chained command - partial match fails",
			[]string{"Bash:git *"},
			"Bash",
			`{"command": "git status && npm install"}`,
			false, // both subcommands must match the pattern
		},
		{
			"Bash chained command - all match",
			[]string{"Bash:git *"},
			"Bash",
			`{"command": "git add . && git commit -m fix"}`,
			true,
		},
		{
			"shell_execute pattern doesnt match Bash",
			[]string{"shell_execute:git *"},
			"Bash",
			`{"command": "git status"}`,
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ps := &WorkspaceServer{
				autoAllowedTools: tt.allowedTools,
			}
			if got := ps.checkAutoAllow(tt.toolName, tt.inputPreview); got != tt.want {
				t.Errorf("checkAutoAllow(%q, %q) with rules %v = %v, want %v",
					tt.toolName, tt.inputPreview, tt.allowedTools, got, tt.want)
			}
		})
	}
}
