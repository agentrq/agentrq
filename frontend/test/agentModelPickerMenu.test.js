// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createApp, h } from 'vue';

const api = vi.hoisted(() => ({ setAgentModel: vi.fn() }));
vi.mock('../src/api', () => api);

const { default: AgentModelPicker } = await import('../src/components/AgentModelPicker.vue');

const workspace = {
  id: 'ws1',
  agentConnected: true,
  agentModels: {
    configId: 'model',
    currentModel: 'a',
    canSet: true,
    models: [{ id: 'a', name: 'Model A' }, { id: 'b', name: 'Model B' }],
  },
};

describe('AgentModelPicker', () => {
  let app;
  let host;
  const settle = () => new Promise((r) => setTimeout(r, 0));
  const click = async (node) => {
    node.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
    await settle();
  };
  const menu = () => document.body.querySelector('div.fixed');

  beforeEach(async () => {
    api.setAgentModel.mockReset();
    host = document.createElement('div');
    document.body.appendChild(host);
    app = createApp({ render: () => h(AgentModelPicker, { workspace }) });
    app.mount(host);
    await settle();
    await click(host.querySelector('button'));
  });
  afterEach(() => {
    app.unmount();
    host.remove();
  });

  // #1346: the capturing scroll listener saw the menu's own scroll, so the
  // wheel or scrollbar closed a long list instead of scrolling it.
  it('stays open while its own list is scrolled', async () => {
    menu().dispatchEvent(new Event('scroll'));
    await settle();
    expect(menu()).not.toBeNull();
  });

  it('is driven by the keyboard: focus on the current model, arrows, Escape back to the button', async () => {
    const trigger = host.querySelector('button');
    const items = () => [...menu().querySelectorAll('[role="menuitemradio"]')];
    expect(items().map((b) => b.getAttribute('aria-checked'))).toEqual(['true', 'false']);
    expect(document.activeElement).toBe(items()[0]);

    menu().dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
    expect(document.activeElement).toBe(items()[1]);

    menu().dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    await settle();
    expect(menu()).toBeNull();
    expect(document.activeElement).toBe(trigger);

    // Down on the closed button opens it; Down again on the open one does not close it.
    trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true, cancelable: true }));
    await settle();
    expect(menu()).not.toBeNull();
    trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true, cancelable: true }));
    await settle();
    expect(menu()).not.toBeNull();

    // Picking hands focus back to the button.
    await click(items()[1]);
    expect(api.setAgentModel).toHaveBeenCalledWith('ws1', 'b');
    expect(menu()).toBeNull();
    expect(document.activeElement).toBe(trigger);
  });

  it('closes when the page scrolls or the window resizes', async () => {
    window.dispatchEvent(new Event('scroll'));
    await settle();
    expect(menu()).toBeNull();

    await click(host.querySelector('button'));
    window.dispatchEvent(new Event('resize'));
    await settle();
    expect(menu()).toBeNull();
  });
});
