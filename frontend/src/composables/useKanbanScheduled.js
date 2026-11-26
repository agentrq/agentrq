// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * Whether the board of every workspace (and a team's) shows its Scheduled
 * column; a workspace's own board always does. Off unless turned on, kept in
 * this browser: the toggle sits in the page header, the columns in the board.
 */

import { ref } from 'vue';

export const KANBAN_SCHEDULED_KEY = 'agentrq:kanban:scheduled';

function read(storage) {
  try {
    return storage?.getItem(KANBAN_SCHEDULED_KEY) === '1';
  } catch {
    return false;
  }
}

const showScheduled = ref(read(globalThis.localStorage));

export function useKanbanScheduled(storage = globalThis.localStorage) {
  function setShowScheduled(on) {
    showScheduled.value = !!on;
    try {
      if (on) storage?.setItem(KANBAN_SCHEDULED_KEY, '1');
      else storage?.removeItem(KANBAN_SCHEDULED_KEY);
    } catch {
      // Private mode or blocked storage: the column still follows the toggle until a reload.
    }
  }
  return {
    showScheduled,
    setShowScheduled,
    toggleScheduled: () => setShowScheduled(!showScheduled.value),
  };
}
