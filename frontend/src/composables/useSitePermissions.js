// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The words for the desktop side panel's permission questions and its Site
 * permissions list.
 *
 * The main process decides and remembers (`desktop/src/main/side-panel/`);
 * this only says it. Kept as plain functions so what a person reads before
 * letting a site use their camera is tested word for word.
 */

/** Each remembered permission: the noun for the list, the verb for the question. */
export const PERMISSIONS = {
  camera: { name: 'Camera', ask: 'use your camera' },
  microphone: { name: 'Microphone', ask: 'use your microphone' },
  geolocation: { name: 'Location', ask: 'know your location' },
  notifications: { name: 'Notifications', ask: 'show notifications' },
  midi: { name: 'MIDI devices', ask: 'use your MIDI devices' },
  'display-capture': { name: 'Screen sharing', ask: 'see your screen' },
  'clipboard-read': { name: 'Clipboard', ask: 'read your clipboard' },
  'idle-detection': { name: 'Idle detection', ask: 'know when you are away' },
  'window-management': { name: 'Window management', ask: 'manage windows on your displays' },
  'local-fonts': { name: 'Fonts', ask: 'use the fonts on this computer' },
  'storage-access': { name: 'Cookies on other sites', ask: 'use its cookies on other sites' },
  'pointer-lock': { name: 'Pointer lock', ask: 'hide and lock your pointer' },
  'keyboard-lock': { name: 'Keyboard lock', ask: 'capture your keyboard shortcuts' },
  'speaker-selection': { name: 'Speakers', ask: 'choose your speakers' },
};

const EXTENSION_PROTOCOL = 'agentrq-ext:';

/**
 * Who is asking, the way a person would say it: the site's host, or the
 * Extension by name for one of an Extension's own pages.
 */
export function siteLabel(origin) {
  try {
    const url = new URL(String(origin ?? ''));
    if (url.protocol === EXTENSION_PROTOCOL) return `The ${url.host} extension`;
    return url.host || String(origin);
  } catch {
    return String(origin ?? '');
  }
}

/** The list's name for a permission; an unknown key is shown as it is. */
export function permissionName(key) {
  return PERMISSIONS[key]?.name ?? key;
}

/** "a", "a and b", "a, b and c". */
function listOf(phrases) {
  if (phrases.length <= 1) return phrases.join('');
  return `${phrases.slice(0, -1).join(', ')} and ${phrases.at(-1)}`;
}

/**
 * The question in the bar: "meet.example wants to use your camera and use your
 * microphone". The camera and microphone of one call read as one request.
 */
export function describeQuestion({ origin, permissions = [] } = {}) {
  const asks = permissions.map((key) => PERMISSIONS[key]?.ask ?? `use ${key}`);
  if (permissions.length === 2 && permissions.includes('camera') && permissions.includes('microphone')) {
    return `${siteLabel(origin)} wants to use your camera and microphone`;
  }
  return `${siteLabel(origin)} wants to ${listOf(asks)}`;
}

/** The origin a page address belongs to, or '' when it has none worth naming. */
export function originOf(rawUrl) {
  try {
    const url = new URL(String(rawUrl ?? ''));
    return url.host ? `${url.protocol}//${url.host}` : '';
  } catch {
    return '';
  }
}

/** Whether the site a page is on has anything remembered for it. */
export function hasDecisions(sites, rawUrl) {
  const origin = originOf(rawUrl);
  return Boolean(origin) && (sites ?? []).some((site) => site.origin === origin);
}
