// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { computed, ref } from 'vue';
import { useToasts } from './useToasts';

/**
 * Whether the app has drawn something over the page: a menu, a dialog or a toast.
 *
 * A guest that is a native view sits above the whole page, so anything the app
 * draws where it is would vanish behind it — the Tauri side panel hides or
 * shrinks its guest while this is true. Electron's `<webview>` is part of the
 * page and ignores it.
 *
 * Menus and dialogs are found rather than registered: every one the app has
 * carries `role="dialog"` or `role="menu"`, so a new one is covered without
 * anybody remembering to say so. Toasts come from the toast list.
 */

const SELECTOR = '[role="dialog"],[role="menu"]';

const shown = ref(false);
const { toasts } = useToasts();

/** True while any app menu, dialog or toast is open. */
export const overlayOpen = computed(() => shown.value || toasts.value.length > 0);

/** Whether a node that came or went is, or holds, a menu or a dialog. */
function concerns(node) {
  return node.nodeType === 1 && (node.matches(SELECTOR) || Boolean(node.querySelector(SELECTOR)));
}

/**
 * Start watching `root` for menus and dialogs.
 *
 * @returns {() => void} stop
 */
export function watchOverlays(root = document.body) {
  const scan = () => { shown.value = Boolean(root.querySelector(SELECTOR)); };
  const observer = new MutationObserver((records) => {
    const changed = records.some((record) => (
      record.type === 'attributes'
      || [...record.addedNodes, ...record.removedNodes].some(concerns)
    ));
    if (changed) scan();
  });
  scan();
  observer.observe(root, { childList: true, subtree: true, attributes: true, attributeFilter: ['role'] });
  return () => {
    observer.disconnect();
    shown.value = false;
  };
}

export function useOverlayOpen() {
  return overlayOpen;
}
