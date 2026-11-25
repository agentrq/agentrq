// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * One look for every form field, label, help line and action button, so the
 * forms read as one app. New Workspace set it; the others follow.
 */

/*
 * Every control is one height: a field, and a button beside it, are 42px
 * (a 20px line, 10px padding and a 1px border, top and bottom), 44px under
 * a finger, where a smaller target is missed.
 */
const BASE =
  'w-full bg-white dark:bg-zinc-950 border rounded-lg px-3 py-2.5 pointer-coarse:min-h-11 ' +
  'text-sm leading-5 text-gray-900 dark:text-zinc-100 placeholder:text-gray-400 dark:placeholder:text-zinc-600 ' +
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
 */
export function fieldClass(invalid = false, { answered = false } = {}) {
  return `${BASE} ${answered ? ANSWERED : DISABLED} ${invalid ? INVALID : VALID}`;
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

export const PRIMARY_BUTTON = `${BUTTON} px-5 ${PRIMARY_COLORS} ${DISABLED_BUTTON}`;

/** A bordered button beside a primary one. */
export const SECONDARY_BUTTON = `${BUTTON} px-5 ${SECONDARY_COLORS} ${DISABLED_BUTTON}`;

export const CANCEL_BUTTON = `${BUTTON} px-4 ${CANCEL_COLORS} ${DISABLED_BUTTON}`;

/** The band under a form's fields that holds its buttons. */
export const ACTIONS =
  'flex flex-col-reverse sm:flex-row sm:items-center sm:justify-end gap-2 sm:gap-3 px-4 sm:px-5 py-3 ' +
  'border-t border-gray-100 dark:border-zinc-800 bg-gray-50/60 dark:bg-zinc-950/40 rounded-b-xl';

/** The card a form sits in. */
export const FORM_CARD = 'bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl shadow-sm';
