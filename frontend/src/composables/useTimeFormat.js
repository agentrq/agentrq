// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { ref } from 'vue';
import * as api from '../api';

/**
 * The clock every time in the app is shown with: the account's own setting,
 * kept in its account settings. `browser` follows the browser's language.
 * One ref for the whole app, so a time drawn anywhere follows a change at once.
 */

// The account settings' groups and fields, as the API names them.
export const DISPLAY_SETTINGS = 'display';
export const TIME_FORMAT = 'timeFormat';

// The values of TIME_FORMAT.
export const TIME_FORMAT_BROWSER = 'browser';
export const TIME_FORMAT_12H = '12h';
export const TIME_FORMAT_24H = '24h';
export const TIME_FORMATS = [TIME_FORMAT_BROWSER, TIME_FORMAT_12H, TIME_FORMAT_24H];

export const timeFormat = ref(TIME_FORMAT_BROWSER);

/** The hour cycle the browser's language uses: h12 for en-US, h23 for en-GB. */
export function browserHourCycle(locale) {
  const cycle = new Intl.DateTimeFormat(locale, { hour: 'numeric' }).resolvedOptions().hourCycle;
  return cycle === 'h11' || cycle === 'h12' ? 'h12' : 'h23';
}

/**
 * Intl options with the account's clock added. Every time shown goes through
 * this, so none forces a clock of its own. `hourCycle` and not `hour12`, which
 * would win over it; the browser's own cycle rather than none, so a place
 * that names a locale for its month names still follows the browser's clock.
 *
 * @param {Intl.DateTimeFormatOptions} options
 */
export function withClock(options, format = timeFormat.value) {
  const hourCycle = format === TIME_FORMAT_12H ? 'h12' : format === TIME_FORMAT_24H ? 'h23' : browserHourCycle();
  return { ...options, hourCycle };
}

/** Reads the account's clock; a failed read leaves the browser's. */
export async function loadTimeFormat(fetchSettings = () => api.fetchAccountSettings()) {
  try {
    const res = await fetchSettings();
    const format = res?.settings?.[DISPLAY_SETTINGS]?.[TIME_FORMAT];
    if (TIME_FORMATS.includes(format)) timeFormat.value = format;
  } catch {
    // The browser's clock stands.
  }
}

/**
 * Saves the account's clock. Shown at once; put back, and the refusal thrown,
 * when the server does not keep it.
 */
export async function saveTimeFormat(format, saveSettings = (settings) => api.updateAccountSettings(settings)) {
  const before = timeFormat.value;
  timeFormat.value = format;
  try {
    await saveSettings({ [DISPLAY_SETTINGS]: { [TIME_FORMAT]: format } });
  } catch (e) {
    timeFormat.value = before;
    throw e;
  }
}
