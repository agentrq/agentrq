// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect } from 'vitest';

import {
  EDITABLE_SETTINGS_TABS,
  READ_ONLY_SETTINGS_TABS,
  buildClaudePermissionsConfig,
  buildMcpServers,
  buildSupervisorMcpUrl,
  isReadOnlySettingsTab,
  shouldShowSettingsActionBar,
} from '../src/composables/useWorkspaceSettings';

describe('useWorkspaceSettings', () => {
  describe('tab classification constants', () => {
    it('defines editable tabs that persist form state', () => {
      expect(EDITABLE_SETTINGS_TABS).toEqual(['general', 'input', 'automations', 'notifications']);
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

  describe('buildClaudePermissionsConfig', () => {
    it('allows every tool of the workspace server with one wildcard', () => {
      const { permissions } = buildClaudePermissionsConfig('agentrq-ws1');

      expect(permissions.allow).toEqual(['mcp__agentrq-ws1__*']);
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

    it('does not add the core server for a workspace that is not named exactly "supervisor"', () => {
      const config = buildClaudePermissionsConfig('agentrq-ws1', 'Supervisor');

      expect(config.permissions.allow).toEqual(['mcp__agentrq-ws1__*']);
      expect(config.enabledMcpjsonServers).toEqual(['agentrq-ws1']);
    });

    it('allows and enables the "agentrq" core server for a supervisor workspace', () => {
      const config = buildClaudePermissionsConfig('agentrq-ws1', 'supervisor');

      expect(config.permissions.allow).toEqual(['mcp__agentrq-ws1__*', 'mcp__agentrq__*']);
      expect(config.enabledMcpjsonServers).toEqual(['agentrq-ws1', 'agentrq']);
    });
  });

  describe('buildSupervisorMcpUrl', () => {
    it('rewrites a subdomain-based workspace mcpUrl to the bare mcp.<domain>/mcp host', () => {
      const url = buildSupervisorMcpUrl({
        workspaceMcpUrl: 'https://a1b2.mcp.agentrq.com',
        origin: 'https://app.agentrq.com',
        basePath: '',
      });

      expect(url).toBe('https://mcp.agentrq.com/mcp');
    });

    it('preserves http and strips any query string before templating', () => {
      const url = buildSupervisorMcpUrl({
        workspaceMcpUrl: 'http://a1b2.mcp.example.internal?token=secret',
        origin: 'http://app.example.internal',
        basePath: '',
      });

      expect(url).toBe('http://mcp.example.internal/mcp');
    });

    it('falls back to the origin-based bare /mcp path when there is no subdomain masking', () => {
      const url = buildSupervisorMcpUrl({
        workspaceMcpUrl: 'http://localhost:8080/mcp/abc123',
        origin: 'http://localhost:8080',
        basePath: '',
      });

      expect(url).toBe('http://localhost:8080/mcp');
    });

    it('honors a configured base path in the fallback case', () => {
      const url = buildSupervisorMcpUrl({
        workspaceMcpUrl: '',
        origin: 'https://app.agentrq.com',
        basePath: '/abc/def/',
      });

      expect(url).toBe('https://app.agentrq.com/abc/def/mcp');
    });
  });

  describe('buildMcpServers', () => {
    it('writes a single entry for a regular workspace', () => {
      const servers = buildMcpServers({
        serverName: 'agentrq-ws1',
        authenticatedUrl: 'https://a1b2.mcp.agentrq.com?token=tok',
        workspaceName: 'My Workspace',
        supervisorMcpUrl: 'https://mcp.agentrq.com/mcp',
      });

      expect(servers).toEqual({
        'agentrq-ws1': { type: 'http', url: 'https://a1b2.mcp.agentrq.com?token=tok' },
      });
    });

    it('adds a second "agentrq" entry for a workspace named exactly "supervisor"', () => {
      const servers = buildMcpServers({
        serverName: 'agentrq-ws1',
        authenticatedUrl: 'https://a1b2.mcp.agentrq.com?token=tok',
        workspaceName: 'supervisor',
        supervisorMcpUrl: 'https://mcp.agentrq.com/mcp',
      });

      expect(servers).toEqual({
        'agentrq-ws1': { type: 'http', url: 'https://a1b2.mcp.agentrq.com?token=tok' },
        agentrq: { type: 'http', url: 'https://mcp.agentrq.com/mcp' },
      });
    });

    it('does not add the second entry for a name that only looks like supervisor', () => {
      const servers = buildMcpServers({
        serverName: 'agentrq-ws1',
        authenticatedUrl: 'https://a1b2.mcp.agentrq.com?token=tok',
        workspaceName: 'Supervisor',
        supervisorMcpUrl: 'https://mcp.agentrq.com/mcp',
      });

      expect(Object.keys(servers)).toEqual(['agentrq-ws1']);
    });
  });
});
