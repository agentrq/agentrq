// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { computed, reactive } from 'vue';
import { canFork, isFork, mergeBlockedReason, parentName } from './useWorkspaceForks';

/**
 * What a right-click on a sidebar workspace offers.
 *
 * The same shape as `useTaskContextMenu`, so `ContextMenu.vue` draws both. A
 * workspace offers Fork; a fork offers Merge instead, because a fork cannot
 * have forks of its own and merging is the only way out of one. The supervisor
 * workspace is account-wide by name and offers neither, so its menu is empty
 * and never opens.
 */

/** The items AgentRQ itself offers on a workspace. */
export const BUILT_IN_ITEMS = [{ key: 'fork', label: 'Fork workspace' }];

/**
 * The item list for one workspace.
 *
 * Merge is listed even while blocked, disabled with the reason beside it: a
 * menu that silently lacked it would leave somebody looking for the way out.
 */
export function workspaceMenuItems(ws, workspaces = []) {
  if (isFork(ws)) {
    const reason = mergeBlockedReason(ws);
    return [{
      key: 'merge',
      label: `Merge into ${parentName(ws, workspaces)}`,
      disabled: !!reason,
      detail: reason,
    }];
  }
  return canFork(ws) ? [...BUILT_IN_ITEMS] : [];
}

/**
 * The menu's state.
 *
 * `workspaces` is a getter so the items follow the list: the menu refreshes it
 * on open, and a task that finished a moment ago should enable Merge without
 * the menu being opened again.
 */
export function useWorkspaceContextMenu({ workspaces = () => [], onOpen = () => {} } = {}) {
  const state = reactive({ show: false, x: 0, y: 0, workspace: null, keyboard: false });

  return {
    menu: computed(() => state),
    items: computed(() => workspaceMenuItems(state.workspace, workspaces())),

    /** From a right-click, at the pointer. Nothing opens for a workspace with no items. */
    open(event, ws) {
      if (workspaceMenuItems(ws, workspaces()).length === 0) return false;
      Object.assign(state, { show: true, x: event.clientX, y: event.clientY, workspace: ws, keyboard: false });
      onOpen(ws);
      return true;
    },

    /**
     * From the row's own button or Shift+F10: under the element, since there
     * is no pointer, and marked so the menu takes the focus.
     */
    openAt(el, ws) {
      const rect = el?.getBoundingClientRect?.() ?? { left: 0, bottom: 0 };
      if (!this.open({ clientX: rect.left, clientY: rect.bottom }, ws)) return false;
      state.keyboard = true;
      return true;
    },

    close() {
      state.show = false;
    },

    /** The chosen key and its workspace, or null for a disabled or absent item. */
    select(key) {
      const ws = state.workspace;
      state.show = false;
      const item = workspaceMenuItems(ws, workspaces()).find((i) => i.key === key);
      if (!ws || !item || item.disabled) return null;
      return { key, workspace: ws };
    },
  };
}
