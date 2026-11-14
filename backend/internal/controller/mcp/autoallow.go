// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlink"
)

// isShellTool returns true if the tool name represents a shell/bash execution tool.
func isShellTool(toolName string) bool {
	return toolName == "Bash" || toolName == "shell_execute" || toolName == "execute_command"
}

// isMCPTool returns true if the tool name represents an MCP tool (prefixed with mcp_).
func isMCPTool(toolName string) bool {
	return strings.HasPrefix(toolName, "mcp_")
}

// extractCommandFromPreview parses the input_preview JSON and extracts the "command" field.
func extractCommandFromPreview(inputPreview string) (string, bool) {
	var input map[string]any
	if err := json.Unmarshal([]byte(inputPreview), &input); err != nil {
		return "", false
	}
	cmd, ok := input["command"].(string)
	if !ok || cmd == "" {
		return "", false
	}
	return cmd, true
}

// extractBaseCommand extracts the leading binary/command name from a shell command string.
// "git status --short" -> "git"
// "npm run build"      -> "npm"
// "/usr/bin/ls -la"    -> "/usr/bin/ls"
// "cd /tmp && ls"      -> "cd"  (caller should split on operators first)
func extractBaseCommand(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	base := fields[0]
	// Strip common shell prefixes: env vars like VAR=val, sudo, etc.
	for strings.Contains(base, "=") && len(fields) > 1 {
		fields = fields[1:]
		base = fields[0]
	}
	return base
}

// buildAutoAllowRule creates the rule string to persist for a given tool + params,
// or "" when no rule may ever allow the command: one saved for it would never
// match it again.
//
// Rules are stored as:
//   - MCP tools:   "mcp_servername_toolname"  (exact match)
//   - Shell tools:  "Bash:git *"              (base-command glob)
//   - Other tools: "Read", "Write"            (exact tool name)
func (ps *WorkspaceServer) buildAutoAllowRule(toolName string, params *agentlink.PermissionRequest) string {
	// MCP tools: store exact name
	if isMCPTool(toolName) {
		return toolName
	}

	// Shell tools: extract the base command and create a glob pattern.
	// Always store with colon format so the UI can show the pattern.
	if isShellTool(toolName) {
		if params != nil {
			if cmd, ok := extractCommandFromPreview(params.InputPreview); ok {
				shell := parseShellCommand(cmd)
				if shell.unsafe {
					return ""
				}
				// Use the first subcommand's base command
				subcommands := shell.subcommands
				if len(subcommands) > 0 {
					base := extractBaseCommand(subcommands[0])
					if base != "" {
						rule := fmt.Sprintf("%s:%s *", toolName, base)
						return rule
					}
				}
			}
		}
		// Fallback: can't extract specific command — allow all commands for this shell tool
		return fmt.Sprintf("%s:*", toolName)
	}

	// Everything else: store exact tool name
	return toolName
}

// checkAutoAllow determines if a tool request is automatically permitted based on stored patterns.
//
// Matching priority:
//  1. MCP tools (mcp_ prefix): exact name match only
//  2. Shell tools (Bash, shell_execute, execute_command): command-aware pattern matching
//  3. Other tools: direct tool name match
func (ps *WorkspaceServer) checkAutoAllow(toolName, inputPreview string) bool {
	// The command is parsed once, not once per stored rule.
	var shell shellCommand
	hasCommand := false
	if isShellTool(toolName) {
		if cmd, ok := extractCommandFromPreview(inputPreview); ok {
			shell, hasCommand = parseShellCommand(cmd), true
		}
	}

	for _, pattern := range ps.autoAllowedTools {
		// 1. MCP tools: exact name match
		if isMCPTool(toolName) {
			if pattern == toolName {
				return true
			}
			continue // MCP tools should only match exactly, skip other pattern types
		}

		// 2. Shell tools: command-aware matching
		if isShellTool(toolName) && strings.HasPrefix(pattern, toolName+":") {
			if hasCommand && shell.allowedBy(strings.TrimPrefix(pattern, toolName+":")) {
				return true
			}
			continue // Shell patterns only apply to shell tools
		}

		// 3. Non-shell, non-MCP: direct tool name match
		if pattern == toolName {
			return true
		}
	}
	return false
}

