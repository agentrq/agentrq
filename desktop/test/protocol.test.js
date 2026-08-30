import { describe, it, expect, vi } from 'vitest'

import {
  PROXY_PREFIXES,
  isProxyPath,
  planStatic,
  isSafeAssetPath,
  decodeAssetPath,
  filterRequestHeaders,
  filterResponseHeaders,
  buildCSP,
  mimeTypeFor,
  createAppProtocolHandler,
} from '../src/main/protocol.js'

const SERVER = 'http://localhost:3000'

/** Minimal stand-in for the request object Electron hands the protocol handler. */
function makeRequest(url, { method = 'GET', headers = {}, body = null } = {}) {
  return { url, method, headers: new Headers(headers), body }
}

function makeHandler(overrides = {}) {
  const netFetch = overrides.netFetch ?? vi.fn(async () => new Response('upstream', { status: 200 }))
  return {
    netFetch,
    handler: createAppProtocolHandler({
      serverUrl: () => SERVER,
      netFetch,
      fileExists: overrides.fileExists ?? (async () => false),
      readFile: overrides.readFile ?? (async () => new TextEncoder().encode('<html></html>')),
      ...(overrides.devServerUrl !== undefined ? { devServerUrl: overrides.devServerUrl } : {}),
    }),
  }
}

describe('isProxyPath', () => {
  it('forwards the API, MCP and well-known prefixes', () => {
    expect(isProxyPath('/api/v1/workspaces')).toBe(true)
    expect(isProxyPath('/.well-known/oauth-authorization-server')).toBe(true)
  })

  it('matches /mcp both bare and with a workspace segment', () => {
    // The backend registers /mcp without a trailing slash, so both forms route.
    expect(isProxyPath('/mcp')).toBe(true)
    expect(isProxyPath('/mcp/0ZzhYQG2qtl')).toBe(true)
  })

  it('leaves renderer routes alone', () => {
    expect(isProxyPath('/')).toBe(false)
    expect(isProxyPath('/workspaces/abc/board')).toBe(false)
    expect(isProxyPath('/assets/index-abc123.js')).toBe(false)
  })

  it('does not forward a route that merely starts with the word api', () => {
    // '/api/' carries its slash precisely so this stays a renderer route.
    expect(isProxyPath('/apiary')).toBe(false)
  })

  it('exposes the prefixes it uses', () => {
    expect(PROXY_PREFIXES).toEqual(['/api/', '/mcp', '/.well-known/'])
  })
})

describe('planStatic', () => {
  it('serves index.html at the root', () => {
    expect(planStatic('/', false)).toEqual({ kind: 'index' })
    expect(planStatic('', false)).toEqual({ kind: 'index' })
  })

  it('serves a file that exists', () => {
    expect(planStatic('/assets/app.js', true)).toEqual({ kind: 'file', path: '/assets/app.js' })
  })

  it('404s a missing asset rather than handing back the SPA', () => {
    // Mirrors the backend: returning index.html for a missing .js would make
    // the browser parse HTML as a script.
    expect(planStatic('/assets/missing.js', false)).toEqual({ kind: 'notFound' })
    expect(planStatic('/favicon.ico', false)).toEqual({ kind: 'notFound' })
  })

  it('falls back to index.html for SPA routes', () => {
    expect(planStatic('/workspaces/abc/board', false)).toEqual({ kind: 'index' })
    expect(planStatic('/tasks/all', false)).toEqual({ kind: 'index' })
  })

  it('treats .html as a route, not an asset', () => {
    expect(planStatic('/anything.html', false)).toEqual({ kind: 'index' })
  })

  it('ignores a dot that belongs to a directory segment', () => {
    // The dot is before the last slash, so this has no extension.
    expect(planStatic('/v1.2/board', false)).toEqual({ kind: 'index' })
  })
})

describe('isSafeAssetPath', () => {
  it('accepts ordinary asset paths', () => {
    expect(isSafeAssetPath('/assets/index.js')).toBe(true)
    expect(isSafeAssetPath('/a..b/c')).toBe(true)
  })

  it('rejects traversal and null bytes', () => {
    expect(isSafeAssetPath('/../../etc/passwd')).toBe(false)
    expect(isSafeAssetPath('/assets/\0.js')).toBe(false)
  })

  it('exports decodeAssetPath, which reports malformed escapes as null', () => {
    expect(decodeAssetPath('/assets/my%20file.js')).toBe('/assets/my file.js')
    expect(decodeAssetPath('/assets/%zz.js')).toBeNull()
  })
})

