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

  theme: {
    /**
     * Tell the shell which theme the app is using, so the native chrome follows
     * the app's own setting rather than the operating system's.
     */
    set: (theme) => ipcRenderer.invoke('agentrq:theme:set', theme),
  },
})
