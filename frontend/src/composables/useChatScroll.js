import { nextTick } from 'vue';

/**
 * Pin a chat pane to its newest message.
 *
 * The one rule worth extracting: **the container is looked up inside the
 * scheduled callback, never before it.**
 *
 * The chat pane is rendered behind a `v-if`, so leaving it for the History view
 * destroys the scroll container and the template ref becomes null. Coming back
 * creates a brand new element, scrolled to the top. At the instant the view
 * switches the ref is still null — the element does not exist until Vue has
 * rendered — so a guard placed *before* the wait concludes "no container,
 * nothing to do" and returns. The pane then opens at the oldest message, which
 * is exactly the bug this fixes.
 *
 * `schedule` is injectable so the timing can be tested without a component.
 *
 * @param {() => HTMLElement | null | undefined} getContainer
 * @param {(fn: () => void) => unknown} [schedule]
 */
export function scrollToBottom(getContainer, schedule = nextTick) {
  return schedule(() => {
    const el = getContainer();
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  });
}

/**
 * Whether switching to `view` should drop the reader at the newest message.
 *
 * Only the chat pane scrolls: History keeps its own position, and returning to
 * a timeline where you had scrolled to a particular tool call only to be thrown
 * to the end would lose your place.
 *
 * @param {string} view
 */
export function shouldScrollOnViewChange(view) {
  return view === 'chat';
}
