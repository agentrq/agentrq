// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { ref } from 'vue';

import { TYPEAHEAD_RESET_MS, useMenuKeyboard } from '../src/composables/useMenuKeyboard';

describe('useMenuKeyboard', () => {
  let menu;
  let close;
  let clock;
  let keys;

  function build(names, { checked, disabled } = {}) {
    menu.value.innerHTML = names
      .map((n) => `<button${n === checked ? ' aria-checked="true"' : ' aria-checked="false"'}${n === disabled ? ' disabled' : ''}>${n}</button>`)
      .join('');
  }
  const focused = () => document.activeElement?.textContent;
  const press = (key, mods = {}) => {
    const event = new KeyboardEvent('keydown', { key, cancelable: true, ...mods });
    keys.onKeydown(event);
    return event;
  };

  beforeEach(() => {
    menu = ref(document.createElement('div'));
    document.body.appendChild(menu.value);
    close = vi.fn();
    clock = { t: 0 };
    keys = useMenuKeyboard({ menu, close, now: () => clock.t });
  });
  afterEach(() => menu.value.remove());

  it('starts on the checked item, else the first, and does nothing with no items', () => {
    build(['Alpha', 'Beta', 'Gamma'], { checked: 'Beta' });
    keys.focusChecked();
    expect(focused()).toBe('Beta');

    build(['Alpha', 'Beta']);
    keys.focusChecked();
    expect(focused()).toBe('Alpha');

    build([]);
    expect(() => keys.focusChecked()).not.toThrow();
    expect(() => useMenuKeyboard({ menu: ref(null), close }).focusChecked()).not.toThrow();
  });

  it('moves with Up and Down, wrapping, skipping disabled items', () => {
    build(['Alpha', 'Beta', 'Gamma'], { disabled: 'Beta' });
    expect(press('ArrowDown').defaultPrevented).toBe(true);
    expect(focused()).toBe('Alpha');
    press('ArrowDown');
    expect(focused()).toBe('Gamma');
    press('ArrowDown');
    expect(focused()).toBe('Alpha');
    press('ArrowUp');
    expect(focused()).toBe('Gamma');
    press('ArrowUp');
    expect(focused()).toBe('Alpha');
  });

  it('starts Up from the bottom when nothing is focused', () => {
    build(['Alpha', 'Beta', 'Gamma']);
    document.body.focus();
    press('ArrowUp');
    expect(focused()).toBe('Gamma');
  });

  it('jumps to the ends with Home and End', () => {
    build(['Alpha', 'Beta', 'Gamma']);
    press('End');
    expect(focused()).toBe('Gamma');
    press('Home');
    expect(focused()).toBe('Alpha');
  });

  it('closes on Escape, and on Tab without stopping the Tab', () => {
    build(['Alpha']);
    expect(press('Escape').defaultPrevented).toBe(true);
    expect(close).toHaveBeenCalledTimes(1);
    expect(press('Tab').defaultPrevented).toBe(false);
    expect(close).toHaveBeenCalledTimes(2);
  });

  it('jumps to an item by what is typed', () => {
    build(['Claude Opus', 'Claude Sonnet', 'Gemini Pro', 'GPT']);
    press('g');
    expect(focused()).toBe('Gemini Pro');
    // A repeated letter walks through the items starting with it.
    press('g');
    expect(focused()).toBe('GPT');
    press('g');
    expect(focused()).toBe('Gemini Pro');

    // A pause starts over; a longer prefix may stay on the current item.
    clock.t += TYPEAHEAD_RESET_MS + 1;
    press('c');
    expect(focused()).toBe('Claude Opus');
    press('l');
    press('a');
    expect(focused()).toBe('Claude Opus');
    press('u');
    press('d');
    press('e');
    press(' ');
    press('s');
    expect(focused()).toBe('Claude Sonnet');

    // Another key ends the search, even without a pause.
    press('Home');
    press('g');
    press('p');
    expect(focused()).toBe('GPT');
  });

  it('leaves a key alone when nothing matches, or it is not a letter, or a modifier is held', () => {
    build(['Alpha', 'Beta']);
    keys.focusChecked();
    expect(press('z').defaultPrevented).toBe(false);
    expect(press('Enter').defaultPrevented).toBe(false);
    clock.t += TYPEAHEAD_RESET_MS + 1;
    expect(press('b', { ctrlKey: true }).defaultPrevented).toBe(false);
    expect(press('b', { metaKey: true }).defaultPrevented).toBe(false);
    expect(press('b', { altKey: true }).defaultPrevented).toBe(false);
    expect(focused()).toBe('Alpha');
  });

  it('uses the real clock by default', () => {
    const real = useMenuKeyboard({ menu, close });
    build(['Alpha', 'Beta']);
    const event = new KeyboardEvent('keydown', { key: 'b', cancelable: true });
    real.onKeydown(event);
    expect(focused()).toBe('Beta');
  });
});
