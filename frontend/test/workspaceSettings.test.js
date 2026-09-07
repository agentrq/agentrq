import { describe, it, expect } from 'vitest';

import {
  EDITABLE_SETTINGS_TABS,
  READ_ONLY_SETTINGS_TABS,
  isReadOnlySettingsTab,
  shouldShowSettingsActionBar,
} from '../src/composables/useWorkspaceSettings';

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
});
