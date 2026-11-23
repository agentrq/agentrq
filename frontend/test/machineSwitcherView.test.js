// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The `M` overlay, mounted: it fetches on open, lists, and opens a machine.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createApp, h, ref } from 'vue';

const push = vi.fn();
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }));

const fetchMachines = vi.fn();
vi.mock('../src/api', () => ({ fetchMachines: (...args) => fetchMachines(...args) }));

import MachineSwitcher from '../src/components/MachineSwitcher.vue';

const MACHINES = [
  { id: 'm1', name: 'build', online: true, enabled: true, sessions: 1 },
  { id: 'm2', name: 'laptop', online: false, enabled: false, sessions: 0 },
];

const tick = () => new Promise((r) => setTimeout(r, 30));

let app;
let el;
const show = ref(false);
const onClose = vi.fn();

function mount(currentMachineId = '') {
  el = document.createElement('div');
  document.body.appendChild(el);
  app = createApp({
    render: () => h(MachineSwitcher, { show: show.value, currentMachineId, onClose }),
  });
  app.mount(el);
}

beforeEach(() => {
  app?.unmount();
  el?.remove();
  show.value = false;
  push.mockReset();
  onClose.mockReset();
  fetchMachines.mockReset();
  fetchMachines.mockResolvedValue({ machines: MACHINES });
});

describe('MachineSwitcher', () => {
  it('fetches nothing until it is opened', async () => {
    mount();
    await tick();
    expect(fetchMachines).not.toHaveBeenCalled();
    expect(el.querySelector('[role="dialog"]')).toBeNull();
  });

  it('lists the machines on open and marks the one on screen', async () => {
    mount('m1');
    show.value = true;
    await tick();

    expect(fetchMachines).toHaveBeenCalledOnce();
    const rows = [...el.querySelectorAll('li')].map((li) =>
      [...li.querySelectorAll('span')].map((s) => s.textContent.trim()).filter(Boolean).join(' ')
    );
    expect(rows).toEqual(['build 1 agent Current', 'laptop 0 agents Off']);
    expect(document.activeElement).toBe(el.querySelector('input'));
  });

  it('opens the highlighted machine on Enter, after moving down', async () => {
    mount();
    show.value = true;
    await tick();

    const input = el.querySelector('input');
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown' }));
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }));

    expect(onClose).toHaveBeenCalled();
    expect(push).toHaveBeenCalledWith('/machines/m2');
  });

  it('filters by the query and says when nothing matches', async () => {
    mount();
    show.value = true;
    await tick();

    const input = el.querySelector('input');
    input.value = 'zzz';
    input.dispatchEvent(new Event('input'));
    await tick();

    expect(el.textContent).toContain('No machine matches');
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }));
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown' }));
    expect(push).not.toHaveBeenCalled();
  });

  it('points somebody with no machines at the machines page', async () => {
    fetchMachines.mockResolvedValue({ machines: [] });
    mount();
    show.value = true;
    await tick();

    expect(el.textContent).toContain('No machines yet.');
    el.querySelector('div.text-center button').click();
    expect(push).toHaveBeenCalledWith('/machines');
  });

  it('shows why the list could not be loaded', async () => {
    fetchMachines.mockRejectedValue(new Error('Failed to fetch machines'));
    mount();
    show.value = true;
    await tick();

    expect(el.textContent).toContain('Failed to fetch machines');
  });

  it('closes on Escape', async () => {
    mount();
    show.value = true;
    await tick();

    el.querySelector('input').dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    expect(onClose).toHaveBeenCalled();
    expect(push).not.toHaveBeenCalled();
  });
});
