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
