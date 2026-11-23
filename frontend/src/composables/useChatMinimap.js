// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { ref, reactive, computed, watch, onUnmounted, toValue } from 'vue';

// The longest preview kept; the hover card clamps it to two lines anyway.
const PREVIEW_CHARS = 280;
// The dots drawn either side of the one being read, so a long thread never
// runs off the pane.
export const MINIMAP_REACH = 6;
// The fewest messages worth a minimap; it also waits for the chat to scroll,
// as a chat that fits on the screen has nowhere to jump to.
export const MINIMAP_MIN = 3;

/**
 * The messages the person sent, as minimap entries. Messages not yet sent
 * (counting down or queued) are left out: they sit at the bottom, by the
 * composer, so there is nothing to jump to.
 *
 * @param {Array<object>} thread
 * @returns {Array<{id: string, n: number, preview: string}>}
 */
export function minimapEntries(thread) {
  return (thread || [])
    .filter((m) => m.sender === 'human' && !m._pending && !m._queued)
    .map((m, i) => ({
      id: String(m.id),
      n: i + 1,
      preview: String(m.text || '').replace(/\s+/g, ' ').trim().slice(0, PREVIEW_CHARS),
    }));
}

/**
 * The dots to draw: `reach` either side of the message being read, sliding
 * with it. Near either end the window keeps its size by reaching further the
 * other way; with nothing read yet it ends at the newest message.
 *
 * @param {Array<{id: string}>} entries
 * @param {string | null} activeId
 * @param {number} [reach]
 */
export function minimapWindow(entries, activeId, reach = MINIMAP_REACH) {
  const size = 2 * reach + 1;
  const found = entries.findIndex((e) => e.id === activeId);
  const at = found < 0 ? entries.length - 1 : found;
  const start = Math.max(0, Math.min(at - reach, entries.length - size));
  return entries.slice(start, start + size);
}

/**
 * The marked message being read: the last one whose top has scrolled past the
 * upper third of the pane, or the last of all once the pane is at its bottom.
 *
 * @param {HTMLElement | null | undefined} container
 * @returns {string | null}
 */
export function activeMessageId(container) {
  if (!container) return null;
  const marks = [...container.querySelectorAll('[data-minimap-id]')];
  if (!marks.length) return null;
  if (container.scrollTop + container.clientHeight >= container.scrollHeight - 2) {
    return marks[marks.length - 1].dataset.minimapId;
  }
  const box = container.getBoundingClientRect();
  const line = box.top + container.clientHeight / 3;
  let active = marks[0];
  for (const el of marks) {
    if (el.getBoundingClientRect().top > line) break;
    active = el;
  }
  return active.dataset.minimapId;
}

/**
 * Tracks which of the person's messages is in view, whether the minimap is
 * worth showing, and scrolls to a message.
 *
 * The container is behind a `v-if` (chat vs trajectory), so the listeners
 * follow the ref rather than being added once. Resizing the window can make
 * the chat fit, or stop fitting, without a scroll, hence the ResizeObserver.
 *
 * @param {import('vue').Ref<HTMLElement | null>} containerRef
 * @param {import('vue').Ref<Array<{id: string}>>} entriesRef
 * @param {(fn: () => void) => unknown} [schedule]
 */
export function useChatMinimap(containerRef, entriesRef, schedule = (fn) => requestAnimationFrame(fn)) {
  const activeId = ref(null);
  const overflows = ref(false);
  const shown = computed(() => overflows.value && entriesRef.value.length >= MINIMAP_MIN);
  let queued = false;

  const update = () => {
    queued = false;
    const el = containerRef.value;
    activeId.value = activeMessageId(el);
    overflows.value = !!el && el.scrollHeight > el.clientHeight + 1;
  };
  const onScroll = () => {
    if (queued) return;
    queued = true;
    schedule(update);
  };

  const resize = typeof ResizeObserver === 'function' ? new ResizeObserver(onScroll) : null;

  watch(containerRef, (el, old) => {
    old?.removeEventListener('scroll', onScroll);
    if (old) resize?.unobserve(old);
    el?.addEventListener('scroll', onScroll, { passive: true });
    if (el) resize?.observe(el);
    onScroll();
  }, { immediate: true });
  watch(() => entriesRef.value.map((e) => e.id).join(','), onScroll);

  onUnmounted(() => {
    containerRef.value?.removeEventListener('scroll', onScroll);
    resize?.disconnect();
  });

  const jumpTo = (id) => {
    const el = [...(containerRef.value?.querySelectorAll('[data-minimap-id]') || [])]
      .find((m) => m.dataset.minimapId === id);
    if (!el) return;
    el.scrollIntoView({ behavior: 'smooth', block: 'start' });
    activeId.value = id;
  };

  return { activeId, shown, jumpTo };
}

// The tasks whose autoscroll was paused from the minimap. Kept for the life of
// the tab, not stored: a pause is for reading back through one conversation.
const pausedTasks = reactive(new Set());

/**
 * Whether new messages may pull this task's chat to the bottom, and the switch
 * the minimap offers to pause that while the person reads up. Jumping to a
 * message pauses it too, until the person resumes.
 *
 * @param {import('vue').MaybeRefOrGetter<string>} taskId
 * @param {() => void} [onPause] called each time a pause begins
 */
export function useAutoscrollPause(taskId, onPause = () => {}) {
  const paused = computed(() => pausedTasks.has(String(toValue(taskId))));
  const pause = () => {
    if (paused.value) return;
    pausedTasks.add(String(toValue(taskId)));
    onPause();
  };
  const toggle = () => {
    if (!pausedTasks.delete(String(toValue(taskId)))) pause();
  };
  return { paused, pause, toggle };
}

/** Forgets every pause; for tests. */
export function resetAutoscrollPauses() {
  pausedTasks.clear();
}
