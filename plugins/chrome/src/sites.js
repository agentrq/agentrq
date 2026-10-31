// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

/**
 * Site tools in the worker: which pages offer tools, telling the server about
 * the shared ones, and running the calls it sends.
 */
import { ALL_SITES, getServerUrl } from './settings.js'
import { clearRefusal, clearRefusalIfChanged, lastRefusal, listShares, reconcile, refused, remember } from './shares.js'
import { createSocket } from './socket.js'
import { createTabs } from './tabs.js'

export { ALL_SITES }
export const TOOL_WAIT = 20_000
const MATCHES = ['https://*/*', 'http://localhost/*']
const SCRIPT_IDS = ['agentrq-bridge', 'agentrq-observer']

export function installSites(chrome, { run, log, fetchImpl, WebSocketImpl, timers }) {
  const tabs = createTabs(chrome, timers)
  // The tools last announced per site, kept with a refusal of them.
  const announced = new Map()

  const announce = (origin, share) => {
    const live = tabs.entry(tabs.bestTab(origin))
    const { url: lastUrl, tools } = live ?? { url: share.lastUrl, tools: share.tools ?? [] }
    // Cleared before it is sent, so a refusal of this announce cannot be wiped.
    run(async () => {
      await clearRefusal(chrome, origin)
      announced.set(origin, tools)
      socket.send({ type: 'announce', origin, workspaceId: share.workspaceId, lastUrl, tools })
    })
  }

  const runCall = async ({ callId, origin, tool, arguments: args }) => {
    let result
    try {
      let tabId = tabs.bestTab(origin, tool)
      if (tabId === null) {
        const share = (await listShares(chrome))[origin]
        await chrome.tabs.create({ url: share?.lastUrl || origin, active: false })
        tabId = await tabs.waitForTool(origin, tool, TOOL_WAIT)
      }
      const answer = tabs.expect(callId, tabId)
      chrome.tabs
        .sendMessage(tabId, { type: 'site-call', callId, tool, arguments: args })
        .catch((err) => tabs.deliver(callId, tabId, { error: err.message }))
      result = await answer
    } catch (err) {
      result = { error: err.message }
    }
    socket.send(result.error ? { type: 'result', callId, error: result.error } : { type: 'result', callId, text: result.text ?? '' })
  }

  const onFrame = (frame) => {
    if (frame.type === 'shares') run(() => reconcile(chrome, frame.shares))
    else if (frame.type === 'call') run(() => runCall(frame))
    else if (frame.type === 'refused') {
      log.error(`AgentRQ: the server refused ${frame.origin}: ${frame.error}`)
      run(() => refused(chrome, frame.origin, frame.error, announced.get(frame.origin)))
    }
  }

  // Every open re-announces every share: after a worker restart or a reconnect
  // the server's record names another socket, or none.
  const onOpen = () =>
    run(async () => {
      for (const [origin, share] of Object.entries(await listShares(chrome))) announce(origin, share)
    })

  const socket = createSocket({ chrome, fetchImpl, WebSocketImpl, timers, log, onFrame, onOpen, getServer: () => getServerUrl(chrome) })

  const onSiteTools = async (tabId, url, tools, documentId) => {
    const origin = new URL(url).origin
    tabs.set(tabId, origin, url, tools, documentId)
    await clearRefusalIfChanged(chrome, origin, tools)
    if (!(await remember(chrome, origin, url, tools))) return
    announce(origin, (await listShares(chrome))[origin])
  }

  // What the popup's strip shows: the front tab's site, its tools, and the
  // workspace it is shared with. A shared site is reported even while its page
  // offers nothing yet, so the strip never hides a share.
  const popupState = async () => {
    const [front] = await chrome.tabs.query({ active: true, lastFocusedWindow: true })
    const origin = front?.url ? new URL(front.url).origin : null
    const share = origin && (await listShares(chrome))[origin]
    const sharedWith = share?.workspaceId ?? null
    let entry = front && tabs.entry(front.id)
    // After a worker restart the badge still stands, while the tab's tools live
    // only in its bridge until the popup asks for them.
    if (origin && (!entry?.tools.length || origin !== entry.origin)) {
      const offered = await chrome.tabs.sendMessage(front.id, { type: 'site-tools' }).catch(() => null)
      if (offered?.tools?.length) {
        await onSiteTools(front.id, front.url, offered.tools)
        entry = tabs.entry(front.id)
      }
    }
    const refusal = origin && (await lastRefusal(chrome, origin))
    // A tab that has moved to another site keeps the old one's entry until
    // that page says something; it offers nothing yet.
    if (!entry?.tools.length || origin !== entry.origin) {
      if (sharedWith) return { origin, url: front.url, tools: [], toolCount: 0, sharedWith, refusal }
      return { origin: null, url: null, tools: [], toolCount: 0, sharedWith: null, refusal: null }
    }
    return { origin, url: entry.url, tools: entry.tools, toolCount: entry.tools.length, sharedWith, refusal }
  }

  chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
    // Only the extension's own pages ask; a page's scripts send from its URL.
    if (message?.type === 'popup-state' && sender.url?.startsWith(chrome.runtime.getURL(''))) {
      popupState().then(sendResponse, (err) => {
        log.error('AgentRQ: popup state:', err)
        sendResponse(null)
      })
      return true
    }
    const tabId = sender.tab?.id
    if (tabId === undefined) return
    // A page left behind by a cross-site navigation is no longer frame 0.
    if (message?.type === 'site-gone') return void tabs.gone(tabId, sender.documentId)
    if (sender.frameId !== 0) return
    if (message?.type === 'site-tools') run(() => onSiteTools(tabId, sender.url, message.tools ?? [], sender.documentId))
    else if (message?.type === 'site-result') tabs.deliver(message.callId, tabId, { text: message.text, error: message.error })
  })
  chrome.tabs.onActivated.addListener(({ tabId }) => tabs.touch(tabId))
  chrome.tabs.onRemoved.addListener((tabId) => tabs.remove(tabId))

  // A share added, moved or removed, from the popup or by reconcile.
  chrome.storage.onChanged.addListener((changes, area) => {
    if (area !== 'local' || !changes.shares) return
    const before = changes.shares.oldValue ?? {}
    const after = changes.shares.newValue ?? {}
    for (const origin of Object.keys(before)) if (!after[origin]) socket.send({ type: 'withdraw', origin })
    for (const [origin, share] of Object.entries(after)) {
      if (before[origin]?.workspaceId !== share.workspaceId) announce(origin, share)
    }
    if (Object.keys(after).length === 0) socket.close()
    else run(socket.ensure)
  })

  // The observer and its bridge run on every site, and so only while the
  // all-sites access is granted. Bridge first: Chrome orders them by id.
  let scripts = Promise.resolve()
  const syncScripts = () =>
    // One at a time, and one failure does not stop the next.
    (scripts = scripts.catch(() => {}).then(async () => {
      const registered = await chrome.scripting.getRegisteredContentScripts({ ids: SCRIPT_IDS })
      if (registered.length) await chrome.scripting.unregisterContentScripts({ ids: SCRIPT_IDS })
      if (!(await chrome.permissions.contains({ origins: ALL_SITES }))) return
      // The server's own app included: its WebMCP is shared like any site's.
      // The popup's frame of it stays unobserved, being no top frame.
      const script = (id, file, world) => ({ id, js: [file], matches: MATCHES, runAt: 'document_start', allFrames: false, world })
      await chrome.scripting.registerContentScripts([
        script(SCRIPT_IDS[0], 'src/bridge.js', 'ISOLATED'),
        script(SCRIPT_IDS[1], 'src/observer.js', 'MAIN'),
      ])
    }))
  chrome.permissions.onAdded.addListener(() => run(syncScripts))
  chrome.permissions.onRemoved.addListener(() => run(syncScripts))

  run(syncScripts)
  run(socket.ensure)
  return { tabs, socket }
}
