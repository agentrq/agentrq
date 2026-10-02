// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

const { contextBridge, ipcRenderer } = require('electron')

/**
 * The preload of every page in the side panel.
 *
 * Set by the main process whatever the `<webview>` element asked for, so a
 * guest can never be given the app's own preload and its bridge. A web page
 * gets nothing at all. An Extension's own page (`agentrq-ext:`) gets one
 * thing: a line to its own Extension — and the main process decides which
 * Extension that is from the page's address, not from anything sent here.
 */
if (location.protocol === 'agentrq-ext:') {
  contextBridge.exposeInMainWorld('agentrq', {
    panel: {
      /**
       * Ask this page's Extension something; resolves with its answer.
       *
       * @returns {Promise<unknown>} rejects with the reason it was refused
       */
      send: async (message) => {
        const result = await ipcRenderer.invoke('agentrq:panel:send', message)
        if (!result?.ok) throw new Error(result?.reason ?? 'The extension did not answer.')
        return result.reply
      },
      /**
       * Called with whatever the Extension `post`s to its pages.
       *
       * @returns {() => void} unsubscribe
       */
      onMessage: (callback) => {
        const listener = (_event, message) => callback(message)
        ipcRenderer.on('agentrq:panel:message', listener)
        return () => ipcRenderer.off('agentrq:panel:message', listener)
      },
    },
  })
}
