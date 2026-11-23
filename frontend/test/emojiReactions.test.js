// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi } from 'vitest';
import {
  QUICK_REACTIONS,
  isReactionText,
  sideOf,
  foldReactions,
  summarizeReactions,
  emojiKeyboardHint,
  openEmojiKeyboard,
} from '../src/composables/useEmojiReactions';

const agent = (id, text = 'Done.') => ({ id, sender: 'agent', text });
const human = (id, text = 'Thanks') => ({ id, sender: 'human', text });

describe('isReactionText', () => {
  it('takes one emoji, with or without surrounding whitespace', () => {
    for (const text of ['👍', ' ❤️ ', '✅\n', '👍🏽', '👨‍👩‍👧', '🇹🇷', '1️⃣']) {
      expect(isReactionText(text), `${JSON.stringify(text)} is a reaction`).toBe(true);
    }
  });

  it('refuses two emoji, an emoji beside words, plain text and non-strings', () => {
    for (const text of ['👍👍', 'ok 👍', 'ok', '1', '', '   ', null, undefined, 42]) {
      expect(isReactionText(text), `${JSON.stringify(text)} is not a reaction`).toBe(false);
    }
  });

  it('accepts every quick reaction it offers', () => {
    for (const emoji of QUICK_REACTIONS) expect(isReactionText(emoji)).toBe(true);
  });
});

describe('sideOf', () => {
  it('puts the agent on its own side and people, Slack included, on the other', () => {
    expect(sideOf({ sender: 'agent' })).toBe('agent');
    expect(sideOf({ sender: 'human' })).toBe('human');
    expect(sideOf({ sender: 'slack' })).toBe('human');
  });
});

describe('foldReactions', () => {
  it("hangs a person's emoji on the agent's message before it", () => {
    const { thread, reactions } = foldReactions([human(1), agent(2), human(3, '👍')]);
    expect(thread.map((m) => m.id)).toEqual([1, 2]);
    expect(reactions[2].map((m) => m.id)).toEqual([3]);
  });

  it("hangs the agent's emoji on the person's message, skipping its own messages in between", () => {
    const { thread, reactions } = foldReactions([human(1), agent(2), agent(3, '✅')]);
    expect(thread.map((m) => m.id)).toEqual([1, 2]);
    expect(reactions[1].map((m) => m.id)).toEqual([3]);
  });

  it('collects several reactions on one message, in the order they were sent', () => {
    const { reactions } = foldReactions([agent(1), human(2, '👍'), human(3, '🎉')]);
    expect(reactions[1].map((m) => m.id)).toEqual([2, 3]);
  });

  it('keeps an emoji as a bubble when the other side has said nothing before it', () => {
    const { thread, reactions } = foldReactions([human(1, '👋'), human(2)]);
    expect(thread.map((m) => m.id)).toEqual([1, 2]);
    expect(reactions).toEqual({});
  });

  it('never reacts to a reaction: answering one with an emoji leaves a bubble', () => {
    const { thread, reactions } = foldReactions([agent(1), human(2, '👍'), agent(3, '❤️')]);
    expect(thread.map((m) => m.id)).toEqual([1, 3]);
    expect(reactions[1].map((m) => m.id)).toEqual([2]);
  });

  it('skips over a message that cannot carry a reaction', () => {
    const plan = { id: 'plan', sender: 'agent', text: '', metadata: { type: 'plan' } };
    const { reactions } = foldReactions([agent(1), plan, human(2, '👍')], (m) => m !== plan);
    expect(reactions[1].map((m) => m.id)).toEqual([2]);
  });

  it('leaves messages not yet sent, attachments and cards as bubbles', () => {
    const held = [
      { id: 'p', sender: 'human', text: '👍', _pending: true },
      { id: 'q', sender: 'human', text: '👍', _queued: true },
      { id: 'a', sender: 'human', text: '👍', attachments: [{ filename: 'x.png' }] },
      { id: 'c', sender: 'agent', text: '👍', metadata: { type: 'permission_request' } },
    ];
    const { thread, reactions } = foldReactions([agent(1), human(2), ...held]);
    expect(thread.map((m) => m.id)).toEqual([1, 2, 'p', 'q', 'a', 'c']);
    expect(reactions).toEqual({});
  });

  it('treats an empty attachment list as no attachment', () => {
    const { reactions } = foldReactions([agent(1), { id: 2, sender: 'human', text: '👍', attachments: [] }]);
    expect(reactions[1].map((m) => m.id)).toEqual([2]);
  });
});

