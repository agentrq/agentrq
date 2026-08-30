const { contextBridge } = require('electron')

/**
 * The seam between the shared Vue app and the desktop shell.
 *
 * Phase 1 exposes only what the renderer needs to know it is running on the
 * desktop; the notification, deep-link and update bridges arrive with their own
 * phases. Kept deliberately narrow — the renderer runs sandboxed with context
 * isolation, and every addition here is a hole in that boundary.
 */
contextBridge.exposeInMainWorld('agentrq', {
  isDesktop: true,
  platform: process.platform,
  versions: {
    electron: process.versions.electron,
    chrome: process.versions.chrome,
    node: process.versions.node,
  },
})