// shellCommand is a command as the rules see it: the commands it runs, and
// whether it does something that no "<base> *" rule's text can show.
type shellCommand struct {
	subcommands []string
	unsafe      bool
}

func parseShellCommand(command string) shellCommand {
	subcommands := splitShellOperators(command)
	return shellCommand{subcommands: subcommands, unsafe: runsUnnamedCode(command, subcommands)}
}

// allowedBy reports whether every subcommand matches pattern. Only the
// catch-all "*" covers an unsafe command: that user allowed everything.
func (c shellCommand) allowedBy(pattern string) bool {
	if pattern == "*" {
		return true
	}
	if c.unsafe {
		return false
	}
	for _, sub := range c.subcommands {
		if !matchesBashPattern(pattern, sub) {
			return false
		}
	}
	return true
}

// isShellCommandAllowed checks if a command (potentially with shell operators) matches the pattern.
func isShellCommandAllowed(pattern, command string) bool {
	return parseShellCommand(command).allowedBy(pattern)
}

// substitutions run a command inside another one's arguments: bash's $( ` <( >(
// and zsh's =(. Quoted text holding one is refused too; telling the two apart
// needs a shell parser.
var substitutions = []string{"$(", "`", "<(", ">(", "=("}

// zshExecQualifier matches a zsh glob qualifier that runs code, *(e:'cmd':) or
// *(+func). It must touch the word before it, so "fix (scope)" is not caught.
var zshExecQualifier = regexp.MustCompile(`[^\s(]\([^)]*(?:e[^\w\s)]|\+\w)`)

// runsUnnamedCode reports whether command runs, or plants, code that a rule
// matching its text never named.
func runsUnnamedCode(command string, subcommands []string) bool {
	for _, s := range substitutions {
		if strings.Contains(command, s) {
			return true
		}
	}
	if zshExecQualifier.MatchString(command) || writesFile(command) {
		return true
	}
	for _, sub := range subcommands {
		if hasExecOption(sub) {
			return true
		}
	}
	return false
}

// writesFile reports whether command redirects output into a file: one written
// that way (a git hook, ~/.bashrc) runs later, unprompted. Duplicating a
// descriptor (2>&1, >&2, 2>&-) and discarding into /dev/null are fine.
func writesFile(command string) bool {
	for i := 0; i < len(command); i++ {
		if command[i] != '>' {
			continue
		}
		i++
		if i < len(command) && (command[i] == '>' || command[i] == '|') {
			i++
		}
		if i < len(command) && command[i] == '&' {
			j := i + 1
			for j < len(command) && (command[j] >= '0' && command[j] <= '9' || command[j] == '-') {
				j++
			}
			if j > i+1 {
				i = j - 1
				continue
			}
			i++ // ">&file" writes to file, as "&>file" does
		}
		for i < len(command) && (command[i] == ' ' || command[i] == '\t') {
			i++
		}
		start := i
		for i < len(command) && !strings.ContainsRune(" \t\r\n;&|<>)", rune(command[i])) {
			i++
		}
		if strings.Trim(command[start:i], `'"`) != "/dev/null" {
			return true
		}
		i--
	}
	return false
}

// hasExecOption reports whether a subcommand hands its program an option that
// runs a command or changes what will run, which "git *" or "find *" would
// otherwise let through.
func hasExecOption(subcommand string) bool {
	fields := strings.Fields(subcommand)
	for len(fields) > 1 && strings.Contains(fields[0], "=") {
		fields = fields[1:]
	}
	switch path.Base(fields[0]) { // subcommands are trimmed and never empty
	case "git":
		return gitHasExecOption(fields[1:])
	case "find":
		for _, a := range fields[1:] {
			switch a {
			case "-exec", "-execdir", "-ok", "-okdir", "-delete", "-fls", "-fprint", "-fprint0", "-fprintf":
				return true
			}
		}
	}
	return false
}

