import { computed, reactive } from 'vue';

/**
 * What a right-click on a task offers.
 *
 * There is one list, and both views that show tasks read it. Until now the items
 * were written inline in two places — `KanbanBoardView.vue` and `TaskFeed.vue`,
 * each with its own literal `[{ key: 'move', label: 'Move Task' }]` — which was
 * survivable while the list was a single hard-coded row and stops being
 * survivable the moment anything is added to it: an entry added to one and not
 * the other means right-click does different things on two views of the same
 * task, and nothing about that failure announces itself.
 *
 * ## Built-ins never move
 *
 * Our own items come first, extension items after a divider. So installing
 * something does not shuffle the entry somebody's hand already knows, and it
 * stays visible which rows came from where.
 *
 * ## An extension item has to earn its place on each task
 *
 * Every entry may carry a `when(task)` predicate. Without one, ten installed
 * extensions mean ten permanent rows on every task, and a menu that long is one
 * nobody reads. An entry that says nothing about when it applies is assumed to
 * always apply, which is right for the built-ins and rare for the rest.
 */

/** The items AgentRQ itself offers. First, always, and in this order. */
export const BUILT_IN_ITEMS = [{ key: 'move', label: 'Move Task' }];

/** What a divider looks like to `ContextMenu.vue`. */
export const DIVIDER = { key: '__divider', divider: true };

/**
 * Whether an extension's entry applies to this task.
 *
 * A predicate that throws is treated as "no". One extension deciding badly must
 * not empty a menu that other extensions are also in, and a thrown error is not
 * a reason to show a row whose own author could not say whether it belonged.
 */
export function appliesTo(entry, task) {
  if (typeof entry?.when !== 'function') return true;
  try {
    return Boolean(entry.when(task));
  } catch {
    return false;
  }
}

/**
 * The full item list for one task.
 *
 * Extension entries are namespaced on the way in. Two extensions may both want
 * a key called `create`, and the menu has to be able to tell them apart when one
 * is chosen — the owner is what makes that possible without asking authors to
 * invent globally unique names.
 */
export function menuItemsFor(task, extensionEntries = []) {
  const applicable = extensionEntries
    .filter((entry) => appliesTo(entry, task))
    .map((entry) => ({
      key: `ext:${entry.owner}:${entry.id}`,
      label: entry.label ?? entry.id,
      owner: entry.owner,
      id: entry.id,
    }));

  if (applicable.length === 0) return [...BUILT_IN_ITEMS];
  return [...BUILT_IN_ITEMS, DIVIDER, ...applicable];
}

/**
 * Reads a chosen key back into who should handle it.
 *
 * The prefix is what keeps a built-in and an extension entry from ever being
 * confused for one another, however an author names theirs.
 */
export function parseSelection(key) {
  const parts = String(key ?? '').split(':');
  if (parts[0] !== 'ext' || parts.length < 3) return { kind: 'builtin', key };
  return { kind: 'extension', owner: parts[1], id: parts.slice(2).join(':') };
}

/**
 * The menu's state, shared by every view that shows tasks.
 *
 * `entries` is a getter rather than a value so the menu follows what is
 * installed: an extension enabled while the board is open should appear on the
 * next right-click, not on the next reload. For that to hold, the getter has to
 * read something reactive — a store or a ref — which is what every real caller
 * does, and what a test has to do too if it means to prove it.
 */
export function useTaskContextMenu({ entries = () => [], onExtensionSelect = () => {} } = {}) {
  const state = reactive({ show: false, x: 0, y: 0, task: null });
  const menu = computed(() => state);

  return {
    menu,
    items: computed(() => menuItemsFor(state.task, entries())),

    open(event, task) {
      Object.assign(state, { show: true, x: event.clientX, y: event.clientY, task });
    },

    close() {
      state.show = false;
    },

    /**
     * Routes a chosen key, returning the built-in key when it is one of ours.
     *
     * The caller keeps its own switch for built-ins — those do things only the
     * view knows how to do — and extension entries are handed off here so no
     * view has to learn what an extension is.
     */
    select(key) {
      const task = state.task;
      state.show = false;
      if (!task) return null;

      const parsed = parseSelection(key);
      if (parsed.kind === 'builtin') return parsed.key;

      onExtensionSelect({ owner: parsed.owner, id: parsed.id, task });
      return null;
    },
  };
}
