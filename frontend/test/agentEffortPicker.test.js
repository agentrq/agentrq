// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createApp, h, nextTick, ref } from 'vue';

const api = vi.hoisted(() => ({ setAgentEffort: vi.fn() }));
vi.mock('../src/api', () => api);

const { canChooseEffort, effortName, useAgentEffortPicker } = await import('../src/composables/useAgentEffortPicker');
const { CONFIRMATION_TIMEOUT_MS } = await import('../src/composables/useAgentModelPicker');
const { useToasts } = await import('../src/composables/useToasts');
const { default: AgentEffortPicker } = await import('../src/components/AgentEffortPicker.vue');

const levels = {
  configId: 'effort',
  currentEffort: 'high',
  canSet: true,
  efforts: [{ id: 'low', name: 'Low', description: 'fastest' }, { id: 'high', name: 'High' }, { id: '' }, null],
};
const connected = (agentEffort = levels) => ({ id: 'ws1', agentConnected: true, agentEffort });

describe('canChooseEffort', () => {
  it('needs a connected agent that offers levels and will act on a choice', () => {
    expect(canChooseEffort(connected())).toBe(true);
    expect(canChooseEffort(null)).toBe(false);
    expect(canChooseEffort({ ...connected(), agentConnected: false })).toBe(false);
    expect(canChooseEffort(connected(null))).toBe(false);
    expect(canChooseEffort(connected({ ...levels, canSet: false }))).toBe(false);
    expect(canChooseEffort(connected({ ...levels, efforts: [] }))).toBe(false);
    expect(canChooseEffort(connected({ ...levels, efforts: null }))).toBe(false);
  });
});

describe('effortName', () => {
  it('names a level by the agent’s name, else its id', () => {
    expect(effortName(levels, 'low')).toBe('Low');
    expect(effortName(levels, 'max')).toBe('max');
    expect(effortName(undefined, '')).toBe('');
  });
});

describe('useAgentEffortPicker', () => {
  function harness({ selectEffort = vi.fn().mockResolvedValue({}), clock = { t: 0 } } = {}) {
    const workspace = ref(connected());
    const picker = useAgentEffortPicker({ workspace: () => workspace.value, selectEffort, now: () => clock.t });
    const agentReports = async (agentEffort) => {
      workspace.value = connected(agentEffort);
      await nextTick();
    };
    return { picker, workspace, selectEffort, clock, agentReports };
  }

  it('lists only the offered levels and shows the agent’s current one', () => {
    const { picker } = harness();
    expect(picker.available.value).toBe(true);
    expect(picker.efforts.value.map((e) => e.id)).toEqual(['low', 'high']);
    expect(picker.selectedId.value).toBe('high');
    expect(picker.selectedName.value).toBe('High');
  });

  it('shows the asked-for level until the agent agrees', async () => {
    const { picker, selectEffort, agentReports } = harness();
    await picker.choose('low');
    expect(selectEffort).toHaveBeenCalledWith('low');
    expect(picker.isPending.value).toBe(true);
    expect(picker.selectedName.value).toBe('Low');

    await agentReports({ ...levels, currentEffort: 'low' });
    expect(picker.isPending.value).toBe(false);
    expect(picker.error.value).toBe('');
  });

  it('says what the agent runs at when it reports another level', async () => {
    const { picker, agentReports } = harness();
    await picker.choose('low');
    await agentReports({ ...levels });
    expect(picker.isPending.value).toBe(false);
    expect(picker.error.value).toBe('The agent is running at High');
  });

  it('hides when a model switch takes the levels away, and settles a pending choice quietly', async () => {
    const { picker, agentReports } = harness();
    await picker.choose('low');
    await agentReports(null);
    expect(picker.available.value).toBe(false);
    expect(picker.efforts.value).toEqual([]);
    expect(picker.isPending.value).toBe(false);
    expect(picker.error.value).toBe('');
  });

  it('selects nothing when the agent names no current level', async () => {
    const { picker, agentReports } = harness();
    await agentReports({ ...levels, currentEffort: '' });
    expect(picker.selectedId.value).toBe('');
    expect(picker.selectedName.value).toBe('');
  });

  it('ignores reports while nothing is pending', async () => {
    const { picker, agentReports } = harness();
    await agentReports({ ...levels, currentEffort: 'low' });
    expect(picker.error.value).toBe('');
  });

  it('asks for nothing when the level is current or empty, and calls off a pending one', async () => {
    const { picker, selectEffort } = harness();
    await picker.choose('');
    await picker.choose('high');
    expect(selectEffort).not.toHaveBeenCalled();

    selectEffort.mockImplementation(() => new Promise(() => {}));
    picker.choose('low');
    await nextTick();
    expect(picker.sending.value).toBe(true);
    await picker.choose('high');
    expect(picker.isPending.value).toBe(false);
  });

  it('gives up with the server’s reason when it refuses', async () => {
    const { picker } = harness({ selectEffort: vi.fn().mockRejectedValue(new Error('the connected agent does not offer that reasoning level')) });
    await picker.choose('low');
    expect(picker.isPending.value).toBe(false);
    expect(picker.sending.value).toBe(false);
    expect(picker.error.value).toBe('the connected agent does not offer that reasoning level');

    const quiet = harness({ selectEffort: vi.fn().mockRejectedValue({}) });
    await quiet.picker.choose('low');
    expect(quiet.picker.error.value).toBe('Could not change the reasoning level');
  });

  it('gives up on a choice nothing confirmed, only once the time is up', async () => {
    const clock = { t: 0 };
    const { picker } = harness({ clock });
    picker.expirePending();
    await picker.choose('low');
    clock.t = CONFIRMATION_TIMEOUT_MS - 1;
    picker.expirePending();
    expect(picker.isPending.value).toBe(true);
    clock.t = CONFIRMATION_TIMEOUT_MS;
    picker.expirePending();
    expect(picker.isPending.value).toBe(false);
    expect(picker.error.value).toBe('The agent did not confirm the change');
  });
});

