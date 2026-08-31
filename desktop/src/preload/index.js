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
  isDesktop: true,
  platform: process.platform,
  versions: {
    electron: process.versions.electron,
    chrome: process.versions.chrome,
    node: process.versions.node,
  },

  connection: {
    /** @returns {Promise<{configured: boolean, serverUrl: string, locked: boolean}>} */
    get: () => ipcRenderer.invoke('agentrq:connection:get'),
    /** Probe a URL without storing it, so the screen can report before committing. */
    validate: (url) => ipcRenderer.invoke('agentrq:connection:validate', url),
    /** Validate, store, and reload the window on success. */
    save: (url) => ipcRenderer.invoke('agentrq:connection:save', url),
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

  updates: {
    /** @returns {Promise<{status: string, detail: string, version: string, enabled: boolean}>} */
    get: () => ipcRenderer.invoke('agentrq:update:get'),
    /** The menu's "Check for Updates…" also routes through here. */
    check: () => ipcRenderer.invoke('agentrq:update:check'),
    /** Restart into the downloaded version. */
    installNow: () => ipcRenderer.invoke('agentrq:update:install'),

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
