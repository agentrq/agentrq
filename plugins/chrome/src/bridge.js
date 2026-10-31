// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

// The bridge: ISOLATED world, document_start, top frame. It relays between the
// page observer and the service worker.
//
// The channel is a DOM event whose type is a nonce the observer takes before any
// page script runs, so the page can neither listen to it nor forge it. Chrome
// runs document_start scripts in the order of their ids, not of registration,
// so this one's id must sort before the observer's.
;(() => {
  const nonce = crypto.randomUUID()
  document.documentElement.dataset.agentrqNonce = nonce

  // To the service worker. Nobody listening, or an extension reloaded under the
  // page, is not the page's problem.
  const send = (message) => {
    try {
      chrome.runtime.sendMessage(message).catch(() => {})
    } catch {}
  }

  // The page's last tools, for a return from the back/forward cache: its
  // scripts do not run again, so the observer does not announce again.
  let tools = null

  const relayed = new Map([
    ['tools', (message) => (tools = { type: 'site-tools', tools: message.tools })],
    ['result', ({ callId, text, error }) => ({ type: 'site-result', callId, text, error })],
  ])

  document.addEventListener(nonce, (event) => {
    let message
    try {
      message = JSON.parse(event.detail)
    } catch {
      return
    }
    const relay = message?.source === 'agentrq-observer' && relayed.get(message.type)
    if (relay) send(relay(message))
  })

  // The page unloading fails the calls still running in it. It goes the way
  // results go, so a result sent before it, like a navigate tool's, arrives first.
  // Same-document navigations keep the page and its tools, and fire nothing.
  window.addEventListener('pagehide', () => send({ type: 'site-gone' }))
  window.addEventListener('pageshow', (event) => {
    if (event.persisted && tools) send(tools)
  })
  document.addEventListener('prerenderingchange', () => {
    if (tools) send(tools)
  })

  chrome.runtime.onMessage.addListener((message, _sender, sendResponse) => {
    if (message?.type === 'site-tools') return void sendResponse(tools)
    if (message?.type !== 'site-call') return
    const { callId, tool, arguments: args } = message
    const detail = JSON.stringify({ source: 'agentrq-bridge', type: 'call', callId, tool, arguments: args })
    document.dispatchEvent(new CustomEvent(nonce, { detail }))
  })
})()