describe('filterRequestHeaders', () => {
  it('drops headers that describe the app:// origin', () => {
    const out = filterRequestHeaders(
      new Headers({
        origin: 'app://agentrq',
        referer: 'app://agentrq/login',
        host: 'agentrq',
        'content-length': '12',
        'content-type': 'application/json',
        accept: 'text/event-stream',
      })
    )
    expect(out.get('origin')).toBeNull()
    expect(out.get('referer')).toBeNull()
    expect(out.get('host')).toBeNull()
    expect(out.get('content-length')).toBeNull()
    expect(out.get('content-type')).toBe('application/json')
    expect(out.get('accept')).toBe('text/event-stream')
  })
})

describe('filterResponseHeaders', () => {
  it('withholds Set-Cookie from the renderer', () => {
    // The cookie belongs to the main-process jar; the renderer must not fork it.
    const out = filterResponseHeaders(new Headers({ 'set-cookie': 'at=secret; HttpOnly' }))
    expect(out.get('set-cookie')).toBeNull()
  })

  it('drops encoding and length, which no longer describe the body', () => {
    const out = filterResponseHeaders(
      new Headers({ 'content-encoding': 'gzip', 'content-length': '4096', 'content-type': 'application/json' })
    )
    expect(out.get('content-encoding')).toBeNull()
    expect(out.get('content-length')).toBeNull()
    expect(out.get('content-type')).toBe('application/json')
  })

  it('drops CORS headers that are meaningless once same-origin', () => {
    const out = filterResponseHeaders(
      new Headers({
        'access-control-allow-origin': '*',
        'access-control-allow-credentials': 'true',
        'access-control-allow-headers': 'Origin',
        'access-control-allow-methods': 'GET',
        'access-control-expose-headers': 'mcp-session-id',
        'cache-control': 'no-cache',
      })
    )
    expect([...out.keys()]).toEqual(['cache-control'])
  })
})

describe('buildCSP', () => {
  it('allows WASM and blob workers for the speech-to-text pipeline', () => {
    const csp = buildCSP()
    expect(csp).toContain(`script-src 'self' 'wasm-unsafe-eval'`)
    expect(csp).toContain(`worker-src 'self' blob:`)
    expect(csp).toContain('huggingface.co')
  })

  it('locks down navigation and embedding', () => {
    const csp = buildCSP()
    expect(csp).toContain(`object-src 'none'`)
    expect(csp).toContain(`frame-ancestors 'none'`)
    expect(csp).toContain(`base-uri 'self'`)
    expect(csp).toContain(`form-action 'self'`)
  })

  it('does not permit eval or inline script in production', () => {
    const csp = buildCSP({ dev: false })
    expect(csp).not.toContain(`'unsafe-eval'`)
    expect(csp).not.toContain(`script-src 'self' 'unsafe-inline'`)
  })

  it('relaxes only in dev, where the Vite client needs it', () => {
    const csp = buildCSP({ dev: true, devServerUrl: 'http://localhost:5174' })
    expect(csp).toContain(`'unsafe-eval'`)
    expect(csp).toContain('http://localhost:5174')
    expect(csp).toContain('ws://localhost:*')
  })
})

describe('mimeTypeFor', () => {
  it('maps the types the renderer bundle actually emits', () => {
    expect(mimeTypeFor('/index.html')).toBe('text/html; charset=utf-8')
    expect(mimeTypeFor('/assets/app.js')).toBe('text/javascript; charset=utf-8')
    expect(mimeTypeFor('/assets/app.mjs')).toBe('text/javascript; charset=utf-8')
    expect(mimeTypeFor('/assets/app.css')).toBe('text/css; charset=utf-8')
    expect(mimeTypeFor('/model.wasm')).toBe('application/wasm')
    expect(mimeTypeFor('/inter.woff2')).toBe('font/woff2')
    expect(mimeTypeFor('/FAVICON.SVG')).toBe('image/svg+xml')
  })

  it('falls back to octet-stream when there is no usable extension', () => {
    expect(mimeTypeFor('/noextension')).toBe('application/octet-stream')
    expect(mimeTypeFor('/v1.2/board')).toBe('application/octet-stream')
    expect(mimeTypeFor('/archive.zip')).toBe('application/octet-stream')
  })
})

