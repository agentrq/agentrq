// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The init script of every page in the side panel: `desktop/src/preload/panel.js`
 * for the Tauri guest, which has no IPC at all.
 *
 * Run before the page's own scripts, in the top frame only (wry runs it in
 * subframes too on Windows, hence the check). A web page gets nothing. An
 * Extension's own page (`agentrq-ext:`) gets `window.agentrq.panel`, whose
 * `send` is a `POST` to its own address on that scheme; the shell decides
 * which Extension answers from the guest's own address, the browser-set
 * `Origin` and the token it handed this page once it loaded, never from
 * anything the page says. The token stays in this closure: the page's own
 * scripts can neither read it nor replace the function that receives it.
 *
 * On WebView2, which shows the shell a key before the page sees it, every
 * page also tells the shell when it left a key unhandled, so that a menu key
 * runs only then, as in Electron.
 *
 * `__WEBVIEW2__` is filled in by the shell: on Windows an Extension's page is
 * served as `http://agentrq-ext.<name>/`.
 */
;(() => {
  const webview2 = __WEBVIEW2__
  if (window.top !== window) return
  if (webview2 && window.chrome?.webview) {
    const say = window.chrome.webview.postMessage.bind(window.chrome.webview)
    const later = window.setTimeout.bind(window)
    window.addEventListener(
      'keydown',
      (e) => {
        if (!e.isTrusted || e.isComposing) return
        if (!(e.ctrlKey || e.altKey || e.metaKey || /^F\d+$/.test(e.key))) return
        // After every listener of the page has had it.
        later(() => {
          if (!e.defaultPrevented) say(`agentrq-panel-key:${String(e.key).toLowerCase()}`)
        }, 0)
      },
      true
    )
  }
  const extensionPage = webview2
    ? /^https?:$/.test(location.protocol) && location.hostname.startsWith('agentrq-ext.')
    : location.protocol === 'agentrq-ext:'
  if (!extensionPage) return

  // Taken now, before the page can replace them.
  const post = window.fetch.bind(window)
  const defer = window.setTimeout.bind(window)
  const stringify = JSON.stringify
  const listeners = new Set()
  let token = null
  let tokenArrived
  const arrival = new Promise((resolve) => {
    tokenArrived = resolve
  })
  Object.defineProperty(window, '__agentrqPanelToken', {
    value: (value) => {
      token = String(value)
      tokenArrived()
    },
    writable: false,
    configurable: false,
  })
  // What the Extension posts, handed over by the shell with the token: a
  // listener that throws is reported, and the others still hear it.
  Object.defineProperty(window, '__agentrqPanelMessage', {
    value: (value, message) => {
      if (token === null || String(value) !== token) return
      for (const listener of [...listeners]) {
        try {
          listener(message)
        } catch (error) {
          defer(() => {
            throw error
          }, 0)
        }
      }
    },
    writable: false,
    configurable: false,
  })

  const panel = Object.freeze({
    /**
     * Ask this page's Extension something; resolves with its answer.
     *
     * @returns {Promise<unknown>} rejects with the reason it was refused
     */
    send: async (message) => {
      // Handed over once the page has loaded.
      if (token === null) await arrival
      const response = await post('/.agentrq/panel-send', {
        method: 'POST',
        headers: { 'content-type': 'application/json', 'x-agentrq-panel-token': token },
        body: stringify(message),
      })
      const result = await response.json().catch(() => null)
      if (!result?.ok) throw new Error(result?.reason ?? 'The extension did not answer.')
      return result.reply
    },
    /**
     * Called with whatever the Extension `post`s to its pages.
     *
     * @returns {() => void} unsubscribe
     */
    onMessage: (callback) => {
      listeners.add(callback)
      return () => listeners.delete(callback)
    },
  })

  Object.defineProperty(window, 'agentrq', {
    value: Object.freeze({ panel }),
    enumerable: true,
    writable: false,
    configurable: false,
  })
})()
