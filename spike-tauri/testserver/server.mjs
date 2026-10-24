// Throwaway WP0 spike: a stand-in AgentRQ server for the Tauri checks. Never merge.
import http from 'node:http'
import crypto from 'node:crypto'

const TOKEN = 'tok-123'
const reports = new Map()
const streams = new Set()

function cookies(req) {
  const out = {}
  for (const part of (req.headers.cookie || '').split(';')) {
    const i = part.indexOf('=')
    if (i > 0) out[part.slice(0, i).trim()] = part.slice(i + 1).trim()
  }
  return out
}

function json(res, status, body, headers = {}) {
  res.writeHead(status, { 'content-type': 'application/json', ...headers })
  res.end(JSON.stringify(body))
}

function html(res, body, headers = {}) {
  res.writeHead(200, { 'content-type': 'text/html; charset=utf-8', ...headers })
  res.end(`<!doctype html><meta charset="utf-8"><body>${body}</body>`)
}

async function readBody(req) {
  const chunks = []
  for await (const c of req) chunks.push(c)
  return Buffer.concat(chunks)
}

const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, 'http://x')
  const p = url.pathname
  const signedIn = cookies(req).at === TOKEN

  if (p === '/api/v1/auth/config') return json(res, 200, { providers: ['github'] })
  if (p === '/api/v1/auth/user') return signedIn ? json(res, 200, { email: 'spike@example.com' }) : json(res, 401, { error: 'unauthorized' })
  if (p === '/api/v1/auth/github/login') {
    res.writeHead(302, { location: '/fake-provider' })
    return res.end()
  }
  if (p === '/fake-provider') return html(res, 'provider<script>setTimeout(()=>location.href="/api/v1/auth/github/callback?code=x",200)</script>')
  if (p === '/api/v1/auth/github/callback') {
    res.writeHead(302, { location: '/', 'set-cookie': `at=${TOKEN}; Path=/; HttpOnly; SameSite=Lax; Max-Age=86400` })
    return res.end()
  }
  if (p === '/') return html(res, `<h1 id="home">home</h1><p>signed in: ${signedIn}</p>`)
  if (p === '/whoami') return json(res, 200, { hasAt: signedIn, cookieHeader: req.headers.cookie || '' })
  if (p === '/api/v1/setcookie') return json(res, 200, { ok: true }, { 'set-cookie': 'evil=1; Path=/' })

  if (p === '/api/v1/events/stream') {
    if (!signedIn) return json(res, 401, { error: 'unauthorized' })
    const count = Number(url.searchParams.get('count') || 0)
    const interval = Number(url.searchParams.get('interval') || 250)
    res.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-store', connection: 'keep-alive' })
    res.write(': hello\n\n')
    let seq = 0
    const entry = { res }
    streams.add(entry)
    const t = setInterval(() => {
      seq++
      res.write(`data: ${JSON.stringify({ type: 'tick', seq, sentAt: Date.now() })}\n\n`)
      if (count && seq >= count) { clearInterval(t); streams.delete(entry); res.end() }
    }, interval)
    entry.stop = () => { clearInterval(t); res.destroy() }
    req.on('close', () => { clearInterval(t); streams.delete(entry) })
    return
  }
  if (p === '/api/v1/__ctl/drop') {
    const n = streams.size
    for (const s of streams) s.stop()
    streams.clear()
    return json(res, 200, { dropped: n })
  }
  if (p === '/api/v1/echo') {
    const body = await readBody(req)
    return json(res, 200, {
      method: req.method,
      bytes: body.length,
      sha256: crypto.createHash('sha256').update(body).digest('hex'),
      contentType: req.headers['content-type'] || '',
      origin: req.headers.origin || '',
      cookie: req.headers.cookie || '',
    })
  }
  if (p === '/api/v1/blob') {
    const size = Number(url.searchParams.get('size') || 1024)
    const buf = Buffer.alloc(size, 7)
    res.writeHead(200, { 'content-type': 'application/octet-stream', 'content-length': String(size) })
    return res.end(buf)
  }
  const rep = p.match(/^\/api\/v1\/__report\/([\w-]+)$/)
  if (rep) {
    if (req.method === 'POST') {
      reports.set(rep[1], JSON.parse((await readBody(req)).toString() || 'null'))
      return json(res, 200, { ok: true }, { 'access-control-allow-origin': '*' })
    }
    return json(res, 200, reports.get(rep[1]) ?? null)
  }
  if (p === '/panel') {
    // The side-panel guest: says where it is and whether it carries `at`.
    return html(res, `<div style="background:#2563eb;color:#fff;height:100vh;font:20px sans-serif;padding:8px">panel guest</div>
<script>
async function report(tag){
  localStorage.setItem('panel', 'set-by-panel')
  await fetch('/api/v1/__report/panel-' + tag, {method:'POST', body: JSON.stringify({
    tag, innerWidth, innerHeight, dpr: devicePixelRatio, cookie: document.cookie,
    who: await (await fetch('/whoami')).json(), ls: localStorage.getItem('profile-marker')
  })})
}
report('load'); let n=0; addEventListener('resize', () => report('resize' + (++n)))
</script>`)
  }
  if (p === '/report-whoami') {
    return html(res, `<script>fetch('/whoami').then(r=>r.json()).then(w=>fetch('/api/v1/__report/s4-after',{method:'POST',body:JSON.stringify(w)}))</script>`)
  }
  if (p === '/storage-probe') {
    return html(res, `<script>
const before = localStorage.getItem('who'); const tag = new URL(location.href).searchParams.get('who')
if (tag) localStorage.setItem('who', tag)
fetch('/api/v1/__report/probe-' + (tag||'read'), {method:'POST', body: JSON.stringify({before, cookie: document.cookie})})
</script>`)
  }
  res.writeHead(404)
  res.end('not found')
})

// The terminal's socket: the one connection the renderer makes straight to the server.
server.on('upgrade', (req, socket) => {
  const key = req.headers['sec-websocket-key']
  const accept = crypto.createHash('sha1').update(key + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11').digest('base64')
  socket.write(`HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: ${accept}\r\n\r\n`)
  const msg = Buffer.from('hello')
  socket.write(Buffer.concat([Buffer.from([0x81, msg.length]), msg]))
  socket.on('error', () => {})
})

const port = Number(process.env.PORT || 3917)
server.listen(port, '127.0.0.1', () => console.log(`spike test server on http://127.0.0.1:${port}`))
