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

  it('shows the full name of the chosen model on hover, and wraps long names', () => {
    expect(host.querySelector('button').getAttribute('title')).toBe('Model A');
    expect(menu().querySelector('[role="menuitemradio"] span').className).toContain('wrap-anywhere');
    // A short list has no search box.
    expect(menu().querySelector('input')).toBeNull();
  });
});

describe('AgentModelPicker with a long list', () => {
  let app;
  let host;
  const settle = () => new Promise((r) => setTimeout(r, 0));
  const menu = () => document.body.querySelector('div.fixed');
  const items = () => [...menu().querySelectorAll('[role="menuitemradio"]')];
  const type = async (input, value) => {
    input.value = value;
    input.dispatchEvent(new Event('input', { bubbles: true }));
    await settle();
  };
  const key = async (node, k) => {
    const event = new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true });
    node.dispatchEvent(event);
    await settle();
    return event;
  };
  const models = Array.from({ length: 12 }, (_, i) => ({ id: `provider/model-${i}`, name: `Model ${i}`, group: i < 6 ? 'Alpha' : 'Beta' }));

  beforeEach(async () => {
    api.setAgentModel.mockReset();
    api.setAgentModel.mockResolvedValue({});
    host = document.createElement('div');
    document.body.appendChild(host);
    const ws = { ...workspace, agentModels: { ...workspace.agentModels, currentModel: 'provider/model-0', models } };
    app = createApp({ render: () => h(AgentModelPicker, { workspace: ws }) });
    app.mount(host);
    await settle();
    host.querySelector('button').dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await settle();
  });
  afterEach(() => {
    app.unmount();
    host.remove();
  });

  it('opens with the search box focused and filters as you type', async () => {
    const input = menu().querySelector('input');
    expect(document.activeElement).toBe(input);
    expect(items()).toHaveLength(12);

    await type(input, 'model-1');
    expect(items().map((b) => b.textContent.trim())).toEqual(['Model 1', 'Model 10', 'Model 11']);

    await type(input, 'beta');
    expect(items()).toHaveLength(6);

    await type(input, 'nothing');
    expect(items()).toHaveLength(0);
    expect(menu().textContent).toContain('No match');
    // Enter with no match picks nothing.
    await key(input, 'Enter');
    expect(api.setAgentModel).not.toHaveBeenCalled();
    expect(menu()).not.toBeNull();
  });

  it('lets letters be typed, arrows reach the list, and Enter pick the first match', async () => {
    const input = menu().querySelector('input');
    const typed = await key(input, 'm');
    expect(typed.defaultPrevented).toBe(false);
    expect(document.activeElement).toBe(input);

    await key(input, 'ArrowDown');
    expect(document.activeElement).toBe(items()[0]);

    input.focus();
    await type(input, 'model-11');
    await key(input, 'Enter');
    expect(api.setAgentModel).toHaveBeenCalledWith('ws1', 'provider/model-11');
    expect(menu()).toBeNull();
  });

  it('closes on Escape from the search box and starts empty next time', async () => {
    const input = menu().querySelector('input');
    await type(input, 'beta');
    await key(input, 'Escape');
    expect(menu()).toBeNull();

    host.querySelector('button').dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await settle();
    expect(menu().querySelector('input').value).toBe('');
    expect(items()).toHaveLength(12);
  });
});
