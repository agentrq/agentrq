// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi } from 'vitest';
import { ref } from 'vue';

import {
  BUILT_IN_ITEMS,
  useWorkspaceContextMenu,
  workspaceMenuItems,
} from '../src/composables/useWorkspaceContextMenu';

const parent = { id: 'p1', name: 'ops' };
const fork = { id: 'f1', name: 'ops fork', forkOfId: 'p1', forkOf: { id: 'p1', name: 'ops' } };

describe('workspaceMenuItems', () => {
  it('offers Fork on a workspace', () => {
    expect(workspaceMenuItems(parent)).toEqual(BUILT_IN_ITEMS);
    expect(BUILT_IN_ITEMS).toEqual([{ key: 'fork', label: 'Fork workspace' }]);
  });

  it('offers Merge instead of Fork on a fork, named for the parent', () => {
    expect(workspaceMenuItems(fork)).toEqual([{ key: 'merge', label: 'Merge into ops', disabled: false, detail: '' }]);
  });

  it('keeps Merge listed but disabled, with the reason, while a task is unfinished', () => {
    expect(workspaceMenuItems({ ...fork, unfinishedTasks: 2 })).toEqual([
      { key: 'merge', label: 'Merge into ops', disabled: true, detail: '2 tasks are not finished' },
    ]);
  });

  it('offers nothing on the supervisor or an archived workspace', () => {
    expect(workspaceMenuItems({ id: 's', name: 'supervisor' })).toEqual([]);
    expect(workspaceMenuItems({ ...parent, archivedAt: 'x' })).toEqual([]);
    expect(workspaceMenuItems(null)).toEqual([]);
  });
});

describe('useWorkspaceContextMenu', () => {
  it('opens at the pointer, refreshes on open, and follows the list', () => {
    const list = ref([parent, { ...fork, unfinishedTasks: 1 }]);
    const onOpen = vi.fn();
    const menu = useWorkspaceContextMenu({ workspaces: () => list.value, onOpen });

    expect(menu.open({ clientX: 10, clientY: 20 }, list.value[1])).toBe(true);
    expect(menu.menu.value).toMatchObject({ show: true, x: 10, y: 20, keyboard: false });
    expect(onOpen).toHaveBeenCalledWith(list.value[1]);
    expect(menu.items.value[0].disabled).toBe(true);
  });

  it('never opens an empty menu', () => {
    const menu = useWorkspaceContextMenu();
    expect(menu.open({ clientX: 0, clientY: 0 }, { id: 's', name: 'supervisor' })).toBe(false);
    expect(menu.menu.value.show).toBe(false);
    expect(menu.openAt(null, { id: 's', name: 'supervisor' })).toBe(false);
  });

  it('opens under the element from the keyboard, and asks for the focus', () => {
    const menu = useWorkspaceContextMenu();
    const el = { getBoundingClientRect: () => ({ left: 5, bottom: 40 }) };
    expect(menu.openAt(el, parent)).toBe(true);
    expect(menu.menu.value).toMatchObject({ show: true, x: 5, y: 40, keyboard: true });
    menu.close();
    expect(menu.menu.value.show).toBe(false);
    expect(menu.openAt(null, parent)).toBe(true);
    expect(menu.menu.value).toMatchObject({ x: 0, y: 0 });
  });

  it('hands back the chosen key with its workspace, and nothing for a disabled item', () => {
    const menu = useWorkspaceContextMenu();
    menu.open({ clientX: 0, clientY: 0 }, parent);
    expect(menu.select('fork')).toEqual({ key: 'fork', workspace: parent });
    expect(menu.menu.value.show).toBe(false);

    menu.open({ clientX: 0, clientY: 0 }, { ...fork, unfinishedTasks: 3 });
    expect(menu.select('merge')).toBe(null);
    menu.open({ clientX: 0, clientY: 0 }, fork);
    expect(menu.select('fork')).toBe(null);
    expect(useWorkspaceContextMenu().select('fork')).toBe(null);
  });
});
