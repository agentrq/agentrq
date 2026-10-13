// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The shared reply box and the strip under it, on their own. The task page and
 * the Pano's chat are tested with them in place.
 */

import { describe, it, expect, afterEach } from 'vitest';
import { createApp, h, nextTick, ref } from 'vue';
import ComposerBox from '../src/components/ComposerBox.vue';
import ComposerNotice from '../src/components/ComposerNotice.vue';

const apps = [];
function mount(render) {
  const el = document.createElement('div');
  document.body.append(el);
  const app = createApp({ render });
  app.mount(el);
  apps.push(app);
  return { el, app };
}

afterEach(() => {
  while (apps.length) apps.pop().unmount();
  document.body.innerHTML = '';
});

describe('the reply box', () => {
  it('puts attributes on the text, hands the caller the element, and fits the text as it changes', async () => {
    const text = ref('');
    let handed = null;
    const { el } = mount(() => h(ComposerBox, {
      modelValue: text.value,
      'onUpdate:modelValue': (v) => { text.value = v; },
      placeholder: 'Say something',
      inputRef: (input) => { handed = input; },
    }, {
      tools: () => h('button', { type: 'button', 'data-test': 'tool' }),
      actions: () => h('button', { type: 'button', 'data-test': 'action' }),
    }));
    const textarea = el.querySelector('textarea');
    expect(handed).toBe(textarea);
    expect(textarea.placeholder).toBe('Say something');
    expect(el.firstElementChild.getAttribute('placeholder')).toBeNull();
    expect(el.querySelector('[data-test="tool"]')).not.toBeNull();
    // Send comes after the caller's actions.
    expect(el.querySelector('[data-test="action"]').nextElementSibling.type).toBe('submit');
    expect(el.querySelector('button[type="submit"]').title).toBe('Send Message');

    Object.defineProperty(textarea, 'scrollHeight', { configurable: true, value: 90 });
    textarea.value = 'two\nlines';
    textarea.dispatchEvent(new Event('input'));
    await nextTick();
    await nextTick();
    expect(text.value).toBe('two\nlines');
    expect(textarea.style.height).toBe('90px');

    Object.defineProperty(textarea, 'scrollHeight', { configurable: true, value: 400 });
    text.value = 'a lot';
    await nextTick();
    await nextTick();
    expect(textarea.style.height).toBe('150px');
  });

  it('turns Send off and names it as asked, without an element callback', () => {
    const { el } = mount(() => h(ComposerBox, { sendDisabled: true, sendTitle: 'Queue it' }));
    const send = el.querySelector('button[type="submit"]');
    expect(send.disabled).toBe(true);
    expect(send.title).toBe('Queue it');
  });

  it('fits nothing once it is gone', async () => {
    const text = ref('');
    const { app } = mount(() => h(ComposerBox, { modelValue: text.value }));
    text.value = 'late';
    await nextTick();
    apps.pop();
    app.unmount();
    await nextTick();
  });
});

describe('the strip under it', () => {
  it('is gray and still by default, with the words and an action', () => {
    const { el } = mount(() => h(ComposerNotice, null, { default: () => 'Queued', action: () => h('a', { 'data-test': 'act' }) }));
    const strip = el.firstElementChild;
    expect(strip.className).toContain('bg-gray-50');
    expect(strip.querySelector('span').className).not.toContain('animate-pulse');
    expect(strip.querySelector('p').textContent).toBe('Queued');
    expect(strip.querySelector('[data-test="act"]')).not.toBeNull();
  });

  it('takes the red and amber tones, and pulses when asked', () => {
    const { el } = mount(() => [
      h(ComposerNotice, { tone: 'red', pulse: true }, () => 'Offline'),
      h(ComposerNotice, { tone: 'amber' }, () => 'Not started'),
    ]);
    const [red, amber] = el.children;
    expect(red.className).toContain('bg-red-50');
    expect(red.querySelector('span').className).toContain('animate-pulse');
    expect(amber.className).toContain('bg-amber-50');
  });
});