describe('createAppProtocolHandler — proxying', () => {
  it('forwards an API request to the configured server, preserving the query', async () => {
    const { handler, netFetch } = makeHandler()
    await handler(makeRequest('app://agentrq/api/v1/tasks?limit=10&offset=20'))

    expect(netFetch).toHaveBeenCalledOnce()
    expect(netFetch.mock.calls[0][0]).toBe('http://localhost:3000/api/v1/tasks?limit=10&offset=20')
  })

  it('streams a request body and marks the half-duplex mode fetch requires', async () => {
    const { handler, netFetch } = makeHandler()
    const body = new ReadableStream()
    await handler(makeRequest('app://agentrq/api/v1/workspaces', { method: 'POST', body }))

    const init = netFetch.mock.calls[0][1]
    expect(init.method).toBe('POST')
    expect(init.body).toBe(body)
    expect(init.duplex).toBe('half')
  })

  it('sends no body for GET and HEAD', async () => {
    const { handler, netFetch } = makeHandler()
    await handler(makeRequest('app://agentrq/api/v1/tasks', { method: 'GET', body: new ReadableStream() }))
    await handler(makeRequest('app://agentrq/api/v1/tasks', { method: 'head', body: new ReadableStream() }))

    expect(netFetch.mock.calls[0][1].body).toBeUndefined()
    expect(netFetch.mock.calls[1][1].body).toBeUndefined()
  })

  it('omits the body when a POST carries none', async () => {
    const { handler, netFetch } = makeHandler()
    await handler(makeRequest('app://agentrq/api/v1/tasks', { method: 'POST', body: null }))
    expect(netFetch.mock.calls[0][1].body).toBeUndefined()
  })

  it('handles redirects itself rather than letting fetch follow them', async () => {
    // An OAuth redirect must reach the shell, not be silently followed to a
    // page the renderer cannot display.
    const { handler, netFetch } = makeHandler()
    await handler(makeRequest('app://agentrq/api/v1/auth/github/login'))
    expect(netFetch.mock.calls[0][1].redirect).toBe('manual')
  })

  it('passes the upstream status and body straight through', async () => {
    const netFetch = vi.fn(async () => new Response('{"ok":true}', { status: 201, statusText: 'Created' }))
    const { handler } = makeHandler({ netFetch })

    const res = await handler(makeRequest('app://agentrq/api/v1/tasks', { method: 'POST', body: new ReadableStream() }))
    expect(res.status).toBe(201)
    expect(await res.text()).toBe('{"ok":true}')
  })

  it('reports an unreachable server as 502 instead of throwing', async () => {
    const netFetch = vi.fn(async () => {
      throw new Error('ECONNREFUSED')
    })
    const { handler } = makeHandler({ netFetch })

    const res = await handler(makeRequest('app://agentrq/api/v1/auth/user'))
    expect(res.status).toBe(502)
    expect(await res.json()).toMatchObject({ error: 'Cannot reach the AgentRQ server', detail: 'ECONNREFUSED' })
  })

  it('survives a thrown value that is not an Error', async () => {
    const netFetch = vi.fn(async () => {
      throw 'socket hang up'
    })
    const { handler } = makeHandler({ netFetch })

    const res = await handler(makeRequest('app://agentrq/api/v1/auth/user'))
    expect((await res.json()).detail).toBe('socket hang up')
  })
})

