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
 */
export function buildCSP({ dev = false, devServerUrl = '' } = {}) {
  const hf = 'https://huggingface.co https://*.hf.co https://cdn-lfs.huggingface.co https://cdn-lfs-us-1.huggingface.co'
  // Google serves avatars from lh3/lh4/lh5.googleusercontent.com and rotates
  // between them; GitHub uses a single host. Both are the providers the app
  // offers, so a new sign-in provider means a new entry here.
  const avatars = 'https://*.googleusercontent.com https://avatars.githubusercontent.com'
  const script = dev ? `'self' 'unsafe-inline' 'unsafe-eval' 'wasm-unsafe-eval'` : `'self' 'wasm-unsafe-eval'`
  const connect = dev ? `'self' ${hf} ${devServerUrl} ws://localhost:* ws://127.0.0.1:*` : `'self' ${hf}`

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
 */
export function createAppProtocolHandler({ serverUrl, netFetch, fileExists, readFile, devServerUrl = '' }) {
  const dev = Boolean(devServerUrl)
  const csp = buildCSP({ dev, devServerUrl })

  async function proxyToServer(request, url) {
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
      upstream = await netFetch(target.toString(), init)
    } catch (err) {
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
    const res = await netFetch(new URL(pathname + search, devServerUrl).toString())
    const headers = filterResponseHeaders(res.headers)
    if ((headers.get('content-type') ?? '').includes('text/html')) {
      headers.set('content-security-policy', csp)
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
      headers.set('content-security-policy', csp)
      // index.html is the SPA entry; a stale copy would pin the app to an old
      // asset graph after an update.
      headers.set('cache-control', 'no-store')
    }
    return new Response(body, { status: 200, headers })
  }

  return async function handleAppProtocol(request) {
    const url = new URL(request.url)

    if (isProxyPath(url.pathname)) {
      return proxyToServer(request, url)
    }
    if (dev) {
      return serveFromDevServer(url.pathname, url.search)
    }
    return serveFromDisk(url.pathname)
  }
}
