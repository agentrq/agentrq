// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * A stand-in website for the side panel's guests to visit.
 *
 * WebDriver reaches the app's main webview only: a guest is a child webview of
 * its own, and no command can run script in it or read its address. So what a
 * guest did is read from where it left a record, here: every request it makes
 * is logged, and a page the site serves can report on itself (what it can see,
 * whether it is still alive) by loading `/beacon?...`, which is logged too.
 * Nothing in the app is asked for more than it already does.
 */
import { createServer } from 'node:http'

import { waitFor } from './webdriver.mjs'

/** What a served page tells the site about itself, and keeps telling it. */
const REPORT = (extra = '') => `<script>
  const gid = Math.random().toString(36).slice(2);
  const tell = (what) => { new Image().src = '/beacon?gid=' + gid + '&' + what + '&_=' + Math.random(); };
  tell('hello=1&page=' + location.pathname + '&bridge=' + typeof window.agentrq + '&node=' + typeof require + '/' + typeof process);
  // A destroyed webview cannot beat: this is how a guest's death is seen.
  setInterval(() => tell('beat=1'), 100);
  ${extra}
</script>`

const PAGE = (title, body = '', script = '') =>
  `<!doctype html><html><head><title>${title}</title></head><body><h1>${title}</h1>${body}${REPORT(script)}</body></html>`

/**
 * @typedef {{ path: string, query: URLSearchParams, at: number }} Hit
 * @returns {Promise<{
 *   origin: string,
 *   hits: Hit[],
 *   page: (path: string, html: string) => void,
 *   hit: (what: string, match: (hit: Hit) => boolean, options?: object) => Promise<Hit>,
 *   beacons: (gid?: string) => Hit[],
 *   stop: () => Promise<void>,
 * }>}
 */
export async function startSite() {
  const hits = []
  const pages = new Map([
    ['/page', PAGE('Release notes', '<p><a id="popup" href="/second" target="_blank">Open the next page</a></p>')],
    ['/second', PAGE('Second page')],
    // The page's own script moves it: allowed, and no click is involved.
    ['/redirect', PAGE('Redirecting', '', "location.href = '/second'")],
    // The panel never shows a file.
    // A popup, asked for by script and then by a link: no person clicked, which
    // is what decides whether WebKit makes a window of either.
    ['/popup', PAGE('Asks for a window', '<a id="p" href="/second" target="_blank">x</a>',
      "tell('before=1'); window.open('/second'); document.getElementById('p').click()")],
    ['/file', PAGE('Going to a file', '', "tell('before=1'); location.href = 'file:///etc/passwd'")],
  ])
  const server = createServer((req, res) => {
    const url = new URL(req.url, 'http://localhost')
    hits.push({ path: url.pathname, query: url.searchParams, at: hits.length })
    if (url.pathname === '/beacon') {
      res.writeHead(204)
      return res.end()
    }
    const html = pages.get(url.pathname)
    if (!html) {
      res.writeHead(404)
      return res.end()
    }
    res.writeHead(200, { 'content-type': 'text/html', 'set-cookie': 'site=panel; Path=/' })
    res.end(html)
  })
  await new Promise((resolveListen) => server.listen(0, '127.0.0.1', resolveListen))
  const origin = `http://127.0.0.1:${server.address().port}`
  return {
    origin,
    hits,
    page: (path, html) => pages.set(path, html),
    /** The first hit that `match` accepts (after `since`, a hit's `at`), once there is one. */
    hit: (what, match, { since = -1, timeout = 20000 } = {}) =>
      waitFor(what, () => hits.find((h) => h.at > since && match(h)), { timeout }),
    /** Every beacon, or one page's. */
    beacons: (gid) => hits.filter((h) => h.path === '/beacon' && (!gid || h.query.get('gid') === gid)),
    stop: () => new Promise((resolveStop) => {
      server.closeAllConnections()
      server.close(resolveStop)
    }),
  }
}

/** A hit's own beacon, by what it says. */
export const says = (key, value) => (h) => h.path === '/beacon' && h.query.get(key) === value
