// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The progress card the person pinned in a task, and the cards they folded.
 * A progress card is an agent's reply drawn as an A2UI card, never a question
 * asked as one: the agent updates it in place as it works, so the pinned one
 * stays current over the conversation. One at a time: pinning another
 * replaces it.
 *
 * Kept per task in this browser only: it is how this person reads the task,
 * not something the agent or a teammate needs to know.
 */
import { ref, watch } from 'vue';

const KEY = 'agentrq.cardPins.';

/** A push pin, drawn filled at 24×24. */
export const PIN_ICON = 'M16 3H8v2h1v6l-2 3v2h4.2v5h1.6v-5H17v-2l-2-3V5h1z';

/**
 * A folded card keeps its top, where a progress card has its title and
 * bar, and fades out the details below, closed by the card's own border.
 */
export const FOLDED_CARD = 'relative max-h-[5.75rem] overflow-hidden rounded-b-xl border-b border-gray-200 dark:border-zinc-800 '
  + 'after:absolute after:inset-x-px after:bottom-0 after:h-4 after:bg-linear-to-b after:from-transparent after:to-white dark:after:to-zinc-900';

/** Whether a message is a progress card: a reply's card, not a question's. */
export function isProgressCard(message) {
  return message?.metadata?.type === 'a2ui';
}

function read(storage, taskId) {
  try {
    const saved = JSON.parse(storage.getItem(KEY + taskId) || '{}');
    return {
      pinned: typeof saved.pinned === 'string' ? saved.pinned : null,
      collapsed: new Set(Array.isArray(saved.collapsed) ? saved.collapsed : []),
    };
  } catch {
    return { pinned: null, collapsed: new Set() };
  }
}

/**
 * @param {import('vue').Ref<string>} taskId
 * @param {Storage} [storage]
 */
export function useCardPins(taskId, storage = globalThis.localStorage) {
  const pinned = ref(null);
  const collapsed = ref(new Set());

  watch(taskId, (id) => {
    const saved = read(storage, id);
    pinned.value = saved.pinned;
    collapsed.value = saved.collapsed;
  }, { immediate: true });

  function save() {
    try {
      storage.setItem(KEY + taskId.value, JSON.stringify({ pinned: pinned.value, collapsed: [...collapsed.value] }));
    } catch {
      // Storage full or refused: the choice still holds until the page reloads.
    }
  }

  return {
    /** The pinned progress card among the task's messages, or null. */
    pinnedCard: (messages) => (messages || []).find((m) => m.id === pinned.value && isProgressCard(m)) || null,
    isPinned: (id) => pinned.value === id,
    togglePin(id) {
      pinned.value = pinned.value === id ? null : id;
      save();
    },
    /** `key` is a message id, or `pinned:` and one for the copy shown pinned. */
    isCollapsed: (key) => collapsed.value.has(key),
    toggleCollapsed(key) {
      const next = new Set(collapsed.value);
      if (!next.delete(key)) next.add(key);
      collapsed.value = next;
      save();
    },
  };
}
