/**
 * Spike: does `EventSource` work against a privileged custom scheme?
 *
 * The whole design rests on the renderer believing it is same-origin, and the
 * frontend's live updates come from `new EventSource('/api/v1/events/stream')`
 * in `useEventBus.js`. Chromium is stricter about EventSource than about fetch,
 * and it was not obvious from the documentation whether a `standard` + `stream`
 * scheme qualifies. If it does not, the fallback is a preload SSE bridge.
 *
 * This answers the transport question in isolation: the SSE stream is generated
 * inside the protocol handler rather than proxied, so nothing depends on a
 * running backend. The stream is shaped exactly like the one the proxy produces
 * — a streaming body with `text/event-stream`.
 *
 * Run with: npm run spike:eventsource
 */
import { app, BrowserWindow, protocol } from 'electron'

protocol.registerSchemesAsPrivileged([
  {
    scheme: 'app',
    privileges: { standard: true, secure: true, supportFetchAPI: true, stream: true, corsEnabled: true },
  },
])

const PAGE = `<!DOCTYPE html><html><body><script>
  const results = { fetch: null, eventSource: null, received: [] };

  function done(verdict) {
    console.log('SPIKE_RESULT ' + JSON.stringify({ verdict, ...results }));
  }

  fetch('/api/v1/ping')
    .then(r => r.text())
    .then(t => { results.fetch = t; })
    .catch(e => { results.fetch = 'ERROR: ' + e.message; });

  let settled = false;
  const es = new EventSource('/api/v1/events/stream');

  es.onopen = () => { results.eventSource = 'open'; };
  es.onmessage = (e) => {
    results.received.push(e.data);
    if (results.received.length >= 3 && !settled) {
      settled = true;
      es.close();
      done('PASS');
    }
  };
  es.onerror = () => {
    if (settled) return;
    settled = true;
    results.eventSource = results.eventSource === 'open' ? 'errored-after-open' : 'failed-to-connect';
    es.close();
    done('FAIL');
  };

  setTimeout(() => { if (!settled) { settled = true; done('TIMEOUT'); } }, 8000);
<\/script></body></html>`

/** An SSE body shaped like the one the proxy hands back from the backend. */
function sseStream() {
  let n = 0
  let timer
  return new ReadableStream({
    start(controller) {
      const encoder = new TextEncoder()
      timer = setInterval(() => {
        n += 1
        controller.enqueue(encoder.encode(`data: {"tick":${n}}\n\n`))
        if (n >= 5) {
          clearInterval(timer)
          controller.close()
        }
      }, 100)
    },
    cancel() {
      clearInterval(timer)
    },
  })
}

app.whenReady().then(() => {
  protocol.handle('app', (request) => {
    const { pathname } = new URL(request.url)

    if (pathname === '/api/v1/events/stream') {
      return new Response(sseStream(), {
        status: 200,
        headers: { 'content-type': 'text/event-stream', 'cache-control': 'no-cache' },
      })
    }
    if (pathname === '/api/v1/ping') {
      return new Response('pong', { status: 200, headers: { 'content-type': 'text/plain' } })
    }
    return new Response(PAGE, { status: 200, headers: { 'content-type': 'text/html; charset=utf-8' } })
  })

  const win = new BrowserWindow({ show: false, webPreferences: { contextIsolation: true, sandbox: true } })

  win.webContents.on('console-message', (event) => {
    const message = typeof event === 'string' ? arguments[2] : event.message
    if (!message?.startsWith?.('SPIKE_RESULT ')) return
    const payload = JSON.parse(message.slice('SPIKE_RESULT '.length))
    console.log('\n─── EventSource over app:// ───')
    console.log(JSON.stringify(payload, null, 2))
    console.log(payload.verdict === 'PASS' ? '\n✓ EventSource works over app://' : '\n✗ EventSource does NOT work over app://')
    app.exit(payload.verdict === 'PASS' ? 0 : 1)
  })

  win.loadURL('app://agentrq/')

  setTimeout(() => {
    console.log('✗ spike timed out with no result')
    app.exit(2)
  }, 15000)
})
