// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, it, expect, vi } from 'vitest';
import { ref } from 'vue';

import {
  BUILT_IN_ITEMS,
  DIVIDER,
  appliesTo,
  menuItemsFor,
  parseSelection,
  useTaskContextMenu,
} from '../src/composables/useTaskContextMenu';

/**
 * There is one list now, and both views that show tasks read it. The item list
 * used to be written inline in KanbanBoardView and TaskFeed separately, which
 * was survivable for a single hard-coded row and stops being survivable the
 * moment anything is added: an entry in one and not the other means right-click
 * does different things on two views of the same task.
 */

const entry = (over = {}) => ({ owner: 'linear', id: 'create', label: 'Create Linear issue', ...over });
const task = { id: 't1', status: 'completed' };

describe('appliesTo', () => {
  it('shows an entry that says nothing about when it applies', () => {
    expect(appliesTo(entry(), task)).toBe(true);
  });

  it('asks the predicate when there is one', () => {
    expect(appliesTo(entry({ when: (t) => t.status === 'completed' }), task)).toBe(true);
    expect(appliesTo(entry({ when: (t) => t.status === 'ongoing' }), task)).toBe(false);
  });

  it('treats a predicate that throws as a no', () => {
    // One extension deciding badly must not empty a menu other extensions are
    // in, and a thrown error is not a reason to show a row whose own author
    // could not say whether it belonged.
    const thrower = entry({ when: () => { throw new Error('nope'); } });

    expect(appliesTo(thrower, task)).toBe(false);
  });
});

describe('menuItemsFor', () => {
  it('is just the built-ins when nothing is installed', () => {
    expect(menuItemsFor(task, [])).toEqual(BUILT_IN_ITEMS);
    expect(menuItemsFor(task)).toEqual(BUILT_IN_ITEMS);
  });

  it('puts extensions after a divider, with the built-ins unmoved', () => {
    // Installing something must not shuffle the entry somebody's hand knows.
    const items = menuItemsFor(task, [entry()]);

    expect(items[0]).toEqual(BUILT_IN_ITEMS[0]);
    expect(items[1]).toEqual(DIVIDER);
    expect(items[2].label).toBe('Create Linear issue');
  });

  it('adds no divider when every extension entry opted out', () => {
    // A trailing divider under nothing is a menu that looks broken.
    const items = menuItemsFor(task, [entry({ when: () => false })]);

    expect(items).toEqual(BUILT_IN_ITEMS);
  });

  it('namespaces keys, so two extensions may both call theirs "create"', () => {
    const items = menuItemsFor(task, [entry(), entry({ owner: 'standup' })]);

    expect(items[2].key).toBe('ext:linear:create');
    expect(items[3].key).toBe('ext:standup:create');
  });

  it('falls back to the id when an entry has no label', () => {
    expect(menuItemsFor(task, [entry({ label: undefined })])[2].label).toBe('create');
  });
});

describe('parseSelection', () => {
  it('reads a built-in as a built-in', () => {
    expect(parseSelection('move')).toEqual({ kind: 'builtin', key: 'move' });
  });

  it('reads an extension key back into who should handle it', () => {
    expect(parseSelection('ext:linear:create')).toEqual({
      kind: 'extension',
      owner: 'linear',
      id: 'create',
    });
  });

  it('keeps an id that contains a colon intact', () => {
    expect(parseSelection('ext:linear:issue:new').id).toBe('issue:new');
  });

  it('treats anything malformed as a built-in rather than guessing', () => {
    expect(parseSelection('ext:linear').kind).toBe('builtin');
    expect(parseSelection(undefined).kind).toBe('builtin');
  });
});

describe('useTaskContextMenu', () => {
  it('opens where the click was, on the task that was clicked', () => {
    const menu = useTaskContextMenu();

    menu.open({ clientX: 10, clientY: 20 }, task);

    expect(menu.menu.value).toMatchObject({ show: true, x: 10, y: 20, task });
  });

  it('follows what is installed rather than what was installed at mount', () => {
    // An extension enabled while the board is open should appear on the next
    // right-click, not the next reload.
    // Reading something reactive, which is what every real caller does.
    const installed = ref([]);
    const menu = useTaskContextMenu({ entries: () => installed.value });
    menu.open({ clientX: 0, clientY: 0 }, task);

    expect(menu.items.value).toHaveLength(1);

    installed.value = [entry()];
    expect(menu.items.value).toHaveLength(3);
  });

  it('hands a built-in back to the view that knows what to do with it', () => {
    const onExtensionSelect = vi.fn();
    const menu = useTaskContextMenu({ onExtensionSelect });
    menu.open({ clientX: 0, clientY: 0 }, task);

    expect(menu.select('move')).toBe('move');
    expect(onExtensionSelect).not.toHaveBeenCalled();
    expect(menu.menu.value.show).toBe(false);
  });

  it('routes an extension entry away, so no view learns what an extension is', () => {
    const onExtensionSelect = vi.fn();
    const menu = useTaskContextMenu({ entries: () => [entry()], onExtensionSelect });
    menu.open({ clientX: 0, clientY: 0 }, task);

    expect(menu.select('ext:linear:create')).toBeNull();
    expect(onExtensionSelect).toHaveBeenCalledWith({ owner: 'linear', id: 'create', task });
  });

  it('does nothing when nothing was open', () => {
    const menu = useTaskContextMenu();

    expect(menu.select('move')).toBeNull();
  });

  it('closes without choosing', () => {
    const menu = useTaskContextMenu();
    menu.open({ clientX: 0, clientY: 0 }, task);

    menu.close();

    expect(menu.menu.value.show).toBe(false);
  });

  it('works with nothing supplied at all', () => {
    // The state a view is in before any extension is installed.
    const menu = useTaskContextMenu();
    menu.open({ clientX: 0, clientY: 0 }, task);

    expect(menu.items.value).toEqual(BUILT_IN_ITEMS);
    expect(menu.select('ext:linear:create')).toBeNull();
  });
});
