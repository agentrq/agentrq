// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

const { contextBridge, ipcRenderer } = require('electron')

/**
 * The seam between the shared Vue app and the desktop shell.
 *
 * Kept deliberately narrow — the renderer runs sandboxed with context
 * isolation, and every addition here is a hole in that boundary. Each method is
 * an explicit request the renderer can make, never a handle to an Electron
 * object.
 */
contextBridge.exposeInMainWorld('agentrq', {
  // The web build clears attachment bytes by messaging its service worker.
  // There is none here, so the renderer asks the main process instead.
  attachments: {
    forgetWorkspace: (workspaceId) =>
      ipcRenderer.invoke('agentrq:attachments:forgetWorkspace', workspaceId),
    forgetTask: (workspaceId, taskId) =>
      ipcRenderer.invoke('agentrq:attachments:forgetTask', workspaceId, taskId),
    forgetAll: () => ipcRenderer.invoke('agentrq:attachments:forgetAll'),
  },

  isDesktop: true,
  platform: process.platform,
  versions: {
    electron: process.versions.electron,
    chrome: process.versions.chrome,
    node: process.versions.node,
  },

  connection: {
    /**
     * `canCancel` is whether this screen has somewhere to go back to — true
     * for a profile that was added and never connected, false on a first run.
     *
     * @returns {Promise<{configured: boolean, serverUrl: string, locked: boolean, canCancel: boolean}>}
     */
    get: () => ipcRenderer.invoke('agentrq:connection:get'),
    /** Probe a URL without storing it, so the screen can report before committing. */
    validate: (url) => ipcRenderer.invoke('agentrq:connection:validate', url),
    /** Validate, store, and reload the window on success. */
    save: (url) => ipcRenderer.invoke('agentrq:connection:save', url),
    /**
     * Discard the profile being set up and return to the previous one. The
     * shell replaces the window, so the caller does not navigate.
     *
     * @returns {Promise<boolean>} false when there was nothing to go back to
     */
    cancel: () => ipcRenderer.invoke('agentrq:connection:cancel'),
  },

  notifications: {
    /** @returns {Promise<{supported: boolean, mutedWorkspaces: string[]}>} */
    get: () => ipcRenderer.invoke('agentrq:notifications:get'),
    setMuted: (workspaceId, muted) =>
      ipcRenderer.invoke('agentrq:notifications:setMuted', workspaceId, muted),

  },

  profiles: {
    /**
     * Signed-in profiles and which one is in use.
     *
     * Names and servers only — a session, a partition or a cookie never
     * crosses this boundary.
     *
     * @returns {Promise<{ activeProfileId: string, profiles: Array<{id: string, label: string, serverUrl: string, active: boolean}> }>}
     */
    get: () => ipcRenderer.invoke('agentrq:profiles:get'),
    /** Switch profiles. The shell replaces the window; the renderer does not reload itself. */
    switch: (id) => ipcRenderer.invoke('agentrq:profiles:switch', id),
    /** Add a profile and switch into it, landing on the connection screen. */
    add: (label) => ipcRenderer.invoke('agentrq:profiles:add', label),
    rename: (id, label) => ipcRenderer.invoke('agentrq:profiles:rename', id, label),
    /** Remove a profile and forget its session. */
    remove: (id) => ipcRenderer.invoke('agentrq:profiles:remove', id),
  },

  files: {
    /**
     * Open a local file that a message linked to.
     *
     * The `file:` URL crosses as-is; the shell decides what may be opened and
     * what is only revealed, because that judgement must not live on the side
     * of the boundary that renders message content.
     *
     * @param {string} fileUrl
     * @returns {Promise<{ok: boolean, revealed?: boolean, error?: string}>}
     *          `revealed` is true when the file was shown in the file manager
     *          rather than opened, so the app can say which happened.
     */
    open: (fileUrl) => ipcRenderer.invoke('agentrq:files:open', fileUrl),
  },

  clipboard: {
    /**
     * Put text on the system clipboard.
     *
     * The renderer has `navigator.clipboard`, but that one refuses to write
     * from a document which is not focused. The shell's has no such condition,
     * so a copy here always lands.
     *
     * @param {string} text
     * @returns {Promise<boolean>}
     */
    write: (text) => ipcRenderer.invoke('agentrq:clipboard:write', text),
  },

  dialog: {
    /**
     * Ask the shell to show the platform's folder chooser.
     *
     * @param {string} [currentPath] where to open the dialog
     * @returns {Promise<string>} the chosen path, or '' if dismissed
     */
    chooseDirectory: (currentPath) => ipcRenderer.invoke('agentrq:dialog:chooseDirectory', currentPath),
  },

  navigation: {
    /**
     * Called with an in-app route whenever the shell asks the app to go
     * somewhere: a notification click, a deep link, the tray, the global
     * shortcut, or the menu.
     *
     * Only the callback crosses the bridge — never the IPC event object, which
     * would hand the renderer a handle back into the main process.
     *
     * @returns {() => void} unsubscribe
     */
    onNavigate: (callback) => {
      const listener = (_event, route) => callback(route)
      ipcRenderer.on('agentrq:navigate', listener)
      return () => ipcRenderer.off('agentrq:navigate', listener)
    },
  },

  extensions: {
    /**
     * What is already known — the cached catalogue, and what is installed.
     *
     * Never searches. GitHub answers ten searches a minute unauthenticated, and
     * spending that on somebody opening a screen would leave nothing for the
     * person who actually asked.
     *
     * @returns {Promise<{index: object, installed: string[]}>}
     */
    state: () => ipcRenderer.invoke('agentrq:extensions:state'),

    /** Go and look again. A deliberate act, from a button. */
    refresh: () => ipcRenderer.invoke('agentrq:extensions:refresh'),

    /**
     * Pick a folder and read what is in it. Installs nothing.
     *
     * Two steps, because a grant is a question: this answers what the extension
     * is and what it would be allowed to reach, and the screen that follows asks
     * before anything is written. A single call would have to install first and
     * ask afterwards, which is not a permission.
     *
     * @returns {Promise<{ok: boolean, cancelled?: boolean, reason?: string, path?: string, manifest?: object, compatible?: boolean, reasons?: string[], shortcutProblems?: string[]}>}
     */
    chooseFolder: () => ipcRenderer.invoke('agentrq:extensions:choose-folder'),

    /** Install what `chooseFolder` found, with what the user agreed to. */
    installLocal: (path, { grant = null, config = null } = {}) =>
      ipcRenderer.invoke('agentrq:extensions:install-local', { path, grant, config }),

    /**
     * Install a catalogue entry, named rather than described.
     *
     * Only the repository name crosses the bridge: the main process resolves it
     * against its own index, so what gets downloaded is decided by what this app
     * discovered, never by what the page says it discovered.
     */
    installFromCatalogue: (fullName, { grant = null, config = null } = {}) =>
      ipcRenderer.invoke('agentrq:extensions:install-catalogue', { fullName, grant, config }),

    /** Remove it, its settings, its grant and anything it scheduled. */
    uninstall: (name) => ipcRenderer.invoke('agentrq:extensions:uninstall', name),

    /** Stop it without removing it — including whatever it had scheduled. */
    setEnabled: (name, enabled) => ipcRenderer.invoke('agentrq:extensions:set-enabled', { name, enabled }),

    /** Save its settings and reload it, so it sees them. */
    configure: (name, values) => ipcRenderer.invoke('agentrq:extensions:configure', { name, values }),

    /**
     * Whether account-wide tools can be used.
     *
     * The supervisor needs an OAuth authorisation the app does not hold until
     * somebody gives it — so this is a question the Extensions screen asks, and
     * `authorize` is the button that answers it.
     */
    supervisor: () => ipcRenderer.invoke('agentrq:extensions:supervisor'),

    /** Ask the user to authorise account-wide access. Opens a window. */
    authorize: () => ipcRenderer.invoke('agentrq:extensions:authorize'),

    /** Give it back. The next account-wide call asks again. */
    deauthorize: () => ipcRenderer.invoke('agentrq:extensions:deauthorize'),

    /**
     * Told when what extensions contribute has changed underneath the app.
     *
     * The host disables an extension after three failures, and nothing about
     * that involves a navigation — so a sidebar row and the key it holds would
     * otherwise stay until the user happened to leave the Extensions screen.
     *
     * @returns {() => void} stop listening
     */
    onChanged: (callback) => {
      const listener = (_event, detail) => callback(detail)
      ipcRenderer.on('agentrq:extensions:changed', listener)
      return () => ipcRenderer.off('agentrq:extensions:changed', listener)
    },

    /**
     * What extensions contribute to one surface, for this context.
     *
     * The context goes *out* rather than the entries coming *in*: an entry's
     * `when(task)` is a function in the main process, and a function cannot
     * cross the bridge. Sending the task is what lets a menu row decide whether
     * it belongs on this particular task instead of every one of them.
     *
     * **`context` must already be a plain object.** `contextBridge` converts
     * arguments as they enter this world and refuses a Proxy outright, so a Vue
     * reactive task rejects here before any code in this file runs — which is
     * why the flattening lives in the caller and cannot be moved down.
     *
     * @param {'page'|'workspace-action'|'task-menu'} surface
     * @returns {Promise<Array<{owner: string, id: string, label: string, order: number}>>}
     */
    entries: (surface, context = {}) => ipcRenderer.invoke('agentrq:extensions:entries', { surface, context }),

    /**
     * The code for a drawer, named by format.
     *
     * Answers with text, which the page hands to a sandboxed frame to run —
     * never to itself. Only a format crosses, so the page cannot name a file.
     */
    drawer: (format) => ipcRenderer.invoke('agentrq:extensions:drawer', format),

    /**
     * Run one, and get back what it wants drawn.
     *
     * @returns {Promise<{ok: boolean, view?: object|null, reason?: string}>}
     */
    invoke: (target, context = {}) => ipcRenderer.invoke('agentrq:extensions:invoke', { target, context }),
  },

  updates: {
    /** @returns {Promise<{status: string, detail: string, version: string, enabled: boolean}>} */
    get: () => ipcRenderer.invoke('agentrq:update:get'),
    /** The menu's "Check for Updates…" also routes through here. */
    check: () => ipcRenderer.invoke('agentrq:update:check'),
    /** Restart into the downloaded version. */
    installNow: () => ipcRenderer.invoke('agentrq:update:install'),

    /**
     * Update by running the one-command installer instead.
     *
     * For the builds that cannot replace themselves — an unsigned macOS app,
     * where Squirrel.Mac refuses to swap the bundle. The installer quits this
     * app, installs, and reopens it, so nothing after this resolves.
     */
    installViaScript: () => ipcRenderer.invoke('agentrq:update:install-via-script'),

    /**
     * Called on every change of update state. Only the state crosses the
     * bridge — never the IPC event object.
     *
     * @returns {() => void} unsubscribe
     */
    onStatus: (callback) => {
      const listener = (_event, state) => callback(state)
      ipcRenderer.on('agentrq:update:status', listener)
      // The shell may have settled on a state before this renderer existed —
      // a check runs at launch — so the current one is delivered immediately.
      ipcRenderer.invoke('agentrq:update:get').then(callback).catch(() => {})
      return () => ipcRenderer.off('agentrq:update:status', listener)
    },
  },

  theme: {
    /**
     * Tell the shell which theme the app is using, so the native chrome follows
     * the app's own setting rather than the operating system's.
     */
    set: (theme) => ipcRenderer.invoke('agentrq:theme:set', theme),
  },
})
