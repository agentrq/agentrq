/**
 * Workspace settings tabs and their editing policies.
 *
 * The settings screen is shared between tabs that edit workspace properties
 * (persisted through `updateWorkspace`), read-only information screens (setup
 * guides, agent memories), dedicated integration flows (slack), and
 * destructive actions (danger zone).
 *
 * The bottom action bar carrying "Cancel" and "Update Workspace" belongs only
 * to the editable tabs of an active workspace.
 */

/**
 * Tabs on the workspace settings screen with form fields that save changes via
 * the bottom action bar.
 */
export const EDITABLE_SETTINGS_TABS = Object.freeze([
  'general',
  'automations',
  'notifications',
]);

/**
 * Tabs on the workspace settings screen that are read-only views.
 */
export const READ_ONLY_SETTINGS_TABS = Object.freeze([
  'setup',
  'memories',
]);

/**
 * Reports whether a given settings tab is read-only.
 *
 * @param {string} tab
 * @returns {boolean}
 */
export function isReadOnlySettingsTab(tab) {
  return READ_ONLY_SETTINGS_TABS.includes(tab);
}

/**
 * Determines whether the bottom action bar (Cancel and Update Workspace buttons)
 * should be displayed.
 *
 * The action bar is shown only for editable tabs. Read-only tabs (setup,
 * memories), custom-action tabs (slack, danger), and archived workspaces omit
 * it.
 *
 * @param {string} tab The ID of the currently active tab.
 * @param {{ archivedAt?: string | null } | boolean | null} [workspaceOrArchived] Workspace object or boolean archive flag.
 * @returns {boolean}
 */
export function shouldShowSettingsActionBar(tab, workspaceOrArchived = null) {
  if (!EDITABLE_SETTINGS_TABS.includes(tab)) {
    return false;
  }
  if (typeof workspaceOrArchived === 'boolean') {
    return !workspaceOrArchived;
  }
  if (workspaceOrArchived && Boolean(workspaceOrArchived.archivedAt)) {
    return false;
  }
  return true;
}

/**
 * Every tool the workspace MCP server exposes to a connected agent, in the
 * order the server registers them.
 *
 * This list mirrors the `mcp.AddTool` block in
 * `backend/internal/controller/mcp/server.go`, and `test/workspaceSettings.test.js`
 * reads that Go source to enforce the match. Keeping it whole is the point: a
 * name missing here is a tool the agent has to stop and ask permission for on
 * every single call, which is exactly the prompt fatigue the generated config
 * exists to remove.
 *
 * Note that `deleteMemory` is pre-approved along with the rest. Workspace
 * memory is versioned by nothing, so this does hand the agent an irreversible
 * tool — but the alternative is a config that stalls mid-task, and an operator
 * who wants the prompt can drop the line from the snippet they paste.
 */
export const WORKSPACE_MCP_TOOLS = Object.freeze([
  'createTask',
  'updateTaskStatus',
  'reply',
  'downloadAttachment',
  'getWorkspace',
  'getTask',
  'publishEvent',
  'loadMemory',
  'saveMemory',
  'deleteMemory',
  'elicit',
]);

/**
 * Builds the `.claude/settings.local.json` contents shown on the setup tab.
 *
 * The server name has to match the key used in `.mcp.json`, since that is what
 * Claude Code prefixes onto each tool to form the permission entry.
 *
 * @param {string} serverName The MCP server key, e.g. `agentrq-0ZzhYQG2qtl`.
 * @returns {{permissions: {allow: string[]}, enableAllProjectMcpServers: boolean, enabledMcpjsonServers: string[]}}
 */
export function buildClaudePermissionsConfig(serverName) {
  return {
    permissions: {
      allow: WORKSPACE_MCP_TOOLS.map((tool) => `mcp__${serverName}__${tool}`),
    },
    enableAllProjectMcpServers: true,
    enabledMcpjsonServers: [serverName],
  };
}