describe('createAppProtocolHandler — bundled assets', () => {
  it('serves index.html at the root with a CSP and no caching', async () => {
    const { handler } = makeHandler({ readFile: async () => new TextEncoder().encode('<html>app</html>') })

    const res = await handler(makeRequest('app://agentrq/'))
    expect(res.status).toBe(200)
    expect(res.headers.get('content-type')).toBe('text/html; charset=utf-8')
    expect(res.headers.get('content-security-policy')).toContain(`default-src 'self'`)
    expect(res.headers.get('cache-control')).toBe('no-store')
    expect(await res.text()).toBe('<html>app</html>')
  })

  it('serves an existing asset with its own type and no CSP header', async () => {
    const readFile = vi.fn(async () => new TextEncoder().encode('body{}'))
    const { handler } = makeHandler({ fileExists: async () => true, readFile })

    const res = await handler(makeRequest('app://agentrq/assets/app.css'))
    expect(res.headers.get('content-type')).toBe('text/css; charset=utf-8')
    expect(res.headers.get('content-security-policy')).toBeNull()
    expect(readFile).toHaveBeenCalledWith('/assets/app.css')
  })

  it('falls back to index.html for a deep SPA route', async () => {
    const readFile = vi.fn(async () => new TextEncoder().encode('<html>app</html>'))
    const { handler } = makeHandler({ readFile })

    const res = await handler(makeRequest('app://agentrq/workspaces/0Zzh/tasks/0hua'))
    expect(res.status).toBe(200)
    expect(readFile).toHaveBeenCalledWith('/index.html')
  })

  it('404s a missing asset', async () => {
    const { handler } = makeHandler()
    const res = await handler(makeRequest('app://agentrq/assets/gone.js'))
    expect(res.status).toBe(404)
  })

  it('refuses a path that decodes to a null byte', async () => {
    // %00 is one of the few sequences that survives URL parsing intact, so it
    // only becomes dangerous at the point the path is decoded for the disk.
    const readFile = vi.fn()
    const { handler } = makeHandler({ fileExists: async () => true, readFile })

    const res = await handler(makeRequest('app://agentrq/assets/%00.js'))
    expect(res.status).toBe(403)
    expect(readFile).not.toHaveBeenCalled()
  })

  it('never sees encoded traversal, because URL parsing collapses it first', async () => {
    // Documents why the traversal guard is defence in depth rather than the
    // primary control: by the time the handler runs, '..' is already gone.
    const readFile = vi.fn(async () => new TextEncoder().encode('x'))
    const fileExists = vi.fn(async () => true)
    const { handler } = makeHandler({ fileExists, readFile })

    await handler(makeRequest('app://agentrq/%2e%2e/%2e%2e/etc/passwd'))
    expect(fileExists).toHaveBeenCalledWith('/etc/passwd')
  })

  it('refuses a path with a malformed escape sequence', async () => {
    const readFile = vi.fn()
    const { handler } = makeHandler({ fileExists: async () => true, readFile })

    const res = await handler(makeRequest('app://agentrq/assets/%zz.js'))
    expect(res.status).toBe(403)
    expect(readFile).not.toHaveBeenCalled()
  })

  it('decodes the path before looking it up on disk', async () => {
    // Without decoding, this asset would be searched for under a literal '%20'.
    const readFile = vi.fn(async () => new TextEncoder().encode('x'))
    const fileExists = vi.fn(async () => true)
    const { handler } = makeHandler({ fileExists, readFile })

    await handler(makeRequest('app://agentrq/assets/my%20file.js'))
    expect(fileExists).toHaveBeenCalledWith('/assets/my file.js')
    expect(readFile).toHaveBeenCalledWith('/assets/my file.js')
  })
})

describe('createAppProtocolHandler — dev server mode', () => {
  it('sources assets from Vite while still proxying the API', async () => {
    const netFetch = vi.fn(async (url) =>
      url.includes('/api/')
        ? new Response('api', { status: 200 })
        : new Response('vite', { status: 200, headers: { 'content-type': 'text/javascript' } })
    )
    const { handler } = makeHandler({ netFetch, devServerUrl: 'http://localhost:5174' })

    await handler(makeRequest('app://agentrq/src/main.js'))
    expect(netFetch.mock.calls[0][0]).toBe('http://localhost:5174/src/main.js')

    await handler(makeRequest('app://agentrq/api/v1/auth/user'))
    expect(netFetch.mock.calls[1][0]).toBe('http://localhost:3000/api/v1/auth/user')
  })

  it('applies the dev CSP to HTML from the dev server', async () => {
    const netFetch = vi.fn(async () => new Response('<html></html>', { headers: { 'content-type': 'text/html' } }))
    const { handler } = makeHandler({ netFetch, devServerUrl: 'http://localhost:5174' })

    const res = await handler(makeRequest('app://agentrq/'))
    expect(res.headers.get('content-security-policy')).toContain(`'unsafe-eval'`)
  })

  it('leaves non-HTML dev responses without a CSP header', async () => {
    const netFetch = vi.fn(async () => new Response('x', { headers: { 'content-type': 'text/javascript' } }))
    const { handler } = makeHandler({ netFetch, devServerUrl: 'http://localhost:5174' })

    const res = await handler(makeRequest('app://agentrq/src/main.js'))
    expect(res.headers.get('content-security-policy')).toBeNull()
  })

  it('tolerates a dev response with no content-type', async () => {
    const netFetch = vi.fn(async () => {
      const r = new Response('x', { status: 200 })
      r.headers.delete('content-type')
      return r
    })
    const { handler } = makeHandler({ netFetch, devServerUrl: 'http://localhost:5174' })

    const res = await handler(makeRequest('app://agentrq/whatever'))
    expect(res.status).toBe(200)
    expect(res.headers.get('content-security-policy')).toBeNull()
  })

  it('forwards the query string to the dev server', async () => {
    const netFetch = vi.fn(async () => new Response('x', { headers: { 'content-type': 'text/javascript' } }))
    const { handler } = makeHandler({ netFetch, devServerUrl: 'http://localhost:5174' })

    await handler(makeRequest('app://agentrq/src/App.vue?vue&type=style'))
    expect(netFetch.mock.calls[0][0]).toBe('http://localhost:5174/src/App.vue?vue&type=style')
  })
})
