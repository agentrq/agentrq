// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Everything an installed extension runs on, assembled, and the methods the
 * shell calls.
 *
 * Electron's `installExtensions` and `buildExtensionRuntime`
 * (`desktop/src/main/index.js`) and its `agentrq:extensions:*` handlers, moved
 * into the extension host. The modules are desktop's own, imported unchanged:
 * every decision is theirs, and this file only wires them. What Electron took
 * from its main process comes from the shell over the wire instead — a fetch
 * through the profile's jar (`profileFetch`), the keychain (`vault.js`), and
 * the windows only the shell can open.
 */

import { decorate } from '../../../desktop/src/main/extensions/catalogue.js'
import { createDiscovery } from '../../../desktop/src/main/extensions/discovery.js'
import { createInstaller } from '../../../desktop/src/main/extensions/install.js'
import {
  createFetchSource,
  makeTempDir,
  readDrawer,
  readManifest,
} from '../../../desktop/src/main/extensions/fetch-source.js'
import { createHost } from '../../../desktop/src/main/extensions/host.js'
import { createBroker, permitsWorkspace } from '../../../desktop/src/main/extensions/broker.js'
import { createSchedules } from '../../../desktop/src/main/extensions/schedules.js'
import { createConfigStore } from '../../../desktop/src/main/extensions/config.js'
import { createStorageStore } from '../../../desktop/src/main/extensions/storage.js'
import { createRuntime } from '../../../desktop/src/main/extensions/runtime.js'
import { claimedShortcuts } from '../../../desktop/src/main/extensions/shortcuts.js'
import { serverTools } from '../../../desktop/src/main/extensions/servers.js'
import { createMcpClient } from '../../../desktop/src/main/extensions/mcp-client.js'
import { createSupervisorAuth } from '../../../desktop/src/main/extensions/supervisor-auth.js'
import { createToolCallReview } from '../../../desktop/src/main/extensions/tool-calls.js'

import { createVault } from './vault.js'

/** As `core::ext_host::unavailable`, for what a later work package wires up. */
export const unavailable = (what, until) => `${what} is unavailable in this preview until ${until}.`

const fail = (reason) => ({ ok: false, reason })

/** No token: the catalogue is searched as nobody, as in Electron. */
const GITHUB_HEADERS = { Accept: 'application/vnd.github+json', 'User-Agent': 'AgentRQ' }

/** A body for the wire: bytes as base64, or none. */
async function bodyOf(init) {
  if (init?.body === undefined || init.body === null) return null
  return Buffer.from(await new Response(init.body).arrayBuffer()).toString('base64')
}

/** Statuses a `Response` may not carry a body with. */
const NO_BODY = new Set([101, 103, 204, 205, 304])

/**
 * A `fetch` through the shell's profile jar: what Electron's `profileFetch`
 * was, its session's `fetch`.
 */
export function createProfileFetch(rpc) {
  return async function profileFetch(input, init = {}) {
    const answer = await rpc.request('profileFetch', {
      url: String(input),
      method: init.method ?? 'GET',
      headers: Object.entries(Object.fromEntries(new Headers(init.headers ?? {}))),
      body: await bodyOf(init),
    })
    if (!answer?.ok) throw new Error(answer?.reason || 'The request failed.')
    const body = NO_BODY.has(answer.status) ? null : Buffer.from(answer.body ?? '', 'base64')
    return new Response(body, { status: answer.status, headers: answer.headers })
  }
}

/**
 * The `panel` an extension may inject. Opening the side panel and posting to
 * its pages is the shell's, and it is not wired yet; the handler an extension
 * registers is kept for when it is.
 */
export function panelSeam(owner) {
  const reason = unavailable('The side panel', 'WP7b')
  return {
    async open() {
      return fail(reason)
    },
    onMessage(handler) {
      if (typeof handler !== 'function') throw new Error(`${owner}: onMessage needs a function.`)
    },
    post() {
      return fail(reason)
    },
  }
}

