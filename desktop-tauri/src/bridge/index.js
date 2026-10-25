// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * `window.agentrq`, shaped exactly like `desktop/src/preload/index.js`.
 *
 * The renderer is Electron's renderer, so this is the same seam with Tauri
 * behind it: a call is `invoke('<command>')`, an `on*` listener is `listen()`,
 * and a command is its Electron channel with `agentrq:` dropped and `:` / `-`
 * turned into `_` (`agentrq:side-panel:permission-answer` is
 * `side_panel_permission_answer`). `test/parity.test.js` fails when the two
 * files drift apart, so a preload change cannot land without this one.
 *
 * Arguments cross as one object keyed by the preload's parameter names, which
 * Tauri maps onto the command's snake_case parameters.
 */
import { getTauriVersion } from '@tauri-apps/api/app'
import { Channel, invoke } from '@tauri-apps/api/core'
import { listen } from '@tauri-apps/api/event'
import { installEventSource, installFetch } from './net.js'

/** Node's name for the OS, as Electron's `process.platform` gives it. */
export function platformOf(userAgent) {
  const ua = String(userAgent ?? '')
  if (/Mac/.test(ua)) return 'darwin'
  if (/Windows/.test(ua)) return 'win32'
  return 'linux'
}

/**
 * Listen to an event and hand back an unsubscribe at once, as the preload does.
 * `listen` is asynchronous, so an unsubscribe that comes first is remembered
 * and applied when the listener arrives, never lost; an event that lands in
 * between is dropped.
 *
 * @returns {() => void}
 */
function subscribe(event, deliver) {
  let stop = null
  let stopped = false
  listen(event, (e) => {
    if (!stopped) deliver(e.payload)
  })
    .then((unlisten) => {
      if (stopped) unlisten()
      else stop = unlisten
    })
    .catch((err) => console.error(`window.agentrq: cannot listen to ${event}`, err))
  return () => {
    stopped = true
    if (stop) stop()
    stop = null
  }
}

/**
 * Freeze the bridge as Electron's contextBridge does, so page code cannot
 * replace a member. `versions` stays mutable until the Tauri version resolves.
 */
function deepFreeze(object) {
  for (const [key, value] of Object.entries(object)) {
    if (key !== 'versions' && value && typeof value === 'object') deepFreeze(value)
  }
  return Object.freeze(object)
}

/**
 * Off macOS the menu's keys are the page's to run, not GTK's or Windows': a
 * native accelerator takes the key before the page, so Ctrl+W would close the
 * app under a terminal that wanted it. Electron runs a menu key only when the
 * page left it unhandled, and so does this: a bubbling `keydown` that is not
 * `defaultPrevented`, against the table `core::menu_keys` makes.
 */
export function installMenuKeys(target, platform) {
  if (platform === 'darwin') return
  let table = []
  invoke('menu_key_bindings')
    .then((bindings) => {
      if (Array.isArray(bindings)) table = bindings
    })
    .catch((err) => console.error('window.agentrq: no menu keys', err))
  target.addEventListener('keydown', (e) => {
    if (e.defaultPrevented || e.isComposing) return
    const hit = table.find((b) => matches(b, e))
    // A held key repeats; only the bindings that say so are meant to.
    if (!hit || (e.repeat && !hit.repeat)) return
    // This listener is on `window`, the last stop of the event, yet a page
    // listener on `window` registered after it runs after it. A task later
    // every listener has run, and `defaultPrevented` stays set on the event.
    setTimeout(() => {
      if (e.defaultPrevented) return
      invoke('menu_run', { id: hit.id }).catch((err) =>
        console.error('window.agentrq: menu key', err)
      )
    }, 0)
  })
}

/** A letter of a script other than Latin: Cyrillic, Greek, Hebrew, Arabic, CJK... */
const NON_LATIN_LETTER = /^(?!\p{Script=Latin})\p{L}$/u

/**
 * A key the menu binds, by what it types, or by where it is when the layout
 * types a letter of another script (`ц` for Ctrl+W on a Cyrillic one). Never by
 * place alone: on AZERTY the key that is `W` on QWERTY types `Z`, and `ß`, `¡`,
 * `´` and dead keys are Latin layouts' own punctuation, not a zoom.
 */
function matches(b, e) {
  if (b.ctrl !== e.ctrlKey || b.shift !== e.shiftKey || b.alt !== e.altKey || b.meta !== e.metaKey) {
    return false
  }
  const key = String(e.key).toLowerCase()
  if (/^[\x21-\x7e]$|^f\d+$/.test(key)) return b.key === key
  return NON_LATIN_LETTER.test(key) && b.code != null && b.code === e.code
}

