// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

/**
 * Running an extension's drawer without letting it near this origin.
 *
 * An extension may ship the code that draws its own format. That code cannot
 * run here: the renderer is on `app://` with the bridge to files, the clipboard
 * and the shell, and code there could call bridge methods nobody granted. The
 * threat is *ungranted capability* rather than untrusted code — an extension
 * already runs in the main process with the machine, but only reaches what the
 * install screen gave it.
 *
 * So a drawer runs in a frame with `sandbox="allow-scripts"` and **no**
 * `allow-same-origin`. That combination gives the document an opaque origin: it
 * cannot read this page, has no bridge, no cookies and no storage. It is handed
 * source over `postMessage`, draws into its own document, and posts back a
 * height. Nothing it produces ever enters this page.
 *
 * ## The one line that must never change
 *
 * Adding `allow-same-origin` makes the frame same-origin with `app://` and the
 * entire argument above collapses — the drawer would be able to reach into this
 * document and through the bridge. `FRAME_SANDBOX` is asserted by a test for
 * exactly that reason.
 *
 * ## Why the drawer's code is posted rather than fetched
 *
 * The frame builds its own blob URL from the text we send it. A blob made out
 * here would carry *this* origin, and handing a privileged-origin URL to
 * sandboxed code is the kind of subtlety that is true until a browser changes
 * its mind. Posting the text keeps the blob, the import and the execution all
 * on the far side of the boundary.
 *
 * ## What the host keeps
 *
 * Only the drawing goes inside. The border, the caption, the format label, the
 * Text/Diagram toggle and the error state stay in `DiagramBlock` — a diagram
 * still looks like every other block because the parts that make it look that
 * way never moved. The frame is transparent and holds a picture.
 */

/**
 * Scripts, and nothing else. **Never add `allow-same-origin`** — see above.
 */
export const FRAME_SANDBOX = 'allow-scripts';

/** How long a drawer gets before the block gives up and shows the source. */
export const DRAW_TIMEOUT_MS = 10000;

/** Messages the frame may send back. Anything else is ignored. */
export const FRAME_MESSAGES = Object.freeze(['drawn', 'failed']);

/**
 * Where the frame document is served.
 *
 * A real URL, deliberately, and the reason is worth keeping: a `srcdoc`,
 * `data:` or `blob:` document **inherits the embedder's CSP**, and this app's
 * is `script-src 'self'`. A sandboxed frame has an opaque origin, for which
 * `'self'` matches nothing — so under an inherited policy no script can run in
 * one at all. A response from a URL carries its own policy instead. The
 * document, and that policy, are in `desktop/src/main/extensions/drawer-frame.js`.
 *
 * **Same origin as the app**, which is not a contradiction: the frame element
 * carries `sandbox="allow-scripts"`, and a sandbox forces an opaque origin
 * whatever the document was served from. Being same-origin is what gets it past
 * the app's own `default-src 'self'` — a frame on another host is refused
 * before it loads, with `ERR_BLOCKED_BY_CSP`.
 */
export const DRAWER_FRAME_URL = 'app://agentrq/__agentrq/drawer-frame.html';

/**
 * Where the frame imports a drawer from.
 *
 * A URL rather than the code itself. A real drawer is megabytes — mermaid
 * bundled is five — and posting that into every frame on the page is not
 * something to do once, let alone per diagram. The browser fetches it once and
 * caches it; the page never holds it at all.
 */
export function drawerUrl(format) {
  return `app://agentrq/__agentrq/drawer/${encodeURIComponent(String(format ?? '').toLowerCase())}.js`;
}

/**
 * Reads a message from the frame, or answers null for one worth ignoring.
 *
 * The frame is sandboxed and its origin is opaque, so `event.origin` is "null"
 * and cannot identify it — the *source* is what does. Every message is checked
 * against the window that was actually created, which is why this takes it.
 */
export function readFrameMessage(event, frameWindow) {
  if (!frameWindow || event?.source !== frameWindow) return null;

  const data = event.data;
  if (!data || typeof data !== 'object') return null;
  if (data.type === 'ready') return { type: 'ready' };
  if (!FRAME_MESSAGES.includes(data.type)) return null;

  if (data.type === 'drawn') {
    const height = Number(data.height);
    return Number.isFinite(height) && height > 0 ? { type: 'drawn', height: Math.min(height, 20000) } : null;
  }

  return { type: 'failed', reason: String(data.reason ?? '').slice(0, 300) || 'This could not be drawn.' };
}

/**
 * Asks whether anything draws a format, once per format.
 *
 * Only the answer crosses, never the code: the frame imports that from a URL.
 * Cached including the failures — a format nothing draws is the common case on
 * any page with an unclaimed fence, and asking again for every block would be a
 * bridge call per diagram per render.
 *
 * `bridge.drawer` is checked rather than `bridge`, the way `useExtensionSurfaces`
 * checks `entries`: an older desktop build has an extensions bridge without this
 * method on it, and calling what is not there is worse than having no drawer.
 */
export function createDrawerSource({ bridge = globalThis.window?.agentrq?.extensions } = {}) {
  const available = Boolean(bridge?.drawer);
  const known = new Map();

  return {
    available,

    /** @returns {Promise<{ok: true, url: string} | {ok: false, reason: string}>} */
    async find(format) {
      const wanted = String(format ?? '').toLowerCase();
      if (!available || !wanted) return { ok: false, reason: '' };

      if (!known.has(wanted)) {
        known.set(
          wanted,
          Promise.resolve(bridge.drawer(wanted))
            .then((result) => (result?.ok ? { ok: true, url: drawerUrl(wanted) } : { ok: false, reason: result?.reason ?? '' }))
            // Never throws: this runs while a message is being drawn, and an
            // exception would take the whole message rather than one block.
            .catch((error) => ({ ok: false, reason: error?.message ?? '' })),
        );
      }
      return known.get(wanted);
    },

    /** After an install or an uninstall, what is drawable has changed. */
    forget() {
      known.clear();
    },
  };
}
