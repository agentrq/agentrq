// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { ref } from 'vue';

const toasts = ref([]);

// An error shows up to four lines and takes longer to read, so it stays longer.
// A stream toast carries a task, a status and a sentence, so it gets a little
// longer than a plain one.
const DURATION = 4000;
const EVENT_DURATION = 6000;
const ERROR_DURATION = 20000;

// A burst of status changes must not bury the screen; the oldest go first.
const MAX_VISIBLE = 4;

// A counter, not Date.now() + Math.random(): near today's timestamp a double
// holds only ~4096 fractions per millisecond, so two toasts in one millisecond
// could share an id, and dismissing one removed both.
let nextId = 0;

export function useToasts() {
  // `link`, when given, is `{ taskId, workspaceId }` — where clicking the
  // toast (not its close button) should navigate. Falsy ids mean no link.
  //
  // `card`, when given, is what a stream toast adds: `kind` (which status icon
  // and pill to show), `eyebrow` (the workspace it happened in) and, for a
  // status change, `from`/`to`.
  const addToast = (message, type = 'info', title = null, duration = null, link = null, card = null) => {
    if (duration === null) duration = type === 'error' ? ERROR_DURATION : card ? EVENT_DURATION : DURATION;
    const id = ++nextId;
    // A zero duration means the toast waits to be dismissed; the progress bar
    // is a countdown, so it has nothing to show.
    const toast = { id, message, type, title, duration, persistent: duration <= 0, link, ...card };

    toasts.value = [...toasts.value, toast].slice(-MAX_VISIBLE);

    if (duration > 0) {
      setTimeout(() => {
        removeToast(id);
      }, duration);
    }
    return id;
  };

  const removeToast = (id) => {
    toasts.value = toasts.value.filter(t => t.id !== id);
  };

  // No default title: the icon already says error, success or info.
  const notifyError = (message, title = null, link = null) => addToast(message, 'error', title, null, link);
  const notifySuccess = (message, title = null, link = null) => addToast(message, 'success', title, null, link);
  const notifyInfo = (message, title = null, link = null) => addToast(message, 'info', title, null, link);

  // A toast decided by useStreamToasts, shown as a task card.
  const notifyEvent = ({ tone, kind, title, message, from, to, taskId, workspaceId }, eyebrow = '') => addToast(
    message,
    tone,
    title || null,
    null,
    taskId && workspaceId ? { taskId, workspaceId } : null,
    { kind, eyebrow, from, to },
  );

  return {
    toasts,
    addToast,
    removeToast,
    notifyError,
    notifySuccess,
    notifyInfo,
    notifyEvent
  };
}
