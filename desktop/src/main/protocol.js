// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The app:// protocol handler — the piece that makes the desktop app work at all.
 *
 * The web frontend addresses the API with same-origin *relative* URLs
 * (`src/api.js`) and authenticates with the `at` cookie. A renderer loaded from
 * file:// would make every one of those calls cross-origin, and the backend
 * sends `Access-Control-Allow-Origin: *` without `Allow-Credentials`, so the
 * cookie would never be attached.
 *
 * So the renderer is served from a privileged `app://` scheme and everything
 * under /api, /mcp and /.well-known is forwarded to the configured AgentRQ
 * server from the main process. The forward runs through Electron's `net`
 * module, which uses the session cookie jar, so `Set-Cookie` from a login is
 * stored against the real server host and replayed on later calls. The renderer
 * only ever sees same-origin traffic, and the backend needs no CORS change.
 *
 * Everything here takes its Electron and filesystem dependencies as arguments so
 * the routing rules can be tested in plain Node with no Electron binary.
 */

import { isAttachmentRequest } from '../../../frontend/src/composables/useAttachmentCache.js'
import { DRAWER_CODE_PREFIX, DRAWER_FRAME_PATH, drawerFrameDocument } from './extensions/drawer-frame.js'

/**
 * Path prefixes forwarded to the AgentRQ server. These mirror the backend's own
 * routing in backend/internal/app/app.go — note `/mcp` has no trailing slash
 * there, so both `/mcp` and `/mcp/<workspace>` match.
 */
export const PROXY_PREFIXES = ['/api/', '/mcp', '/.well-known/']

/** Response headers that must not be forwarded to the renderer. */
const STRIPPED_RESPONSE_HEADERS = new Set([
  // The session cookie jar in the main process already stored these against the
  // real server host. Replaying them at the app:// origin would either fail or
  // fork the session into two places.
  'set-cookie',
  // net.fetch has already decoded the body; forwarding the original encoding or
  // length would describe bytes the renderer never receives.
  'content-encoding',
  'content-length',
  // Meaningless now that the renderer sees the response as same-origin.
  'access-control-allow-origin',
  'access-control-allow-credentials',
  'access-control-allow-headers',
  'access-control-allow-methods',
  'access-control-expose-headers',
])

/** Request headers that must not be forwarded to the server. */
const STRIPPED_REQUEST_HEADERS = new Set([
  // Both would carry the app:// origin, which means nothing to the server and
  // can trip origin checks. net.fetch sets its own Host.
  'origin',
  'referer',
  'host',
  'content-length',
])

/** Requests carrying a body, which must be streamed rather than dropped. */
const BODYLESS_METHODS = new Set(['GET', 'HEAD'])

/**
 * How long a forwarded request may take to produce *response headers*.
 *
 * A request that never settles is worse than one that fails. The promise this
 * handler returns is what the renderer's own fetch is waiting on, so a fetch
 * that neither resolves nor rejects leaves every screen behind it waiting
 * forever, with no error raised anywhere — the app is frozen, not broken. A
 * crash of Chromium's network service does exactly that: it abandons the
 * requests that were in flight rather than failing them.
 *
 * The budget covers headers only, and the timer is cleared the moment they
 * arrive, so a long-lived body — the event stream — is never cut off.
 */
export const PROXY_TIMEOUT_MS = 30000

/**
 * The same budget for a request that carries a body.
 *
 * Longer, because the server cannot answer until it has read what is being
 * sent, so the upload is inside the window. Attachments travel as base64 in a
 * JSON body (`frontend/src/api.js`), which puts several megabytes on a link
 * whose speed we do not get to choose — and a reply whose attachment was
 * rejected for being slow is worse than one that took a while.
 */
export const UPLOAD_TIMEOUT_MS = 120000

/** Raced against the fetch, so the handler settles whatever the fetch does. */
const TIMED_OUT = Symbol('timed out')

/** Thrown by `fetchUpstream` when the budget above is spent. */
export class UpstreamTimeout extends Error {
  constructor(ms) {
    super(`no response headers after ${ms}ms`)
    this.name = 'UpstreamTimeout'
  }
}

/**
 * True when a path should be forwarded to the AgentRQ server rather than served
 * from the bundled renderer assets.
 */
export function isProxyPath(pathname) {
  return PROXY_PREFIXES.some((prefix) =>
    prefix.endsWith('/') ? pathname.startsWith(prefix) : pathname === prefix || pathname.startsWith(prefix)
  )
}

