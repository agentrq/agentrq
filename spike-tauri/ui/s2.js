// WP0 S1 + S2: streaming through the scheme, and design A end to end. Never merge.
import { T, report, finish, sleep, median, step } from '/common.js'

const PORT = new URLSearchParams(location.search).get('port')

await step('s2-origin', () => ({
  origin: location.origin,
  href: location.href,
  secureContext: window.isSecureContext,
  cryptoSubtle: !!window.crypto?.subtle,
  clipboard: !!navigator.clipboard,
  mediaDevices: !!navigator.mediaDevices?.getUserMedia,
  serviceWorker: 'serviceWorker' in navigator,
  userAgent: navigator.userAgent,
  localStorage: (() => { localStorage.setItem('k', 'v'); return localStorage.getItem('k') === 'v' })(),
}))

// The jar starts empty: the proxy must answer 401, then 200 once Rust signs in.
await step('s2-auth', async () => {
  const before = (await fetch('/api/v1/auth/user')).status
  const signin = await T.invoke('signin_direct')
  const after = await fetch('/api/v1/auth/user')
  return { before, signin, after: after.status, body: await after.json(), pass: before === 401 && after.status === 200 }
})

// S1: is a response body streamed through the scheme, or buffered?
await step('s1', async () => {
  const t0 = performance.now()
  const res = await fetch('/api/v1/events/stream?count=5&interval=300')
  const tHeaders = performance.now() - t0
  const reader = res.body.getReader()
  const chunks = []
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    chunks.push({ t: Math.round(performance.now() - t0), bytes: value.length })
  }
  const slow0 = performance.now()
  await (await fetch('/__spike/slow')).text()
  return {
    status: res.status,
    tHeadersMs: Math.round(tHeaders),
    chunks,
    // 5 events 300 ms apart: streamed means headers at once and ~5 chunks.
    streamed: tHeaders < 500 && chunks.length > 1,
    slowHandlerMs: Math.round(performance.now() - slow0),
  }
})

await step('s2-bodies', async () => {
  const json = await (await fetch('/api/v1/echo', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ a: 1 }) })).json()
  const big = new Uint8Array(5 * 1024 * 1024).map((_, i) => i % 251)
  const fd = new FormData()
  fd.append('file', new Blob([big]), 'big.bin')
  const t0 = performance.now()
  const up = await (await fetch('/api/v1/echo', { method: 'POST', body: fd })).json()
  const upMs = performance.now() - t0
  const raw = await (await fetch('/api/v1/echo', { method: 'PUT', body: big })).json()
  let rawSha = null
  if (crypto.subtle) rawSha = [...new Uint8Array(await crypto.subtle.digest('SHA-256', big))].map((b) => b.toString(16).padStart(2, '0')).join('')
  const t1 = performance.now()
  const blob = await (await fetch('/api/v1/blob?size=' + 20 * 1024 * 1024)).arrayBuffer()
  const downMs = performance.now() - t1
  return {
    json: { bytes: json.bytes, origin: json.origin, cookieSent: json.cookie.includes('at=') },
    multipart: { bytes: up.bytes, contentType: up.contentType.split(';')[0], ms: Math.round(upMs) },
    raw: { bytes: raw.bytes, shaMatch: rawSha ? rawSha === raw.sha256 : 'no crypto.subtle' },
    download: { bytes: blob.byteLength, ms: Math.round(downMs) },
    pass: json.bytes === 7 && up.bytes > big.length && raw.bytes === big.length && blob.byteLength === 20 * 1024 * 1024,
  }
})

await step('s2-set-cookie-hidden', async () => {
  const res = await fetch('/api/v1/setcookie')
  return { header: res.headers.get('set-cookie'), documentCookie: document.cookie, pass: !res.headers.get('set-cookie') && !document.cookie.includes('evil') }
})

await step('s2-websocket', () => new Promise((resolve) => {
  const port = PORT || '3917'
  let ws
  try { ws = new WebSocket(`ws://127.0.0.1:${port}/ws`) } catch (e) { return resolve({ pass: false, error: String(e) }) }
  const t = setTimeout(() => resolve({ pass: false, error: 'timeout' }), 4000)
  ws.onmessage = (e) => { clearTimeout(t); resolve({ pass: e.data === 'hello' }); ws.close() }
  ws.onerror = () => { clearTimeout(t); resolve({ pass: false, error: 'onerror' }) }
}))

// S2: frontend/src/useEventBus.js, unchanged, over the shim.
const { useEventBus } = await import('/src/useEventBus.js')
const bus = useEventBus(undefined, { buffer: false })
const got = []
bus.onEvent((e) => got.push({ ...e, at: Date.now() }))
bus.connect()
await sleep(2500)
const first = got.length
const latency = median(got.map((e) => e.at - e.sentAt))
await report('s2-eventbus', { events: first, connected: bus.isConnected.value, medianLatencyMs: latency, pass: first >= 5 && bus.isConnected.value })

// The server drops the stream: useEventBus checks auth (200) and reconnects.
await fetch('/api/v1/__ctl/drop')
await sleep(3500)
const resumed = got.slice(first)
await report('s2-eventbus-reconnect', {
  eventsAfterDrop: resumed.length,
  seqRestarted: resumed[0]?.seq === 1,
  connected: bus.isConnected.value,
  pass: resumed.length >= 3 && bus.isConnected.value,
})

// Signed out: the next error must send the page to /login (login.html finishes).
await T.invoke('jar_clear')
await fetch('/api/v1/__ctl/drop')
await sleep(5000)
await report('s2-401-redirect', { pass: false, path: location.pathname })
await finish()
