package mcp

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	zlog "github.com/rs/zerolog/log"
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

// buildAutoAllowRule creates the rule string to persist for a given tool + params.
//
// Rules are stored as:
//   - MCP tools:   "mcp_servername_toolname"  (exact match)
//   - Shell tools:  "Bash:git *"              (base-command glob)
//   - Other tools: "Read", "Write"            (exact tool name)
func (ps *WorkspaceServer) buildAutoAllowRule(toolName string, params *PermissionRequestParams) string {
	// MCP tools: store exact name
	if isMCPTool(toolName) {
		return toolName
	}

	// Shell tools: extract the base command and create a glob pattern.
	// Always store with colon format so the UI can show the pattern.
	if isShellTool(toolName) {
		if params != nil {
			if cmd, ok := extractCommandFromPreview(params.InputPreview); ok {
				// Split on shell operators and use the first subcommand's base command
				subcommands := splitShellOperators(cmd)
				if len(subcommands) > 0 {
					base := extractBaseCommand(subcommands[0])
					if base != "" {
						rule := fmt.Sprintf("%s:%s *", toolName, base)
						zlog.Debug().Str("rule", rule).Str("command", cmd).Msg("shell auto-allow rule generated")
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
			cmdPattern := strings.TrimPrefix(pattern, toolName+":")

			if cmd, ok := extractCommandFromPreview(inputPreview); ok {
				if isShellCommandAllowed(cmdPattern, cmd) {
					return true
				}
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

// isShellCommandAllowed checks if a command (potentially with shell operators) matches the pattern.
// Every subcommand (split on &&, ||, ;, |) must match the pattern.
func isShellCommandAllowed(pattern, command string) bool {
	subcommands := splitShellOperators(command)
	for _, sub := range subcommands {
		if !matchesBashPattern(pattern, sub) {
			return false
		}
	}
	return true
}

// splitShellOperators splits a command string on shell operators (&&, ||, ;, |).
func splitShellOperators(command string) []string {
	parts := regexp.MustCompile(`&&|\|\||;|\|`).Split(command, -1)
	var subcommands []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			subcommands = append(subcommands, trimmed)
		}
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