/**
 * Decide what to serve for a non-proxied path, mirroring the SPA fallback in
 * backend/internal/app/app.go: a real file wins; otherwise a path that looks
 * like an asset (has an extension that isn't .html) is a 404, and anything else
 * falls back to index.html so vue-router can handle the route.
 *
 * `exists` is passed in rather than probed here so the rule stays a pure
 * function.
 */
export function planStatic(pathname, exists) {
  if (pathname === '/' || pathname === '') return { kind: 'index' }
  if (exists) return { kind: 'file', path: pathname }

  const lastDot = pathname.lastIndexOf('.')
  const lastSlash = pathname.lastIndexOf('/')
  if (lastDot !== -1 && lastDot > lastSlash) {
    const ext = pathname.slice(lastDot)
    if (ext !== '.html') return { kind: 'notFound' }
  }
  return { kind: 'index' }
}

/**
 * Reject paths that must never reach the filesystem.
 *
 * Checked on the *decoded* path, because decoding is what can reintroduce a
 * dangerous character: `%00` survives URL parsing and becomes a null byte.
 *
 * Directory traversal is belt-and-braces here — the URL parser already
 * collapses dot segments, including percent-encoded ones (`%2e%2e` normalises
 * away before this is called). It stays because this function is also the rule
 * any future caller with an unparsed path will reach for.
 */
export function isSafeAssetPath(pathname) {
  if (pathname.includes('\0')) return false
  return !pathname.split('/').includes('..')
}

/**
 * `url.pathname` stays percent-encoded, but the filesystem needs real
 * characters — without decoding, an asset whose name contains a space would be
 * looked up as a literal `%20`.
 *
 * @returns {string|null} null when the escape sequence is malformed.
 */
export function decodeAssetPath(pathname) {
  try {
    return decodeURIComponent(pathname)
  } catch {
    return null
  }
}

export function filterRequestHeaders(headers) {
  const out = new Headers()
  for (const [key, value] of headers) {
    if (!STRIPPED_REQUEST_HEADERS.has(key.toLowerCase())) out.append(key, value)
  }
  return out
}

export function filterResponseHeaders(headers) {
  const out = new Headers()
  for (const [key, value] of headers) {
    if (!STRIPPED_RESPONSE_HEADERS.has(key.toLowerCase())) out.append(key, value)
  }
  return out
}

/**
 * The WebSocket origin for a server, or '' when there is not one yet.
 *
 * The terminal socket is the only thing the renderer opens that does not go
 * through this handler, because Electron's protocol handler forwards HTTP and
 * does not intercept WebSocket upgrades. So it is addressed absolutely, at the
 * configured server, and `connect-src 'self'` — which means this app:// origin
 * — refuses it. That refusal is silent in a way worth knowing about: the
 * WebSocket constructor throws, so nothing is ever connected and no error
 * reaches the page.
 *
 * Only the origin is returned, and only the ws/wss form of it. A policy is not
 * a place to be generous: the server's https origin does not belong in
 * connect-src, because nothing in the renderer is allowed to address it
 * directly.
 */
export function webSocketOrigin(serverUrl) {
  if (!serverUrl) return ''
  let url
  try {
    url = new URL(serverUrl)
  } catch {
    // Before the connection screen is answered this is whatever somebody has
    // typed so far. A policy with a broken entry in it is rejected wholesale
    // by Chromium, which would break far more than the terminal.
    return ''
  }
  if (url.protocol === 'https:') return `wss://${url.host}`
  if (url.protocol === 'http:') return `ws://${url.host}`
  return ''
}

/**
 * Content-Security-Policy for the renderer.
 *
 * `wasm-unsafe-eval` is what lets the transformers.js speech-to-text worker
 * instantiate its WASM module; `worker-src blob:` is how that worker is spawned.
 * Model weights are fetched from the Hugging Face CDN, so those hosts are in
 * connect-src — API traffic needs nothing beyond 'self' because it is proxied
 * through this same origin.
 *
 * img-src carries the sign-in providers' avatar CDNs. `user.picture` is the URL
 * the provider hands back verbatim, pointing at their own host, so 'self' alone
 * silently blocked every profile photo and the sidebar fell back to the
 * initial-letter placeholder — visible only in the desktop build, because the
 * browser build is served without a CSP at all.
 *
 * These are listed host by host rather than opening img-src to `https:`. The
 * point of the policy is that injected markup cannot reach an arbitrary origin,
 * and an <img> to a URL of the attacker's choosing is a working beacon even
 * though it renders nothing.
 *
 * Deliberately absent: COOP/COEP. Cross-origin isolation would unlock
 * multi-threaded WASM, but `require-corp` also blocks every CDN response that
 * lacks a CORP header, including the model weights. Single-threaded inference
 * works; isolation can be revisited if transcription proves too slow.
 *
 * In dev the Vite client needs inline and eval'd script, so the policy is
 * relaxed there and there only.
 *
 * `serverUrl` is here for the terminal socket, and for nothing else — see
 * [webSocketOrigin]. It is why this is rebuilt per request rather than once:
 * the configured server changes when somebody switches profile or answers the
 * connection screen, and a policy baked in at startup would still name the
 * previous one.
 */