describe('summarizeReactions', () => {
  it('counts each emoji once in first-use order and names who sent it', () => {
    const list = [human(1, '👍'), human(2, '🎉'), { id: 3, sender: 'slack', text: ' 👍 ' }, agent(4, '👍')];
    expect(summarizeReactions(list)).toEqual([
      { emoji: '👍', count: 3, label: 'You and Agent reacted 👍' },
      { emoji: '🎉', count: 1, label: 'You reacted 🎉' },
    ]);
  });
});

describe('emojiKeyboardHint', () => {
  it('points a phone at its own keyboard', () => {
    expect(emojiKeyboardHint({}, { userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0)', maxTouchPoints: 5 }))
      .toBe('Use the emoji key on your keyboard');
  });

  it('gives the macOS shortcut on a Mac, in the desktop app or a browser', () => {
    expect(emojiKeyboardHint({ os: 'darwin' }, { userAgent: '' })).toBe('Press Control-Command-Space for all emoji');
    expect(emojiKeyboardHint({}, { userAgent: '', platform: 'MacIntel' })).toBe('Press Control-Command-Space for all emoji');
  });

  it('gives the Windows shortcut on Windows', () => {
    expect(emojiKeyboardHint({ os: 'win32' }, { userAgent: '' })).toBe('Press Windows-. for all emoji');
    expect(emojiKeyboardHint({}, { userAgent: '', userAgentData: { platform: 'Windows' } })).toBe('Press Windows-. for all emoji');
  });

  it("falls back to the system's own picker elsewhere", () => {
    expect(emojiKeyboardHint({}, { userAgent: '', platform: 'Linux x86_64' })).toBe("Use your system's emoji picker for all emoji");
    expect(emojiKeyboardHint({}, { userAgent: '' })).toBe("Use your system's emoji picker for all emoji");
  });

  it('reads the real navigator when none is given', () => {
    expect(typeof emojiKeyboardHint()).toBe('string');
  });
});

describe('openEmojiKeyboard', () => {
  it("opens the desktop shell's panel after focusing the field", async () => {
    const order = [];
    const field = { focus: () => order.push('focus') };
    const bridge = { showPanel: vi.fn(async () => { order.push('panel'); return true; }) };
    await expect(openEmojiKeyboard({ field, isDesktop: true, bridge })).resolves.toBe(true);
    expect(order).toEqual(['focus', 'panel']);
  });

  it('reports false when the shell has no panel on this system', async () => {
    const bridge = { showPanel: async () => false };
    await expect(openEmojiKeyboard({ isDesktop: true, bridge })).resolves.toBe(false);
  });

  it('reports false when the bridge call fails', async () => {
    const bridge = { showPanel: async () => { throw new Error('bridge disconnected'); } };
    await expect(openEmojiKeyboard({ isDesktop: true, bridge })).resolves.toBe(false);
  });

  it('only focuses the field in a browser, which cannot open the keyboard', async () => {
    const field = { focus: vi.fn() };
    const bridge = { showPanel: vi.fn() };
    await expect(openEmojiKeyboard({ field, bridge })).resolves.toBe(false);
    expect(field.focus).toHaveBeenCalledOnce();
    expect(bridge.showPanel).not.toHaveBeenCalled();
  });

  it('copes with no field and no options at all', async () => {
    await expect(openEmojiKeyboard()).resolves.toBe(false);
    await expect(openEmojiKeyboard({ field: null, isDesktop: true, bridge: {} })).resolves.toBe(false);
  });
});