describe('AgentEffortPicker', () => {
  let app;
  let host;
  const settle = () => new Promise((r) => setTimeout(r, 0));
  const click = async (node) => {
    node.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
    await settle();
  };
  const menu = () => document.body.querySelector('div.fixed');

  function mount(workspace, compact = false) {
    host = document.createElement('div');
    document.body.appendChild(host);
    app = createApp({ render: () => h(AgentEffortPicker, { workspace: workspace.value, compact }) });
    app.mount(host);
  }

  beforeEach(() => {
    api.setAgentEffort.mockReset();
    api.setAgentEffort.mockResolvedValue({ requested: 'low' });
  });
  afterEach(() => {
    app?.unmount();
    host?.remove();
    app = undefined;
  });

  it('is absent when the agent offers no levels', async () => {
    mount(ref(connected(null)));
    await settle();
    expect(host.querySelector('button')).toBeNull();
  });

  it('opens on the offered levels and asks the agent for the one picked', async () => {
    mount(ref(connected()));
    await settle();
    const trigger = host.querySelector('button');
    expect(trigger.textContent).toContain('High');

    await click(trigger);
    const items = [...menu().querySelectorAll('button')];
    expect(items.map((b) => b.textContent.trim())).toEqual(['Low', 'High●']);
    expect(items[0].title).toBe('fastest');

    await click(items[0]);
    expect(api.setAgentEffort).toHaveBeenCalledWith('ws1', 'low');
    expect(menu()).toBeNull();
    expect(host.querySelector('button').textContent).toContain('Low…');
  });

  it('closes on a click outside, a scroll, or a second click, and stays open for a click inside', async () => {
    mount(ref(connected()));
    await settle();
    const trigger = host.querySelector('button');
    await click(trigger);
    await click(menu().querySelector('h3'));
    expect(menu()).not.toBeNull();
    await click(document.body);
    expect(menu()).toBeNull();
    await click(document.body);

    await click(trigger);
    window.dispatchEvent(new Event('scroll'));
    await settle();
    expect(menu()).toBeNull();

    await click(trigger);
    await click(trigger);
    expect(menu()).toBeNull();
  });

  it('draws as plain text on a workspace card', async () => {
    mount(ref(connected()), true);
    await settle();
    expect(host.querySelector('svg')).toBeNull();
    expect(host.querySelector('button').textContent.trim()).toBe('High');
  });

  it('toasts a refusal', async () => {
    api.setAgentEffort.mockRejectedValue(new Error('the connected agent could not be reached'));
    mount(ref(connected()));
    await settle();
    await click(host.querySelector('button'));
    await click(menu().querySelector('button'));
    expect(useToasts().toasts.value.at(-1).message).toBe('the connected agent could not be reached');
  });

  it('checks the pending choice on a clock', async () => {
    vi.useFakeTimers();
    try {
      mount(ref(connected()));
      await vi.advanceTimersByTimeAsync(1000);
    } finally {
      vi.useRealTimers();
    }
    expect(host.querySelector('button')).not.toBeNull();
  });
});
