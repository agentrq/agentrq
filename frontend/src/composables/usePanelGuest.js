// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The page inside the desktop side panel, behind one interface.
 *
 * `SidePanel.vue` never touches a guest directly: the shell decides what the
 * guest is. Electron's is a `<webview>` element; the Tauri build supplies a
 * native child webview through {@link registerPanelGuest}.
 *
 * Which one is chosen by the shell's own declaration, `window.agentrq.sidePanel
 * .guest` — never by probing for `<webview>` support, which says what the
 * runtime can do and not what the shell has set up around it.
 *
 * A guest answers:
 *   mount(host, url)  loadURL(url)  goBack()  goForward()  reload()  stop()
 *   canGoBack()  canGoForward()  focus()
 *   findInPage(text, { forward, findNext })  stopFindInPage(action)
 *   setVisible(bool)  setDragging(bool)  destroy()
 *   on(event, listener) → unsubscribe, for:
 *     loading   (boolean)
 *     navigate  ({ url, inPage })
 *     found     ({ active, total })
 *     fail      ({ code, description })   — never code -3, a replaced navigation
 */

/** What the shell is taken to be when it names no guest: the original one. */
export const DEFAULT_GUEST = 'electron';

/** "Aborted": a navigation replaced by another, which is not a failure. */
const ABORTED = -3;

function emitter() {
  const listeners = new Map();
  return {
    on(event, listener) {
      if (!listeners.has(event)) listeners.set(event, new Set());
      listeners.get(event).add(listener);
      return () => listeners.get(event).delete(listener);
    },
    emit(event, payload) {
      for (const listener of listeners.get(event) ?? []) listener(payload);
    },
  };
}

/**
 * Electron's guest: a `<webview>`, created in script rather than declared in a
 * template — its `src` has to be set before it is attached, and the template
 * compiler does not know the element.
 *
 * @param {object} [options]
 * @param {() => HTMLElement} [options.createElement]  test seam
 */
export function createElectronGuest({ createElement = () => document.createElement('webview') } = {}) {
  const events = emitter();
  let element = null;

  function listen(type, handler) {
    element.addEventListener(type, handler);
  }

  return {
    on: events.on,

    mount(host, url) {
      element = createElement();
      element.setAttribute('src', url);
      // White, as a browser's canvas is: a guest is transparent by default, and
      // a page that sets no background of its own would otherwise be dark text
      // on the app's dark theme.
      element.className = 'absolute inset-0 w-full h-full bg-white';
      listen('did-start-loading', () => events.emit('loading', true));
      listen('did-stop-loading', () => events.emit('loading', false));
      listen('did-navigate', (event) => events.emit('navigate', { url: event.url, inPage: false }));
      listen('did-navigate-in-page', (event) => {
        if (event.isMainFrame) events.emit('navigate', { url: event.url, inPage: true });
      });
      listen('found-in-page', (event) => events.emit('found', {
        active: event.result?.activeMatchOrdinal ?? 0,
        total: event.result?.matches ?? 0,
      }));
      listen('did-fail-load', (event) => {
        if (event.errorCode === ABORTED || event.isMainFrame === false) return;
        events.emit('fail', { code: event.errorCode, description: event.errorDescription });
      });
      host.append(element);
    },

    loadURL: (url) => element?.loadURL(url),
    goBack: () => element?.goBack(),
    goForward: () => element?.goForward(),
    canGoBack: () => Boolean(element?.canGoBack?.()),
    canGoForward: () => Boolean(element?.canGoForward?.()),
    reload: () => element?.reload(),
    stop: () => element?.stop(),
    focus: () => element?.focus(),
    findInPage: (text, options) => element?.findInPage(text, options),
    stopFindInPage: (action) => element?.stopFindInPage(action),

    // The `<webview>` is a DOM element: the panel hides it with its host, and
    // `pointer-events: none` during a resize drag is the host's class too.
    setVisible() {},
    setDragging() {},

    destroy() {
      element?.remove();
      element = null;
    },
  };
}

const implementations = new Map([[DEFAULT_GUEST, createElectronGuest]]);

/** Let another shell supply its own guest, by the name it declares. */
export function registerPanelGuest(kind, factory) {
  implementations.set(kind, factory);
}

/**
 * The guest the shell declared.
 *
 * @param {object} [options]
 * @param {{ guest?: string }} [options.bridge]  `window.agentrq.sidePanel`
 * @param {object} [options.options]  handed to the implementation
 */
export function usePanelGuest({ bridge = globalThis.window?.agentrq?.sidePanel, options } = {}) {
  const kind = bridge?.guest ?? DEFAULT_GUEST;
  const factory = implementations.get(kind);
  if (!factory) throw new Error(`Unknown side panel guest: ${kind}`);
  return factory(options);
}