export function buildCSP({ dev = false, devServerUrl = '', serverUrl = '' } = {}) {
  const hf = 'https://huggingface.co https://*.hf.co https://cdn-lfs.huggingface.co https://cdn-lfs-us-1.huggingface.co'
  // Google serves avatars from lh3/lh4/lh5.googleusercontent.com and rotates
  // between them; GitHub uses a single host. Both are the providers the app
  // offers, so a new sign-in provider means a new entry here.
  const avatars = 'https://*.googleusercontent.com https://avatars.githubusercontent.com'
  const script = dev ? `'self' 'unsafe-inline' 'unsafe-eval' 'wasm-unsafe-eval'` : `'self' 'wasm-unsafe-eval'`
  // The terminal socket, at the configured server. In dev the local sockets
  // stay listed as well: they cover Vite's HMR client, which is a different
  // connection to a different port.
  const socket = webSocketOrigin(serverUrl)
  const connect = dev
    ? `'self' ${hf} ${devServerUrl} ws://localhost:* ws://127.0.0.1:* ${socket}`.trimEnd()
    : `'self' ${hf} ${socket}`.trimEnd()

  return [
    `default-src 'self'`,
    `script-src ${script}`,
    `style-src 'self' 'unsafe-inline'`,
    `img-src 'self' data: blob: ${avatars}`,
    `font-src 'self' data:`,
    `connect-src ${connect}`,
    `worker-src 'self' blob:`,
    `media-src 'self' blob:`,
    // No plugins, and no way for injected markup to navigate the shell away
    // from the app or embed it in a frame.
    `object-src 'none'`,
    `frame-ancestors 'none'`,
    `base-uri 'self'`,
    `form-action 'self'`,
  ].join('; ')
}

const MIME_TYPES = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.mjs': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.gif': 'image/gif',
  '.webp': 'image/webp',
  '.ico': 'image/x-icon',
  '.woff': 'font/woff',
  '.woff2': 'font/woff2',
  '.ttf': 'font/ttf',
  '.wasm': 'application/wasm',
  '.map': 'application/json; charset=utf-8',
  '.txt': 'text/plain; charset=utf-8',
}

export function mimeTypeFor(pathname) {
  const lastDot = pathname.lastIndexOf('.')
  const lastSlash = pathname.lastIndexOf('/')
  if (lastDot === -1 || lastDot < lastSlash) return 'application/octet-stream'
  return MIME_TYPES[pathname.slice(lastDot).toLowerCase()] ?? 'application/octet-stream'
}

/**
 * Build the `protocol.handle('app', ...)` callback.
 *
 * @param {object} deps
 * @param {() => string} deps.serverUrl        Base URL of the AgentRQ server.
 * @param {typeof fetch} deps.netFetch         Electron's `net.fetch` (session-aware).
 * @param {(p: string) => Promise<boolean>} deps.fileExists
 * @param {(p: string) => Promise<Uint8Array>} deps.readFile
 * @param {string} [deps.devServerUrl]         When set, static assets come from
 *                                             the Vite dev server instead of disk,
 *                                             so HMR works without breaking the
 *                                             same-origin illusion.
 * @param {(method: string, pathname: string) => void} [deps.onRequestProxied]
 *                                             Called with every request forwarded
 *                                             to the server, before the fetch is
 *                                             made. This is the only place the main
 *                                             process sees the renderer's own
 *                                             outgoing calls — notifications.js
 *                                             uses it to recognise a reply/respond
 *                                             this desktop instance just sent.
 */
