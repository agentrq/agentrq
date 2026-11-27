// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * One look for every form field, label, help line and action button, so the
 * forms read as one app. New Workspace set it; the others follow.
 */

/*
 * Every control of a size is one height. A form's field, and a button beside
 * it, are 42px (a 20px line, 10px padding, a 1px border), 44px under a
 * finger. The task chat is smaller on purpose, like its messages: a field is
 * 32px (13px type, an 18px line, 6px padding), as its queued-message editor.
 * A `compact` field is 36px, on a phone as well, beside the compact buttons.
 */
export const CONTROL_SIZE = {
  form: 'rounded-lg px-3 py-2.5 pointer-coarse:min-h-11 text-sm leading-5',
  chat: 'rounded-sm px-2 py-1.5 text-[13px] leading-[18px]',
  compact: 'rounded-sm px-3 py-2 text-xs leading-[18px]',
};

const BASE =
  'w-full bg-white dark:bg-zinc-950 border ' +
  'text-gray-900 dark:text-zinc-100 placeholder:text-gray-400 dark:placeholder:text-zinc-600 ' +
  'outline-none transition-shadow focus:ring-2';

const DISABLED = 'disabled:opacity-50 disabled:cursor-not-allowed';

// An answered card's fields: still read in full, since they say what was sent.
const ANSWERED =
  'disabled:bg-gray-50 dark:disabled:bg-zinc-900 disabled:text-gray-700 dark:disabled:text-zinc-300 disabled:cursor-default';

const VALID =
  'border-gray-300 dark:border-zinc-700 focus:border-gray-900 dark:focus:border-zinc-300 ' +
  'focus:ring-gray-900/10 dark:focus:ring-white/10';

const INVALID =
  'border-red-500 dark:border-red-500/70 focus:border-red-600 dark:focus:border-red-400 ' +
  'focus:ring-red-500/15 dark:focus:ring-red-500/20';

/**
 * A text field, select or textarea; red while `invalid`. An `answered` one,
 * disabled because its answer was sent, keeps its text at full strength.
 * `size` is `form`, or `chat` for one inside the task chat.
 */
export function fieldClass(invalid = false, { answered = false, size = 'form' } = {}) {
  return `${BASE} ${CONTROL_SIZE[size]} ${answered ? ANSWERED : DISABLED} ${invalid ? INVALID : VALID}`;
}

export const FIELD = fieldClass();

export const LABEL = 'block text-xs font-bold text-gray-800 dark:text-zinc-200';

export const HELP = 'text-[11px] font-medium text-gray-500 dark:text-zinc-400';

export const ERROR = 'text-[11px] font-bold text-red-600 dark:text-red-400';

/** What every button shares: the field's height, a focus ring for the keyboard. Padding is each kind's own. */
export const BUTTON =
  'inline-flex items-center justify-center gap-2 py-2.5 pointer-coarse:min-h-11 rounded-lg border ' +
  'text-[11px] leading-5 font-black uppercase tracking-widest transition-all disabled:cursor-not-allowed ' +
  'outline-none focus-visible:ring-2 focus-visible:ring-gray-900/20 dark:focus-visible:ring-white/25';

export const PRIMARY_COLORS =
  'bg-black dark:bg-white text-white dark:text-zinc-900 border-transparent hover:opacity-90';

export const SECONDARY_COLORS =
  'bg-white dark:bg-zinc-900 text-gray-800 dark:text-zinc-100 border-gray-200 dark:border-zinc-700 ' +
  'hover:bg-gray-50 dark:hover:bg-zinc-800';

export const CANCEL_COLORS =
  'border-transparent text-gray-600 dark:text-zinc-300 hover:bg-gray-100 dark:hover:bg-zinc-800';

export const DISABLED_BUTTON = 'disabled:opacity-30';

/**
 * A button inside the task chat, as its Cancel and Send Now: small, bold
 * capitals, 25px, a border on every kind so they are one height. The chat
 * puts them at the bottom right, the primary last.
 */
export const CHAT_BUTTON =
  'inline-flex items-center justify-center gap-1 px-2.5 py-1 rounded-sm border shrink-0 ' +
  'text-[10px] leading-[15px] font-bold uppercase tracking-widest transition-colors disabled:cursor-not-allowed ' +
  'outline-none focus-visible:ring-2 focus-visible:ring-gray-900/20 dark:focus-visible:ring-white/25';

export const CHAT_SECONDARY_COLORS =
  'bg-white dark:bg-zinc-900 border-gray-300 dark:border-zinc-700 text-gray-900 dark:text-zinc-100 ' +
  'hover:border-gray-400 dark:hover:border-zinc-500';

