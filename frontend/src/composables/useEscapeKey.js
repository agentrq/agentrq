// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { onMounted, onUnmounted } from 'vue';

/**
 * Run `close` when Escape is pressed anywhere on the page.
 *
 * On the window, not the element: a `@keydown.esc` on a modal's backdrop never
 * fires, because nothing inside it has focus. An Escape somebody else already
 * answered — the palette, a context menu, the shell closing an overlay — has
 * its default prevented and is left alone, so one press closes one thing.
 * `close` returns false when it had nothing to close, and the key goes on.
 *
 * @param {() => boolean | void} close
 * @param {{ target?: EventTarget, onMounted?: Function, onUnmounted?: Function }} [options]
 */
export function useEscapeKey(close, options = {}) {
  const {
    target = globalThis.window,
    onMounted: mount = onMounted,
    onUnmounted: unmount = onUnmounted,
  } = options;

  const onKeydown = (event) => {
    if (event.key !== 'Escape' || event.defaultPrevented || event.isComposing) return;
    if (close() === false) return;
    event.preventDefault();
  };

  mount(() => target?.addEventListener('keydown', onKeydown));
  unmount(() => target?.removeEventListener('keydown', onKeydown));

  return { onKeydown };
}
