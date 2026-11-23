// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { usesCommandKey } from './useKeyboardShortcuts';
import { isMobileDevice } from './useNativeDictation';

/**
 * Reactions to a message, drawn the way a chat app draws them.
 *
 * There is no such thing as a reaction on the server: a reaction is a reply
 * whose whole text is one emoji, sent and stored like any other, so an agent
 * reads it as the plain message it is. Only the thread draws it differently,
 * as a badge on the last thing the other side said rather than as a bubble of
 * its own.
 */

/** The reactions offered with one tap. */
export const QUICK_REACTIONS = ['👍', '❤️', '😂', '😮', '🙏', '✅', '👀', '🎉'];

// A grapheme that is an emoji: a pictograph (with any skin tone, variation
// selector or joined sequence riding on it), a flag's pair of regional
// indicators, or a keycap such as 1️⃣.
const EMOJI = /\p{Extended_Pictographic}|\p{Regional_Indicator}|⃣/u;

const segmenter = new Intl.Segmenter(undefined, { granularity: 'grapheme' });

/**
 * Whether a message's text is a reaction: exactly one emoji, and nothing else
 * but surrounding whitespace. Two emoji, or one beside a word, is something
 * said, and stays a bubble.
 *
 * @param {unknown} text
 * @returns {boolean}
 */
export function isReactionText(text) {
  if (typeof text !== 'string') return false;
  const graphemes = [...segmenter.segment(text.trim())];
  return graphemes.length === 1 && EMOJI.test(graphemes[0].segment);
}

/**
 * Which side of the conversation a message is on. Slack messages are a
 * person's, so they sit on the human's side with the human's own.
 *
 * @param {{ sender?: string }} m
 * @returns {'agent' | 'human'}
 */
export function sideOf(m) {
  return m.sender === 'agent' ? 'agent' : 'human';
}

/**
 * Whether a message is drawn as a reaction rather than a bubble. A message
 * not yet sent is still the sender's to change, an attachment or a card
 * (a permission request, a question) is more than an emoji, so those stay
 * bubbles whatever their text.
 */
function canReact(m) {
  return !m._pending && !m._queued && !m.metadata?.type
    && !(m.attachments?.length) && isReactionText(m.text);
}

/**
 * Take the reactions out of a thread and hang each on the message it answers:
 * the nearest one before it from the other side. A reaction with nothing to
 * answer — the first thing in a thread, or after only its own side's
 * messages — has nowhere to hang and stays a bubble.
 *
 * @template {{ id: string|number, sender?: string }} M
 * @param {M[]} messages in thread order
 * @param {(m: M) => boolean} [canCarry] whether a message can be reacted to;
 *   one that cannot (a plan card) is skipped over, not reacted to
 * @returns {{ thread: M[], reactions: Record<string, M[]> }} the thread without
 *   its reactions, and the reactions on each message by its id
 */
export function foldReactions(messages, canCarry = () => true) {
  const thread = [];
  const reactions = {};
  for (const m of messages) {
    if (canReact(m)) {
      const side = sideOf(m);
      const target = thread.findLast((t) => sideOf(t) !== side && canCarry(t));
      if (target) {
        (reactions[target.id] ??= []).push(m);
        continue;
      }
    }
    thread.push(m);
  }
  return { thread, reactions };
}

/**
 * One badge entry per emoji, in the order each was first used, with how many
 * times it was sent and by whom.
 *
 * @param {{ text: string, sender?: string }[]} list
 * @returns {{ emoji: string, count: number, label: string }[]}
 */
export function summarizeReactions(list) {
  const byEmoji = new Map();
  for (const r of list) {
    const emoji = r.text.trim();
    const entry = byEmoji.get(emoji) ?? { emoji, count: 0, senders: [] };
    entry.count += 1;
    const who = sideOf(r) === 'agent' ? 'Agent' : 'You';
    if (!entry.senders.includes(who)) entry.senders.push(who);
    byEmoji.set(emoji, entry);
  }
  return [...byEmoji.values()].map(({ emoji, count, senders }) => ({
    emoji,
    count,
    label: `${senders.join(' and ')} reacted ${emoji}`,
  }));
}

/**
 * How to bring up the system's emoji keyboard by hand, for where the app
 * cannot open it: a browser has no way to, and the desktop shell has one only
 * on macOS and Windows.
 *
 * @param {{ os?: string }} [platform] the platform store's state
 * @param {Navigator} [nav]
 * @returns {string}
 */
export function emojiKeyboardHint(platform = {}, nav = globalThis.navigator) {
  if (isMobileDevice(nav)) return 'Use the emoji key on your keyboard';
  if (usesCommandKey(platform, nav)) return 'Press Control-Command-Space for all emoji';
  const reported = platform.os || nav?.userAgentData?.platform || nav?.platform || '';
  if (/win/i.test(reported)) return 'Press Windows-. for all emoji';
  return "Use your system's emoji picker for all emoji";
}

/**
 * Open the system's emoji keyboard on a text field.
 *
 * The field is focused first in every case: the keyboard types into whatever
 * has focus, and on a phone focusing it is what brings the keyboard up.
 *
 * @param {{ field?: { focus?: () => void } | null, isDesktop?: boolean,
 *   bridge?: { showPanel?: () => Promise<boolean> } }} opts
 * @returns {Promise<boolean>} whether a keyboard was opened; when false the
 *   caller says how to open it instead (emojiKeyboardHint)
 */
export async function openEmojiKeyboard({ field, isDesktop = false, bridge } = {}) {
  field?.focus?.();
  if (!isDesktop || typeof bridge?.showPanel !== 'function') return false;
  try {
    return (await bridge.showPanel()) === true;
  } catch {
    return false;
  }
}