export const CHAT_PRIMARY_BUTTON = `${CHAT_BUTTON} ${PRIMARY_COLORS}`;

/**
 * The chat's toggle, as the task page's Agent | Human and Chat | Trajectory:
 * a grey track, the picked segments raised in white. Segments are the
 * chat buttons' type, so a toggle and a button beside it read as one size.
 */
export const CHAT_TOGGLE_TRACK =
  'inline-flex flex-wrap gap-0.5 max-w-full p-0.5 rounded-lg bg-gray-100 dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700/50';

const CHAT_SEGMENT =
  'inline-flex items-center justify-center px-2.5 py-1 rounded-md text-[10px] leading-[15px] font-bold uppercase tracking-widest ' +
  'cursor-pointer select-none transition-all outline-none focus-visible:ring-2 focus-visible:ring-gray-900/20 dark:focus-visible:ring-white/25 ' +
  'has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-gray-900/20 dark:has-[:focus-visible]:ring-white/25';

/** One segment of a chat toggle, raised while `on`. */
export function chatSegmentClass(on) {
  return `${CHAT_SEGMENT} ${on
    ? 'bg-white dark:bg-zinc-700 text-black dark:text-white shadow-sm'
    : 'text-gray-500 dark:text-zinc-400 hover:text-gray-800 dark:hover:text-zinc-200'}`;
}

export const CHAT_SECONDARY_BUTTON = `${CHAT_BUTTON} ${CHAT_SECONDARY_COLORS}`;

export const PRIMARY_BUTTON = `${BUTTON} px-5 ${PRIMARY_COLORS} ${DISABLED_BUTTON}`;

/** A bordered button beside a primary one. */
export const SECONDARY_BUTTON = `${BUTTON} px-5 ${SECONDARY_COLORS} ${DISABLED_BUTTON}`;

export const CANCEL_BUTTON = `${BUTTON} px-4 ${CANCEL_COLORS} ${DISABLED_BUTTON}`;

/** The band under a form's fields that holds its buttons. */
export const ACTIONS =
  'flex flex-col-reverse sm:flex-row sm:items-center sm:justify-end gap-2 sm:gap-3 px-4 sm:px-5 py-3 ' +
  'border-t border-gray-100 dark:border-zinc-800 bg-gray-50/60 dark:bg-zinc-950/40 rounded-b-xl';

/**
 * The image preview's buttons, for a panel's own actions rather than a
 * form's: 36px and a 4px corner, as Update Workspace's, sentence case.
 * The primary is the page's ink (black, white on dark), the secondary
 * bordered.
 */
const COMPACT_BUTTON =
  'inline-flex items-center justify-center gap-2 px-5 py-2 rounded-sm border text-[10px] leading-[18px] font-semibold transition-all ' +
  'disabled:opacity-30 disabled:cursor-not-allowed outline-none focus-visible:ring-2 focus-visible:ring-gray-900/20 dark:focus-visible:ring-white/25';

export const COMPACT_PRIMARY_BUTTON =
  `${COMPACT_BUTTON} bg-black dark:bg-white text-white dark:text-black border-transparent hover:bg-gray-800 dark:hover:bg-gray-100`;

export const COMPACT_SECONDARY_BUTTON =
  `${COMPACT_BUTTON} border-gray-300 dark:border-zinc-700 text-gray-800 dark:text-white hover:bg-gray-100 dark:hover:bg-zinc-800`;

/**
 * The on/off switch of the skill cards, styled by its own aria-checked so the
 * look cannot disagree with what a screen reader is told: a 44×24 track, dark
 * when on, and a 16px knob that slides 20px. Put SWITCH_KNOB in a span inside.
 */
export const SWITCH =
  'group relative block shrink-0 w-11 h-6 rounded-full border transition-colors motion-reduce:transition-none ' +
  'bg-gray-200 dark:bg-zinc-700 border-gray-300 dark:border-zinc-600 ' +
  'aria-checked:bg-gray-900 aria-checked:border-gray-900 dark:aria-checked:bg-white dark:aria-checked:border-white ' +
  'disabled:opacity-50 disabled:cursor-not-allowed ' +
  'outline-none focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:ring-gray-900/30 dark:focus-visible:ring-white/30 dark:focus-visible:ring-offset-zinc-900';

export const SWITCH_KNOB =
  'absolute top-[3px] left-[3px] size-4 rounded-full bg-white dark:bg-zinc-400 transition-transform motion-reduce:transition-none ' +
  'group-aria-checked:translate-x-5 dark:group-aria-checked:bg-zinc-900';

/** The card a form sits in. */
export const FORM_CARD = 'bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl shadow-sm';
