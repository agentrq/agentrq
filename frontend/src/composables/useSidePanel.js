// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { reactive } from 'vue';

/**
 * The desktop app's side panel: whether it is open, how wide, and what it shows.
 *
 * One panel per window, so the state is the module's rather than a component's
 * — the sidebar button, the title bar, the menu accelerator and a link in a
 * message all reach the same one.
 *
 * Remembered per device in `localStorage`, because it is a layout preference
 * rather than anything an account owns. Every access is guarded: storage can be
 * missing or throw (a private window, blocked site data), and a panel that
 * forgets its width is better than an app that fails to start.
 */

export const SIDE_PANEL_STORAGE_KEY = 'agentrq:side-panel';

/** The width before the window has been measured. */
export const DEFAULT_WIDTH = 440;
export const MIN_WIDTH = 320;
/**
 * What the main column keeps however wide the panel is dragged — about a
 * phone's width, which is also what an unresized panel leaves it: the task
 * view lays itself out by its own width, so beside the panel it looks the way
 * it does on a phone.
 */
export const MIN_MAIN = 480;

/**
 * How wide the panel is drawn.
 *
 * `width` is what somebody dragged it to, or `null` when they never have —
 * in which case the panel takes all the room the main column does not need.
 */
export function panelWidth(width, available = Infinity) {
  if (width === null || width === undefined) return clampWidth(available - MIN_MAIN, available);
  return clampWidth(width, available);
}

/** The schemes a panel may show, and therefore the only ones restored. */
const PANEL_PROTOCOLS = new Set(['http:', 'https:', 'agentrq-ext:']);

/** Hosts that are only ever served over plain http in practice. */
const LOCAL_HOST_RE = /^(localhost|127\.0\.0\.1|\[::1\])(:\d+)?(\/|$)/i;

/** A scheme, as opposed to `host:port` — the character after the colon is not a digit. */
const SCHEME_RE = /^[a-z][a-z0-9+.-]*:(?!\d)/i;

/**
 * The panel's width for a container `available` pixels wide.
 *
 * Never narrower than {@link MIN_WIDTH}, and never so wide that the main column
 * has less than {@link MIN_MAIN} — unless the window is too small for both, in
 * which case the panel keeps its minimum and the main column gives way, since a
 * panel too narrow to use is worse than a main column that scrolls.
 */
export function clampWidth(width, available = Infinity) {
  const wanted = Number.isFinite(width) ? width : DEFAULT_WIDTH;
  const widest = Math.max(MIN_WIDTH, available - MIN_MAIN);
  return Math.round(Math.min(Math.max(wanted, MIN_WIDTH), widest));
}

/**
 * What somebody typed into the address field, as a URL the panel will load.
 *
 * A bare host gets a scheme the way a browser would give it one — `https://`,
 * or `http://` for the local machine, where nothing has a certificate. Any
 * scheme the panel does not show (`javascript:`, `file:`, `data:` …) answers
 * '' so the field refuses it rather than handing it on.
 */
export function normaliseAddress(text) {
  const typed = String(text ?? '').trim();
  if (!typed || /\s/.test(typed)) return '';

  let candidate = typed;
  if (SCHEME_RE.test(typed)) {
    if (!/^(https?|agentrq-ext):\/\//i.test(typed)) return '';
  } else {
    candidate = `${LOCAL_HOST_RE.test(typed) ? 'http' : 'https'}://${typed}`;
  }

  try {
    const url = new URL(candidate);
    return url.host ? url.href : '';
  } catch {
    return '';
  }
}

/** Whether `url` is something the panel shows, and therefore safe to restore. */
export function isPanelUrl(url) {
  try {
    return PANEL_PROTOCOLS.has(new URL(String(url ?? '')).protocol);
  } catch {
    return false;
  }
}

function defaultStorage() {
  try {
    return globalThis.localStorage ?? null;
  } catch {
    return null;
  }
}

/** The remembered state, with anything unusable replaced by a default. */
export function readSaved(storage = defaultStorage()) {
  let saved = {};
  try {
    saved = JSON.parse(storage?.getItem(SIDE_PANEL_STORAGE_KEY) ?? '{}') ?? {};
  } catch {
    saved = {};
  }
  return {
    open: saved.open === true,
    // Taking all the room beside the sidebar, with the main column hidden.
    full: saved.full === true,
    // `null` is "fill the window", and is what an unresized panel remembers.
    width: typeof saved.width === 'number' ? clampWidth(saved.width) : null,
    url: isPanelUrl(saved.url) ? saved.url : '',
  };
}

/** Remember the state. A storage that refuses is ignored, not reported. */
export function writeSaved({ open, full, width, url }, storage = defaultStorage()) {
  try {
    storage?.setItem(SIDE_PANEL_STORAGE_KEY, JSON.stringify({ open, full, width, url }));
  } catch {
    // Nothing useful to do: the panel works, it just will not be remembered.
  }
}

let shared = null;
const nobody = () => {};
/** Told each time a closed panel opens, whoever opened it. App.vue reports it. */
let announceOpen = nobody;

/**
 * The panel.
 *
 * @param {object} [options]
 * @param {Storage|null} [options.storage]  test seam; `localStorage` otherwise
 * @param {() => void} [options.onOpen]  called each time a closed panel opens,
 *   by this caller or any other — the task view's button and a link both count
 */
export function useSidePanel({ storage = defaultStorage(), onOpen } = {}) {
  if (!shared) shared = reactive(readSaved(storage));
  if (onOpen) announceOpen = onOpen;
  const state = shared;
  const save = () => writeSaved(state, storage);

  function open(url = '') {
    if (url && isPanelUrl(url)) state.url = url;
    if (!state.open) {
      state.open = true;
      announceOpen();
    }
    save();
  }

  function close() {
    state.open = false;
    save();
  }

  function toggle() {
    if (state.open) close();
    else open();
  }

  /** Where the panel is now — kept so a relaunch reopens the same page. */
  function setUrl(url) {
    if (!isPanelUrl(url) || url === state.url) return;
    state.url = url;
    save();
  }

  function setWidth(width, available) {
    state.width = clampWidth(width, available);
    save();
  }

  /**
   * All the room beside the sidebar, or back to sharing it. The main column is
   * hidden rather than closed, so the page beside the panel is still there,
   * scrolled where it was, when the panel is collapsed again.
   */
  function setFull(full) {
    state.full = Boolean(full);
    save();
  }

  function toggleFull() {
    setFull(!state.full);
  }

  /** Back to filling the room the main column leaves. */
  function resetWidth() {
    state.width = null;
    save();
  }

  return { state, open, close, toggle, setUrl, setWidth, resetWidth, setFull, toggleFull };
}

/** Forget the shared state, so each test starts from storage. */
export function resetSidePanelForTests() {
  shared = null;
  announceOpen = nobody;
}
