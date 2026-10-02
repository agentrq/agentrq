// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { onMounted, onUnmounted, ref } from 'vue';

/**
 * Whether a view has a phone's width, measured on the view rather than the window.
 *
 * The window is the wrong thing to ask on the desktop: beside the side panel a
 * view can be 480px wide in a 1600px window, and laid out for the window it
 * shows a task list and a task squeezed side by side into room for one. This
 * asks the element instead, so a view beside the panel looks the way it does
 * on a phone. Pair it with Tailwind's container variants (`@min-[48rem]:`) on
 * an `@container` root, which are the same question asked in CSS.
 */

/** Tailwind's `md`, which is where the app's views stop being a phone's. */
export const NARROW_BELOW = 768;

/** A width of 0 is an element not laid out yet, which is not a narrow one. */
export function isNarrowWidth(width, below = NARROW_BELOW) {
  return width > 0 && width < below;
}

/**
 * @param {import('vue').Ref<HTMLElement|null>} elementRef
 * @param {{ below?: number, Observer?: typeof ResizeObserver }} [options]
 * @returns {import('vue').Ref<boolean>}
 */
export function useNarrowLayout(elementRef, { below = NARROW_BELOW, Observer = globalThis.ResizeObserver } = {}) {
  const narrow = ref(false);
  let observer = null;

  onMounted(() => {
    const element = elementRef.value;
    if (!element || !Observer) return;
    narrow.value = isNarrowWidth(element.clientWidth, below);
    observer = new Observer((entries) => {
      const width = entries?.[0]?.contentRect?.width ?? element.clientWidth;
      narrow.value = isNarrowWidth(width, below);
    });
    observer.observe(element);
  });

  onUnmounted(() => observer?.disconnect());

  return narrow;
}