export function createBridge(userAgent) {
  const versions = { tauri: '', webview: String(userAgent ?? '') }
  getTauriVersion()
    .then((v) => {
      versions.tauri = v
    })
    .catch(() => {})

  return deepFreeze({
    attachments: {
      forgetWorkspace: (workspaceId) => invoke('attachments_forget_workspace', { workspaceId }),
      forgetTask: (workspaceId, taskId) => invoke('attachments_forget_task', { workspaceId, taskId }),
      forgetAll: () => invoke('attachments_forget_all'),
    },

    isDesktop: true,
    platform: platformOf(userAgent),
    versions,

    connection: {
      get: () => invoke('connection_get'),
      validate: (url) => invoke('connection_validate', { url }),
      save: (url) => invoke('connection_save', { url }),
      cancel: () => invoke('connection_cancel'),
    },

    notifications: {
      get: () => invoke('notifications_get'),
      setMuted: (workspaceId, muted) => invoke('notifications_set_muted', { workspaceId, muted }),
    },

    profiles: {
      get: () => invoke('profiles_get'),
      switch: (id) => invoke('profiles_switch', { id }),
      add: (label) => invoke('profiles_add', { label }),
      rename: (id, label) => invoke('profiles_rename', { id, label }),
      remove: (id) => invoke('profiles_remove', { id }),
    },

    files: {
      open: (fileUrl) => invoke('files_open', { fileUrl }),
    },

    clipboard: {
      write: (text) => invoke('clipboard_write', { text }),
    },

    emoji: {
      showPanel: () => invoke('emoji_show_panel'),
    },

    dialog: {
      chooseDirectory: (currentPath) => invoke('dialog_choose_directory', { currentPath }),
    },

    navigation: {
      onNavigate: (callback) => {
        const unsub = subscribe('agentrq:navigate', (route) => callback(route))
        invoke('navigation_ready')
        return unsub
      },
    },

    extensions: {
      state: () => invoke('extensions_state'),
      refresh: () => invoke('extensions_refresh'),
      chooseFolder: () => invoke('extensions_choose_folder'),
      installLocal: (path, { grant = null, config = null } = {}) =>
        invoke('extensions_install_local', { path, grant, config }),
      installFromCatalogue: (fullName, { grant = null, config = null } = {}) =>
        invoke('extensions_install_catalogue', { fullName, grant, config }),
      uninstall: (name) => invoke('extensions_uninstall', { name }),
      setEnabled: (name, enabled) => invoke('extensions_set_enabled', { name, enabled }),
      configure: (name, values) => invoke('extensions_configure', { name, values }),
      setHookConsent: (name, level) => invoke('extensions_set_hook_consent', { name, level }),
      supervisor: () => invoke('extensions_supervisor'),
      authorize: () => invoke('extensions_authorize'),
      deauthorize: () => invoke('extensions_deauthorize'),
      onChanged: (callback) => subscribe('agentrq:extensions:changed', (detail) => callback(detail)),
      entries: (surface, context = {}) => invoke('extensions_entries', { surface, context }),
      drawer: (format) => invoke('extensions_drawer', { format }),
      invoke: (target, context = {}) => invoke('extensions_invoke', { target, context }),
    },

    updates: {
      get: () => invoke('update_get'),
      check: () => invoke('update_check'),
      installNow: () => invoke('update_install'),
      installViaScript: () => invoke('update_install_via_script'),
      onStatus: (callback) => {
        const off = subscribe('agentrq:update:status', (state) => callback(state))
        // A check runs at launch, so the shell may have settled before this
        // listener existed: the current state is delivered immediately.
        invoke('update_get').then(callback).catch(() => {})
        return off
      },
    },

    sidePanel: {
      // The panel's page lives in a native child webview, not a `<webview>`.
      guest: 'tauri',
      onToggle: (callback) => subscribe('agentrq:side-panel:toggle', () => callback()),
      onOpen: (callback) => subscribe('agentrq:side-panel:open', (url) => callback(url)),
      onFind: (callback) => subscribe('agentrq:side-panel:find', () => callback()),
      pages: () => invoke('side_panel_pages'),
      openExternal: (url) => invoke('side_panel_open_external', { url }),
      onPermissionRequest: (callback) =>
        subscribe('agentrq:side-panel:permission-request', (question) => callback(question)),
      onPermissionSettled: (callback) =>
        subscribe('agentrq:side-panel:permission-settled', (id) => callback(id)),
      answerPermission: (id, decision) => invoke('side_panel_permission_answer', { id, decision }),
      permissions: () => invoke('side_panel_permissions'),
      removePermission: (origin, permission) =>
        invoke('side_panel_permission_remove', { origin, permission }),
    },

    view: {
      get: () => invoke('view_get'),
      zoom: (direction) => invoke('view_zoom', { direction }),
      toggleFullScreen: () => invoke('view_fullscreen'),
    },

    theme: {
      set: (theme) => invoke('theme_set', { theme }),
    },
  })
}

// Before the renderer's first request: the app scheme cannot carry these as
// they are (docs/agents/desktop-tauri.md).
installFetch(window)
installEventSource(window, invoke, Channel)
Object.defineProperty(window, 'agentrq', {
  value: createBridge(navigator.userAgent),
  enumerable: true,
  writable: false,
  configurable: false,
})
installMenuKeys(window, platformOf(navigator.userAgent))