export function createAppProtocolHandler({
  serverUrl,
  netFetch,
  fileExists,
  readFile,
  devServerUrl = '',
  attachments = null,
  /**
   * The code for a drawer, by format. Injected so the handler does not have to
   * know what an extension is — it serves what it is given.
   */
  drawerFor = async () => ({ ok: false, reason: 'Extensions are unavailable.' }),
  onRequestProxied = () => {},
  proxyTimeoutMs = PROXY_TIMEOUT_MS,
  uploadTimeoutMs = UPLOAD_TIMEOUT_MS,
}) {
  const dev = Boolean(devServerUrl)
  // A function rather than a value: `serverUrl` is answered by the shell and
  // changes under a running app, and the policy names it.
  const csp = () => buildCSP({ dev, devServerUrl, serverUrl: serverUrl() })

  /**
   * `netFetch`, bounded by [PROXY_TIMEOUT_MS].
   *
   * Deliberately a race rather than an abort-and-await: aborting asks the fetch
   * to fail, and a fetch whose network service has gone is exactly the one that
   * may not answer. Racing settles this promise on our own timer, so the
   * renderer always gets a reply. The abort still fires, as best-effort cleanup
   * of a request nobody is waiting for any more.
   */
  async function fetchUpstream(target, init) {
    const ms = init.body ? uploadTimeoutMs : proxyTimeoutMs
    const controller = new AbortController()
    let expire
    const budget = new Promise((resolve) => {
      expire = setTimeout(() => resolve(TIMED_OUT), ms)
    })

    try {
      const result = await Promise.race([netFetch(target, { ...init, signal: controller.signal }), budget])
      if (result !== TIMED_OUT) return abortOnCancel(result, controller)
      controller.abort()
      throw new UpstreamTimeout(ms)
    } finally {
      clearTimeout(expire)
    }
  }

  /**
   * The response, with a body that aborts its fetch when it is cancelled.
   *
   * Closing an EventSource, or leaving a view mid-request, cancels the body this
   * handler returned. Cancelling a `net.fetch` body does not end its request: the
   * connection stays open upstream until the server ends it, and an event stream
   * never does. Over HTTP/1.1 six of them take every connection Chromium allows a
   * host, and every later request queues forever. Only the abort closes it, and
   * `request.signal` cannot stand in: Electron never aborts it.
   */
  function abortOnCancel(res, controller) {
    if (!res.body) return res
    const reader = res.body.getReader()
    const body = new ReadableStream({
      async pull(out) {
        const { done, value } = await reader.read()
        if (done) out.close()
        else out.enqueue(value)
      },
      cancel: (reason) => controller.abort(reason),
    })
    return new Response(body, { status: res.status, statusText: res.statusText, headers: res.headers })
  }

  async function proxyToServer(request, url) {
    onRequestProxied(request.method, url.pathname)

    const base = serverUrl()
    if (!base) {
      // Before the first run's connection screen is answered there is nowhere
      // to forward to. Saying so plainly beats throwing out of the handler.
      return Response.json({ error: 'No AgentRQ server is configured' }, { status: 503 })
    }

    const target = new URL(url.pathname + url.search, base)
    const init = {
      method: request.method,
      headers: filterRequestHeaders(request.headers),
      redirect: 'manual',
    }
    if (!BODYLESS_METHODS.has(request.method.toUpperCase()) && request.body) {
      init.body = request.body
      // Required by fetch when a stream is used as the body.
      init.duplex = 'half'
    }

    let upstream
    try {
      upstream = await fetchUpstream(target.toString(), init)
    } catch (err) {
      // A timeout is its own answer: 502 says the server refused us, and the
      // renderer's retry policy for the two is not the same.
      if (err instanceof UpstreamTimeout) {
        return Response.json({ error: 'The AgentRQ server did not respond in time' }, { status: 504 })
      }
      // The server being unreachable is an ordinary state for a desktop client
      // — it is a normal response to the renderer, not a crashed handler.
      return Response.json(
        { error: 'Cannot reach the AgentRQ server', detail: String(err?.message ?? err) },
        { status: 502 }
      )
    }

    // The body is passed straight through rather than buffered, which is what
    // keeps the SSE event stream live.
    return new Response(upstream.body, {
      status: upstream.status,
      statusText: upstream.statusText,
      headers: filterResponseHeaders(upstream.headers),
    })
  }

  async function serveFromDevServer(pathname, search) {
    // The dev server has its own SPA fallback, so the plan is not applied here.
    let res
    try {
      res = await fetchUpstream(new URL(pathname + search, devServerUrl).toString(), {})
    } catch (err) {
      // Same reasoning as the proxy above: unhandled, this rejects the handler
      // and the renderer is left with a request that never completes.
      return new Response(`Cannot reach the dev server: ${String(err?.message ?? err)}`, { status: 502 })
    }
    const headers = filterResponseHeaders(res.headers)
    if ((headers.get('content-type') ?? '').includes('text/html')) {
      headers.set('content-security-policy', csp())
    }
    return new Response(res.body, { status: res.status, statusText: res.statusText, headers })
  }

  async function serveFromDisk(rawPathname) {
    const pathname = decodeAssetPath(rawPathname)
    if (pathname === null || !isSafeAssetPath(pathname)) {
      return new Response('Forbidden', { status: 403 })
    }

    const plan = planStatic(pathname, await fileExists(pathname))
    if (plan.kind === 'notFound') {
      return new Response('Not Found', { status: 404 })
    }

    const filePath = plan.kind === 'index' ? '/index.html' : plan.path
    const body = await readFile(filePath)
    const headers = new Headers({ 'content-type': mimeTypeFor(filePath) })

    if (plan.kind === 'index') {
      headers.set('content-security-policy', csp())
      // index.html is the SPA entry; a stale copy would pin the app to an old
      // asset graph after an update.
      headers.set('cache-control', 'no-store')
    }
    return new Response(body, { status: 200, headers })
  }

  /**
   * An attachment, served from disk when it has been seen before.
   *
   * Only this path buffers a response body. Everything else is streamed
   * straight through, which is what keeps the SSE event stream live — buffering
   * that would hold the whole stream in memory and deliver none of it.
   *
   * An attachment is safe to buffer because it is capped at a couple of
   * megabytes and is not a stream in any meaningful sense, and it has to be
   * buffered to be written at all.
   */
  async function proxyAttachment(request, url) {
    const hit = await attachments.read(url.pathname).catch(() => null)
    if (hit) {
      const headers = new Headers({ 'content-type': hit.contentType })
      if (hit.contentDisposition) headers.set('content-disposition', hit.contentDisposition)
      return new Response(hit.body, { status: 200, headers })
    }

    const upstream = await proxyToServer(request, url)
    // Only a successful body is worth keeping; an error page cached under an
    // attachment's name would outlive whatever caused it.
    if (upstream.status !== 200) return upstream

    let buffered
    try {
      buffered = await upstream.arrayBuffer()
    } catch {
      // The body could not be read, so there is nothing to serve or to store.
      return new Response('Bad Gateway', { status: 502 })
    }

    const contentType = upstream.headers.get('content-type') ?? ''
    await attachments
      .write(url.pathname, Buffer.from(buffered), {
        contentType,
        contentDisposition: upstream.headers.get('content-disposition') ?? '',
        size: buffered.byteLength,
      })
      .catch(() => {})

    return new Response(buffered, { status: 200, headers: upstream.headers })
  }

  return async function handleAppProtocol(request) {
    const url = new URL(request.url)

    // Before anything else static. This is not a renderer asset — it is the
    // document an extension's drawer runs in, and it carries its own policy
    // rather than the app's, which is the only way a sandboxed frame can run a
    // script at all. See `extensions/drawer-frame.js`.
    if (url.pathname === DRAWER_FRAME_PATH) {
      // Both halves together: the header and the document carry the same nonce,
      // and generating them apart would let them disagree — which refuses the
      // bootstrap and looks exactly like the frame being broken.
      const frame = drawerFrameDocument()
      return new Response(frame.html, {
        status: 200,
        headers: {
          'content-type': 'text/html; charset=utf-8',
          'content-security-policy': frame.csp,
          // Never cached: the document is generated, and a stale copy of the
          // bootstrap is the kind of thing nobody thinks to look for.
          'cache-control': 'no-store',
        },
      })
    }

    // A drawer's code, for the frame to import. Served rather than posted into
    // the frame: a real drawer is megabytes — mermaid bundled is five — and
    // handing that to every frame on the page is not something to do once,
    // let alone per diagram. A URL is fetched once and cached.
    if (url.pathname.startsWith(DRAWER_CODE_PREFIX)) {
      const format = decodeURIComponent(url.pathname.slice(DRAWER_CODE_PREFIX.length).replace(/\.js$/, ''))
      const found = await drawerFor(format)
      if (!found.ok) return new Response(found.reason ?? 'No such drawer', { status: 404 })

      return new Response(found.code, {
        status: 200,
        headers: {
          'content-type': 'text/javascript; charset=utf-8',
          // The frame's origin is opaque, so an ES module import from it is a
          // cross-origin request that needs CORS to be readable at all.
          'access-control-allow-origin': '*',
          // No store: what is installed can change under the app, and a cached
          // drawer would outlive the extension that supplied it.
          'cache-control': 'no-store',
        },
      })
    }

    if (isProxyPath(url.pathname)) {
      if (attachments && request.method === 'GET' && isAttachmentRequest(url.pathname)) {
        return proxyAttachment(request, url)
      }
      return proxyToServer(request, url)
    }
    if (dev) {
      return serveFromDevServer(url.pathname, url.search)
    }
    return serveFromDisk(url.pathname)
  }
}
