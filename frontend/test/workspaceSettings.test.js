import { existsSync, readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';

import { describe, it, expect } from 'vitest';

import {
  EDITABLE_SETTINGS_TABS,
  READ_ONLY_SETTINGS_TABS,
  WORKSPACE_MCP_TOOLS,
  buildClaudePermissionsConfig,
  isReadOnlySettingsTab,
  shouldShowSettingsActionBar,
} from '../src/composables/useWorkspaceSettings';

const SERVER_GO = 'backend/internal/controller/mcp/server.go';

/**
 * Locates the Go MCP server's source by walking up from the working directory.
 *
 * The path is not derived from `import.meta.url`: these tests run under jsdom,
 * where that is an http URL rather than a file one.
 */
function serverSourcePath() {
  for (let dir = process.cwd(); ; dir = dirname(dir)) {
    const candidate = resolve(dir, SERVER_GO);
    if (existsSync(candidate)) {
      return candidate;
    }
    if (dirname(dir) === dir) {
      throw new Error(`could not find ${SERVER_GO} above ${process.cwd()}`);
    }
  }
}

/**
 * The tool names the Go MCP server registers, read straight out of its source.
 *
 * Every `Name:` in that file belongs to an `mcp.AddTool` call, but the call is
 * matched explicitly so that a `Name` field added elsewhere later cannot quietly
 * inflate this list.
 */
function toolsRegisteredByServer() {
  const source = readFileSync(serverSourcePath(), 'utf-8');
  return [...source.matchAll(/mcp\.AddTool\(mcpSrv, &mcp\.Tool\{\s*Name:\s*"([^"]+)"/g)]
    .map((match) => match[1]);
}

describe('useWorkspaceSettings', () => {
  describe('tab classification constants', () => {
    it('defines editable tabs that persist form state', () => {
      expect(EDITABLE_SETTINGS_TABS).toEqual(['general', 'automations', 'notifications']);
    });

    it('defines read-only tabs that only display information', () => {
      expect(READ_ONLY_SETTINGS_TABS).toEqual(['setup', 'memories']);
    });
  });

  describe('isReadOnlySettingsTab', () => {
    it('identifies memories and setup as read-only', () => {
      expect(isReadOnlySettingsTab('memories')).toBe(true);
      expect(isReadOnlySettingsTab('setup')).toBe(true);
    });

    it('returns false for editable or action-based tabs', () => {
      expect(isReadOnlySettingsTab('general')).toBe(false);
      expect(isReadOnlySettingsTab('automations')).toBe(false);
      expect(isReadOnlySettingsTab('notifications')).toBe(false);
      expect(isReadOnlySettingsTab('slack')).toBe(false);
      expect(isReadOnlySettingsTab('danger')).toBe(false);
    });

    it('returns false for unknown or missing tabs', () => {
      expect(isReadOnlySettingsTab('unknown')).toBe(false);
      expect(isReadOnlySettingsTab(undefined)).toBe(false);
      expect(isReadOnlySettingsTab(null)).toBe(false);
    });
  });

  describe('shouldShowSettingsActionBar', () => {
    it('shows action bar for editable tabs in active workspaces', () => {
      expect(shouldShowSettingsActionBar('general')).toBe(true);
      expect(shouldShowSettingsActionBar('automations')).toBe(true);
      expect(shouldShowSettingsActionBar('notifications')).toBe(true);

      const activeWorkspace = { id: 'ws1', name: 'Active Workspace', archivedAt: null };
      expect(shouldShowSettingsActionBar('general', activeWorkspace)).toBe(true);
      expect(shouldShowSettingsActionBar('automations', activeWorkspace)).toBe(true);
      expect(shouldShowSettingsActionBar('notifications', activeWorkspace)).toBe(true);

      expect(shouldShowSettingsActionBar('general', false)).toBe(true);
    });

    it('hides action bar on read-only tabs like memories and setup', () => {
      expect(shouldShowSettingsActionBar('memories')).toBe(false);
      expect(shouldShowSettingsActionBar('setup')).toBe(false);

      const activeWorkspace = { id: 'ws1', archivedAt: null };
      expect(shouldShowSettingsActionBar('memories', activeWorkspace)).toBe(false);
      expect(shouldShowSettingsActionBar('setup', activeWorkspace)).toBe(false);
    });

    it('hides action bar on dedicated action tabs like slack and danger', () => {
      expect(shouldShowSettingsActionBar('slack')).toBe(false);
      expect(shouldShowSettingsActionBar('danger')).toBe(false);
    });

    it('hides action bar for unknown tabs', () => {
      expect(shouldShowSettingsActionBar('custom')).toBe(false);
      expect(shouldShowSettingsActionBar('')).toBe(false);
      expect(shouldShowSettingsActionBar(null)).toBe(false);
    });

    it('hides action bar when workspace is archived, even on editable tabs', () => {
      const archivedWorkspace = { id: 'ws1', archivedAt: '2026-09-01T12:00:00Z' };
      expect(shouldShowSettingsActionBar('general', archivedWorkspace)).toBe(false);
      expect(shouldShowSettingsActionBar('automations', archivedWorkspace)).toBe(false);
      expect(shouldShowSettingsActionBar('notifications', archivedWorkspace)).toBe(false);

      // Boolean flag overload
      expect(shouldShowSettingsActionBar('general', true)).toBe(false);
    });
  });

  describe('WORKSPACE_MCP_TOOLS', () => {
    it('lists every tool the MCP server registers, in the same order', () => {
      expect(WORKSPACE_MCP_TOOLS).toEqual(toolsRegisteredByServer());
    });

    it('names the tools an agent needs to work a task end to end', () => {
      expect(WORKSPACE_MCP_TOOLS).toEqual([
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
    });

    it('is frozen, so a caller cannot mutate the shared list', () => {
      expect(Object.isFrozen(WORKSPACE_MCP_TOOLS)).toBe(true);
    });
  });

  describe('buildClaudePermissionsConfig', () => {
    it('prefixes every tool with the MCP server name', () => {
      const { permissions } = buildClaudePermissionsConfig('agentrq-ws1');

      expect(permissions.allow).toEqual([
        'mcp__agentrq-ws1__createTask',
        'mcp__agentrq-ws1__updateTaskStatus',
        'mcp__agentrq-ws1__reply',
        'mcp__agentrq-ws1__downloadAttachment',
        'mcp__agentrq-ws1__getWorkspace',
        'mcp__agentrq-ws1__getTask',
        'mcp__agentrq-ws1__publishEvent',
        'mcp__agentrq-ws1__loadMemory',
        'mcp__agentrq-ws1__saveMemory',
        'mcp__agentrq-ws1__deleteMemory',
        'mcp__agentrq-ws1__elicit',
      ]);
    });

    it('pre-approves every registered tool, leaving no prompt behind', () => {
      const { permissions } = buildClaudePermissionsConfig('agentrq-ws1');

      expect(permissions.allow).toHaveLength(toolsRegisteredByServer().length);
    });

    it('enables the project MCP servers and names this one', () => {
      const config = buildClaudePermissionsConfig('agentrq-ws1');

      expect(config.enableAllProjectMcpServers).toBe(true);
      expect(config.enabledMcpjsonServers).toEqual(['agentrq-ws1']);
    });

    it('serialises to the snippet shape the setup tab displays', () => {
      const config = buildClaudePermissionsConfig('agentrq-ws1');

      expect(Object.keys(config)).toEqual([
        'permissions',
        'enableAllProjectMcpServers',
        'enabledMcpjsonServers',
      ]);
      expect(JSON.parse(JSON.stringify(config))).toEqual(config);
    });
  });
});
