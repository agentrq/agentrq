// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * One look for every form field, label, help line and action button, so the
 * forms read as one app. New Workspace set it; the others follow.
 */

const BASE =
  'w-full bg-white dark:bg-zinc-950 border rounded-lg px-3 py-2.5 ' +
  'text-sm text-gray-900 dark:text-zinc-100 placeholder:text-gray-400 dark:placeholder:text-zinc-600 ' +
  'outline-none transition-shadow focus:ring-2 disabled:opacity-50 disabled:cursor-not-allowed';

const VALID =
  'border-gray-300 dark:border-zinc-700 focus:border-gray-900 dark:focus:border-zinc-300 ' +
  'focus:ring-gray-900/10 dark:focus:ring-white/10';

const INVALID =
  'border-red-500 dark:border-red-500/70 focus:border-red-600 dark:focus:border-red-400 ' +
  'focus:ring-red-500/15 dark:focus:ring-red-500/20';

/** A text field, select or textarea; red while `invalid`. */
export function fieldClass(invalid = false) {
  return `${BASE} ${invalid ? INVALID : VALID}`;
}

export const FIELD = fieldClass();

export const LABEL = 'block text-xs font-bold text-gray-800 dark:text-zinc-200';

export const HELP = 'text-[11px] font-medium text-gray-500 dark:text-zinc-400';

export const ERROR = 'text-[11px] font-bold text-red-600 dark:text-red-400';

export const PRIMARY_BUTTON =
  'inline-flex items-center justify-center gap-2 bg-black dark:bg-white text-white dark:text-zinc-900 ' +
  'px-5 py-2.5 rounded-lg text-[11px] font-black uppercase tracking-widest hover:opacity-90 ' +
  'disabled:opacity-30 disabled:cursor-not-allowed transition-all';

export const CANCEL_BUTTON =
  'inline-flex items-center justify-center px-4 py-2.5 rounded-lg text-[11px] font-black uppercase tracking-widest ' +
  'text-gray-600 dark:text-zinc-300 hover:bg-gray-100 dark:hover:bg-zinc-800 transition-colors';

/** The band under a form's fields that holds its buttons. */
export const ACTIONS =
  'flex flex-col-reverse sm:flex-row sm:items-center sm:justify-end gap-2 sm:gap-3 px-4 sm:px-5 py-3 ' +
  'border-t border-gray-100 dark:border-zinc-800 bg-gray-50/60 dark:bg-zinc-950/40 rounded-b-xl';

/** The card a form sits in. */
export const FORM_CARD = 'bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl shadow-sm';
