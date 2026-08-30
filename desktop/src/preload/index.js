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
})
