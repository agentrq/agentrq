// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { randomBytes } from 'node:crypto'

/**
 * The document an extension's drawer runs in.
 *
 * Served by the protocol handler rather than written into a `srcdoc`, and that
 * is not a style choice — it is the only thing that works.
 *
 * ## Why this cannot be a srcdoc, a data: URL or a blob:
 *
 * All three are *local schemes*, and a document loaded from one **inherits the
 * embedder's Content-Security-Policy**. The renderer's policy is
 * `script-src 'self' 'wasm-unsafe-eval'`, so the frame's own bootstrap was
 * refused before it could run:
 *
 *     Executing inline script violates the following Content Security Policy
 *     directive 'script-src 'self' 'wasm-unsafe-eval''
 *
 * And it cannot be worked around by loading the script from a URL instead. A
 * sandboxed frame has an **opaque origin**, and `'self'` in an inherited policy
 * matches nothing for an opaque origin — so under this app's CSP *no* script
 * can run in a sandboxed local-scheme frame at all.
 *
 * A response from a real URL carries its own policy instead of inheriting one.
 * Sandboxing changes the document's origin; it does not stop the response that
 * created it from having headers. So the frame is served from here, with the
 * policy below, and the element still carries `sandbox="allow-scripts"` — which
 * is what keeps the origin opaque and the bridge out of reach.
 *
 * ## What this policy allows, and why each line is there
 *
 * `default-src 'none'` — a drawer reaches nothing by default, the network
 * included. It cannot fetch, cannot beacon, cannot load a font from a CDN.
 *
 * `script-src 'unsafe-inline' blob:` — the bootstrap below is inline, and the
 * drawer itself is imported from a blob the frame makes out of the text it was
 * posted. Both are needed and neither reaches outside the document.
 *
 * `img-src data: blob:` and `font-src data:` — a drawer that produces a picture
 * usually produces it as one of these. Still nothing off the machine.
 */

/** A fresh nonce per document, so only this app's bootstrap runs inline. */
export function newNonce() {
  return randomBytes(16).toString('base64')
}

/** The origin this app is served from, and the only one a frame may import from. */
export const APP_ORIGIN = 'app://agentrq'

/** Where a drawer's code is served. The format is the last segment. */
export const DRAWER_CODE_PREFIX = '/__agentrq/drawer/'

/** The URL a frame imports a drawer from. */
export function drawerCodeUrl(format, origin = APP_ORIGIN) {
  return `${origin}${DRAWER_CODE_PREFIX}${encodeURIComponent(String(format ?? '').toLowerCase())}.js`
}

/** Where the frame document is served. Matched exactly by the handler. */
export const DRAWER_FRAME_PATH = '/__agentrq/drawer-frame.html'

/**
 * The policy the frame document carries, instead of inheriting the app's.
 *
 * **A nonce rather than `'unsafe-inline'`**, which is the practice VS Code's
 * webview guide argues for and it is right: with `'unsafe-inline'` the bootstrap
 * runs, and so would any inline script the drawer injects afterwards. The
 * drawer is already arbitrary code we chose to run, so this is not the wall —
 * but there is no reason to leave a door open beside it, and a fresh nonce per
 * document costs nothing when the document is generated per request anyway.
 *
 * `blob:` stays, separately: it is how the drawer module itself is imported,
 * and an import is not an inline script.
 */
export function drawerFrameCSP(nonce, origin = APP_ORIGIN) {
  return [
    "default-src 'none'",
    // The drawer is imported from a URL this app serves, not posted in as text.
    // A 5 MB bundle — which is what mermaid is — cannot be handed to every
    // frame on the page, and a URL is cached once by the browser instead.
    `script-src 'nonce-${nonce}' ${origin}`,
    "style-src 'unsafe-inline'",
    'img-src data: blob:',
    'font-src data:',
    // Nothing to navigate to, nothing to embed, and no way to reach the
    // network — a drawer cannot send what it was handed anywhere.
    "connect-src 'none'",
    "form-action 'none'",
    "base-uri 'none'",
  ].join('; ')
}

/**
 * What runs inside the frame, before any extension code does.
 *
 * Small on purpose: receive, import, draw, measure, report. The drawer contract
 * is one exported function —
 *
 *     export default function draw(root, source, { theme }) { … }
 *
 * — which draws into `root` and returns nothing, or throws. Anything it puts in
 * that document stays in that document.
 */
const BOOTSTRAP = `
(() => {
  const root = document.getElementById('root');
  let drawer = null;

  const post = (message) => parent.postMessage(message, '*');

  const measure = () => {
    const height = Math.ceil(root.getBoundingClientRect().height);
    post({ type: 'drawn', height: Math.max(height, 1) });
  };

  const fail = (error) => {
    root.replaceChildren();
    const text = error && error.message ? error.message : String(error);
    post({ type: 'failed', reason: String(text).split('\\n')[0] });
  };

  window.addEventListener('message', async (event) => {
    const request = event.data;
    if (!request || request.type !== 'draw') return;

    try {
      if (!drawer) {
        // Imported from the URL the host named. This document's origin is
        // opaque, so the response has to be CORS-readable — the handler says
        // so — and the policy above is what permits this one origin and no
        // other.
        const module = await import(request.url);
        drawer = module.default || module.draw;
        if (typeof drawer !== 'function') throw new Error('This drawer exports no draw function.');
      }

      root.replaceChildren();
      if (request.colour) document.body.style.color = request.colour;
      await drawer(root, request.source, { theme: request.theme });

      // Measured twice, and the reason is not fussiness. An animation frame is
      // the accurate moment -- anything laid out asynchronously has happened by
      // then -- but it never fires in a window that is not painting. A hidden
      // window, a background tab, a minimised app: the callback is simply never
      // called, and a block waiting only for it sits on "Drawing..." until the
      // timeout gives up. So the height is reported straight away, and again on
      // the next frame if one ever comes. The second is a correction, not the
      // answer.
      measure();
      requestAnimationFrame(measure);
    } catch (error) {
      fail(error);
    }
  });

  post({ type: 'ready' });
})();
`

/**
 * The document itself.
 *
 * Everything it needs is inlined — the reset, the colours, the font stack —
 * because the frame cannot read the page's stylesheet and a drawer should
 * inherit this app's look rather than each extension inventing one.
 */
export function drawerFrameDocument({ font = 'system-ui, -apple-system, sans-serif', nonce = newNonce() } = {}) {
  const csp = drawerFrameCSP(nonce)
  const html = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="${csp}">
<style>
  *, *::before, *::after { box-sizing: border-box; }
  html, body { margin: 0; padding: 0; background: transparent; }
  body { font-family: ${font}; font-size: 13px; line-height: 1.5; overflow: hidden; }
  #root { display: inline-block; min-width: 100%; }
  #root svg, #root img { max-width: 100%; height: auto; }
</style>
</head>
<body><div id="root"></div><script nonce="${nonce}">${BOOTSTRAP}</script></body>
</html>`

  // The header and the document carry the *same* nonce. Returned together so
  // they cannot be generated separately and quietly disagree — which would
  // refuse the bootstrap and look exactly like the frame being broken.
  return { html, csp }
}