/**
 * @param {object} deps
 * @param {{request: Function, notify: Function}} deps.rpc  The wire to the shell.
 * @param {string} deps.dataDir  Electron's `userData`: the app's data directory.
 * @param {string} deps.appVersion
 * @param {(dir: string, name: string) => Promise<any>} deps.load  Imports an extension.
 * @param {(path: string) => {read: Function, write: Function}} deps.jsonFile
 * @param {object} deps.fs  `node:fs/promises`.
 * @param {Function} deps.spawn  `node:child_process`'s.
 * @param {(...parts: string[]) => string} deps.join
 * @param {typeof fetch} [deps.fetchImpl]  For GitHub, which needs no session.
 */
export function createExtensionHost({
  rpc,
  dataDir,
  appVersion,
  load,
  jsonFile,
  fs,
  spawn,
  join,
  fetchImpl = fetch,
  logger = console,
}) {
  let serverUrl = ''
  const tools = () => serverTools(appVersion)
  const jsonStore = (filename) => jsonFile(join(dataDir, filename))
  const profileFetch = createProfileFetch(rpc)
  const sealing = createVault(rpc)

  const cachePath = join(dataDir, 'extensions-catalogue.json')
  const github = (read) => async (url) => {
    const response = await fetchImpl(url, { headers: GITHUB_HEADERS })
    if (response.status === 404) return null
    if (!response.ok) throw new Error(`GitHub responded ${response.status}`)
    return read(response)
  }
  const discovery = createDiscovery({
    fetchJson: github((response) => response.json()),
    fetchText: github((response) => response.text()),
    readFile: () => fs.readFile(cachePath, 'utf8'),
    writeFile: (contents) => fs.writeFile(cachePath, contents, 'utf8'),
  })

  let installedNow = []
  const installer = createInstaller({
    fetchSource: createFetchSource({ spawn, fetchImpl }),
    readManifest,
    move: (from, to) => fs.rename(from, to),
    makeDir: (dir) => fs.mkdir(dir, { recursive: true }),
    remove: (target) => fs.rm(target, { recursive: true, force: true }),
    makeTempDir,
    dirFor: (name) => join(dataDir, 'extensions', name),
    store: jsonStore('extensions-installed.json'),
    claimedKeys: () => claimedShortcuts(installedNow),
  })

  const configStore = createConfigStore({
    store: sealing.wrap(jsonStore('extensions-config.json')),
    vault: sealing.vault,
    logger,
  })

  async function workspaceToken(workspaceId) {
    const response = await profileFetch(`${serverUrl}/api/v1/workspaces/${workspaceId}/token`)
    if (!response.ok) {
      throw new Error(`AgentRQ could not get a token for that workspace (${response.status}).`)
    }
    const token = (await response.json())?.token ?? ''
    if (!token) throw new Error('That workspace did not give up an MCP token for this account.')
    return token
  }

  const workspaceClients = new Map()
  function clientForWorkspace(workspaceId) {
    if (!workspaceClients.has(workspaceId)) {
      workspaceClients.set(
        workspaceId,
        createMcpClient({
          endpoint: () => (serverUrl ? `${serverUrl}/mcp/${workspaceId}` : ''),
          headers: async () => ({ authorization: `Bearer ${await workspaceToken(workspaceId)}` }),
          fetchImpl: profileFetch,
        }),
      )
    }
    return workspaceClients.get(workspaceId)
  }

  const supervisorAuth = createSupervisorAuth({
    serverUrl: () => serverUrl,
    fetchImpl: profileFetch,
    // The shell's window, which answers with the redirect or with nothing.
    openWindow: async (url, redirectUri) => (await rpc.request('authWindow', { url, redirectUri })) ?? null,
  })

  /** Sealed by the shell, never written in the clear; not kept at all without a keychain. */
  const authStore = jsonStore('extensions-supervisor-auth.json')

  async function saveSupervisorAuth() {
    const held = supervisorAuth.saved()
    if (!held.token || !(await sealing.refresh())) return
    try {
      const answer = await rpc.request('seal', { texts: [JSON.stringify(held)] })
      if (answer?.ok) await authStore.write({ sealed: answer.sealed[0] })
    } catch (error) {
      logger.warn?.('could not store the supervisor authorisation:', error?.message ?? error)
    }
  }

  async function restoreSupervisorAuth() {
    try {
      const { sealed } = (await authStore.read()) ?? {}
      if (!sealed) return
      const answer = await rpc.request('open', { blobs: [sealed] })
      if (typeof answer?.opened?.[0] === 'string') supervisorAuth.restore(JSON.parse(answer.opened[0]))
    } catch {
      // From another machine, or no keychain to open it with: ask again.
    }
  }

  const supervisorClient = createMcpClient({
    endpoint: () => (serverUrl ? `${serverUrl}/mcp` : ''),
    headers: async () => ({ authorization: `Bearer ${supervisorAuth.token()}` }),
    fetchImpl: profileFetch,
  })

  const callSupervisor = async ({ tool, args }) => {
    if (!supervisorAuth.authorized()) {
      throw new Error(
        'AgentRQ is not authorised to use account-wide tools yet. Open Extensions and choose Authorise.',
      )
    }
    let answer = await supervisorClient.callTool(tool, args)
    if (!answer.ok && /credentials|401|403/i.test(answer.reason)) {
      const refreshed = await supervisorAuth.refresh()
      if (refreshed.ok) {
        supervisorClient.reset()
        answer = await supervisorClient.callTool(tool, args)
      }
    }
    if (!answer.ok) throw new Error(answer.reason)
    return answer.result
  }

  const broker = createBroker({
    callWorkspace: async ({ workspaceId, tool, args }) => {
      const answer = await clientForWorkspace(workspaceId).callTool(tool, args)
      if (!answer.ok) throw new Error(answer.reason)
      return answer.result
    },
    callSupervisor,
  })

  const storageStore = createStorageStore({
    store: sealing.wrap(jsonStore('extensions-storage.json')),
    vault: sealing.vault,
    logger,
  })

  const host = createHost({
    // The shell is told which extension is loading, so a crash while it loads
    // is held against that one and not every extension running.
    load: async (installation) => {
      rpc.notify('loading', { name: installation.name })
      try {
        return await load(installation.dir, installation.name)
      } finally {
        rpc.notify('loading', { name: null })
      }
    },
    readConfig: (name) => configStore.resolve(name),
    clientFor: (name) => broker.clientFor(name),
    storageFor: (name) => storageStore.for(name),
    capabilities: { panel: panelSeam },
    onDisabled: async (name, reason) => {
      await installer.setEnabled(name, false)
      rpc.notify('changed', { name, reason })
    },
    logger,
  })

  const schedules = createSchedules({
    call: (tool, args) => callSupervisor({ tool, args }),
    allows: (name, workspaceId) => permitsWorkspace(broker.grantFor(name), workspaceId),
    store: jsonStore('extensions-schedules.json'),
  })

  const grantStore = jsonStore('extensions-grants.json')

  const runtime = createRuntime({
    installer,
    host,
    broker,
    schedules,
    configStore,
    storageStore,
    readManifest,
    readDrawer,
    servers: tools,
    logger,
  })

  async function sendReviewVerdict(request, verdict) {
    if (!serverUrl) return fail('Not connected to a server.')
    try {
      const res = await profileFetch(
        `${serverUrl}/api/v1/workspaces/${request.workspaceId}/tasks/${request.taskId}/permission`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ requestId: request.requestId, behavior: verdict.behavior, decidedBy: verdict.by }),
        },
      )
      if (!res.ok) return fail(res.status === 410 ? 'already answered' : `server said ${res.status}`)
      return { ok: true }
    } catch (error) {
      // `profileFetch` throws only `Error`s: the wire's, or the shell's refusal.
      return fail(error.message)
    }
  }

  const toolCallReview = createToolCallReview({
    reviewers: () => runtime.reviewers(),
    grantFor: (name) => broker.grantFor(name),
    allows: permitsWorkspace,
    sendVerdict: sendReviewVerdict,
    onFailure: (name, error) => {
      host.recordFailure(name, error).catch(() => {})
    },
    logger,
  })

  async function persistGrants() {
    try {
      await grantStore.write(runtime.grants())
    } catch {
      // Installed and granted for this run either way.
    }
  }

  /** Changed after a call, so the shell knows what a crash would be held against. */
  const report = () => rpc.notify('running', { names: host.list().map((loaded) => loaded.name) })

  async function catalogueEntry(fullName) {
    const index = await discovery.list()
    return decorate(index, tools()).entries.find((candidate) => candidate.fullName === fullName)
  }

  /** Each method the shell may call, by the name it calls it. */
  const methods = {
    async state() {
      installedNow = await installer.list()
      return { index: decorate(await discovery.list(), tools()), installed: await runtime.state() }
    },
    async refresh() {
      const result = await discovery.refresh()
      return { ...result, index: decorate(result.index, tools()) }
    },
    inspect: ({ path }) => runtime.inspect(path),
    async installLocal({ path, grant = null, config = null }) {
      if (!path) return fail('No folder was chosen.')
      const result = await runtime.installLocal(path, { grant, config })
      await persistGrants()
      return result
    },
    async installFromCatalogue({ fullName, grant = null, config = null }) {
      if (!fullName) return fail('No extension was chosen.')
      // Looked up here, never taken from the page: the entry decides what is
      // downloaded and the digest it is checked against.
      const entry = await catalogueEntry(fullName)
      if (!entry) return fail('That extension is no longer in the catalogue.')
      const result = await runtime.installFromCatalogue(entry, { grant, config })
      await persistGrants()
      return result
    },
    async uninstall({ name }) {
      const result = await runtime.remove(name)
      await persistGrants()
      return result
    },
    setEnabled: ({ name, enabled }) => runtime.setEnabled(name, enabled),
    configure: ({ name, values }) => runtime.configure(name, values ?? {}),
    async setHookConsent({ name, level }) {
      const result = await runtime.setHookConsent(name, level)
      if (result.ok) await persistGrants()
      return result
    },
    supervisor: () => ({ authorized: supervisorAuth.authorized() }),
    async authorize() {
      const result = await supervisorAuth.authorize()
      if (result.ok) await saveSupervisorAuth()
      return result
    },
    async deauthorize() {
      supervisorAuth.forget()
      try {
        await authStore.write({})
      } catch {
        // Forgotten in memory is what stops it being used.
      }
      return { ok: true }
    },
    entries: ({ surface, context }) => runtime.entries(surface ?? '', context ?? {}),
    hasDrawer: ({ format }) => runtime.hasDrawer(format),
    drawerFor: ({ format }) => runtime.drawerFor(format),
    invoke: ({ target, context }) => runtime.invoke(target ?? {}, context ?? {}),
    panelPages: () => runtime.panelPages(),
    panelPagesFor: ({ name }) => runtime.panelPagesFor(name),
    /** Asked about anything pending in an event from the server's stream. */
    async reviewToolCalls({ event }) {
      try {
        await toolCallReview.handle(event)
      } catch (error) {
        logger.warn?.('[extensions] tool-call review failed:', error?.message ?? error)
      }
      return { ok: true }
    },
  }

  /**
   * `start`, once called. A call that arrives while it runs waits for it: one
   * that installed what it was loading would load that extension twice.
   */
  let starting = null

  /** Every method, after the start, telling the shell what is running once it is done. */
  const handlers = Object.fromEntries(
    Object.entries(methods).map(([name, method]) => [
      name,
      async (params) => {
        await Promise.allSettled([starting])
        try {
          return await method(params ?? {})
        } finally {
          report()
        }
      },
    ]),
  )

  return {
    handlers,
    notifications: {
      /** Which server the active profile uses; sent before anything needs it. */
      context(params) {
        const url = params?.serverUrl
        if (typeof url === 'string' && url !== serverUrl) {
          serverUrl = url
          // Clients are per server: a session on the old one is no use.
          workspaceClients.clear()
          supervisorClient.reset()
        }
      },
    },

    /**
     * What runs once the wire is up: Electron's `startAll`.
     *
     * `disable` is what the shell turned off because the host crashed three
     * times running it. Turned off before anything loads, or the same crash
     * would come round again.
     */
    start(disable = []) {
      starting = startAll(disable)
      return starting
    },

    stop: () => runtime.stopAll(),
  }

  async function startAll(disable) {
    await sealing.refresh()
    for (const { name, reason } of disable) {
      await installer.setEnabled(name, false)
      rpc.notify('changed', { name, reason })
    }
    installedNow = await installer.list()
    await restoreSupervisorAuth()
    try {
      const saved = await grantStore.read()
      for (const [name, grant] of Object.entries(saved ?? {})) runtime.rememberGrant(name, grant)
    } catch {
      // Nothing granted yet.
    }
    const results = await runtime.startAll()
    report()
    return results
  }
}