// gitHasExecOption covers git's own ways to run a command: config it reads (-c,
// a config write, another --git-dir), helpers it starts (--upload-pack, rebase
// --exec, bisect run…) and files it writes (--output, another --work-tree).
func gitHasExecOption(args []string) bool {
	i := 0
	for ; i < len(args) && strings.HasPrefix(args[i], "-"); i++ {
		for _, p := range []string{"-c", "--config-env", "--exec-path", "--git-dir", "--work-tree"} {
			if strings.HasPrefix(args[i], p) {
				return true
			}
		}
		switch args[i] {
		case "-C", "--namespace", "--super-prefix", "--attr-source", "--list-cmds":
			i++ // takes a value
		}
	}
	if i >= len(args) {
		return false
	}
	sub, rest := args[i], args[i+1:]
	for _, a := range rest {
		if a == "--output" {
			return true
		}
		for _, p := range []string{"--upload-pack", "--receive-pack", "--exec", "--output=", "--extcmd", "--open-files-in-pager"} {
			if strings.HasPrefix(a, p) {
				return true
			}
		}
	}
	switch sub {
	case "config":
		return !gitConfigOnlyReads(rest)
	case "filter-branch", "instaweb", "send-email":
		return true
	case "bisect":
		return len(rest) > 0 && rest[0] == "run"
	case "submodule":
		for _, a := range rest {
			if a == "foreach" {
				return true
			}
		}
	case "rebase", "difftool":
		return hasShortFlag(rest, 'x')
	case "grep":
		return hasShortFlag(rest, 'O')
	case "clone":
		return hasShortFlag(rest, 'u')
	}
	return false
}

// hasShortFlag reports whether args hold the short option -flag, alone or in a
// cluster such as -ix.
func hasShortFlag(args []string, flag byte) bool {
	for _, a := range args {
		if len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.IndexByte(a[1:], flag) >= 0 {
			return true
		}
	}
	return false
}

// gitConfigOnlyReads reports whether git config only reads. A write (core.pager,
// alias.x=!cmd) is a command the next git runs.
func gitConfigOnlyReads(args []string) bool {
	if len(args) > 0 && (args[0] == "get" || args[0] == "list") {
		return true
	}
	plain := 0
	for _, a := range args {
		switch a {
		case "--get", "--get-all", "--get-regexp", "--get-urlmatch", "--list", "-l":
			return true
		}
		if !strings.HasPrefix(a, "-") {
			plain++
		}
	}
	return plain == 1
}

// shellOperators matches every token that can separate two commands, plus the
// redirect forms that merely contain an ampersand. A newline is a separator in
// its own right, not merely something the glob's "." fails to match.
var shellOperators = regexp.MustCompile(`[0-9]*>&[0-9-]*|&>>?|&&|\|\||;|\||&|[\r\n]+`)

// isShellSeparator reports whether a token matched by shellOperators ends a
// command, as opposed to being a redirect that happens to contain an ampersand.
func isShellSeparator(token string) bool {
	switch token {
	case "&&", "||", ";", "|", "&":
		return true
	}
	return strings.Trim(token, "\r\n") == ""
}

// splitShellOperators splits a command string into the commands it actually
// runs, cutting on &&, ||, ;, |, & and newlines, and leaving redirects intact.
func splitShellOperators(command string) []string {
	var subcommands []string
	start := 0
	for _, loc := range shellOperators.FindAllStringIndex(command, -1) {
		if !isShellSeparator(command[loc[0]:loc[1]]) {
			continue
		}
		if trimmed := strings.TrimSpace(command[start:loc[0]]); trimmed != "" {
			subcommands = append(subcommands, trimmed)
		}
		start = loc[1]
	}
	if trimmed := strings.TrimSpace(command[start:]); trimmed != "" {
		subcommands = append(subcommands, trimmed)
	}
	return subcommands
}

// matchesBashPattern checks if a command matches a glob pattern.
// Glob syntax: * matches any sequence of characters.
// Examples:
//
//	"git *"  matches "git status", "git push origin main"
//	"npm *"  matches "npm run build", "npm install"
//	"ls"     matches "ls" only
func matchesBashPattern(pattern, command string) bool {
	if pattern == command {
		return true
	}

	// Convert glob pattern to regex
	regexPattern := regexp.QuoteMeta(pattern)
	regexPattern = strings.ReplaceAll(regexPattern, `\*`, `.*`)

	matched, _ := regexp.MatchString("^"+regexPattern+"$", command)
	return matched
}
