import DOMPurify from 'dompurify';

/**
 * Drawing a diagram from its source, and the two things that makes safe.
 *
 * An extension sends the *source* of a diagram, never markup — that is what the
 * `diagram` node type is for, and why the view vocabulary has nothing that
 * carries HTML. So the drawing happens here, in AgentRQ's own code, the same
 * way `renderMarkdown` already draws untrusted markdown.
 *
 * ## Mermaid produces SVG, and SVG is markup
 *
 * Mermaid takes text and returns an `<svg>` string. That string is ours rather
 * than an extension's, but it is *derived* from text an extension supplied,
 * which was itself usually derived from something else — an agent's message, an
 * issue title, a webhook payload. So it goes through DOMPurify before it
 * reaches the DOM, in SVG mode. That is the same treatment message bodies get,
 * for the same reason: the thing being drawn came from somewhere nobody
 * controls.
 *
 * ## Configuration is not the caller's to choose
 *
 * Mermaid reads `%%{init: ...}%%` directives out of the *source*, and one of
 * the things they can set is `securityLevel`. Left alone, a diagram could turn
 * off mermaid's own HTML escaping from inside the text being drawn. So mermaid
 * is initialised once, here, with `securityLevel: 'strict'` and
 * `htmlLabels: false`, and `%%{init}%%` is refused by the extension before it
 * ever arrives — belt and braces, because the two guards fail differently.
 *
 * ## Loaded when one is actually on screen
 *
 * Mermaid is large. The import is dynamic so the browser build pays nothing
 * for it until a message actually contains a diagram somebody claimed.
 */

/** Set once, the first time a diagram is drawn. */
let mermaidPromise = null;

/** Distinct per diagram: mermaid uses it as an element id while measuring. */
let nextId = 0;

export const THEMES = Object.freeze({ light: 'default', dark: 'dark' });

/**
 * The one mermaid instance, configured before it can be asked to draw.
 *
 * @param {Function} [importer]  Injected so a test does not load mermaid.
 */
export function loadMermaid(importer = () => import('mermaid')) {
  if (!mermaidPromise) {
    mermaidPromise = importer()
      .then((module) => {
        const mermaid = module.default ?? module;
        mermaid.initialize({
          startOnLoad: false,
          // The two that matter, and neither is the caller's to change: strict
          // keeps mermaid escaping its own labels, and HTML labels are what a
          // label would otherwise be able to smuggle markup through.
          securityLevel: 'strict',
          htmlLabels: false,
          flowchart: { htmlLabels: false },
          fontFamily: 'inherit',
        });
        return mermaid;
      })
      .catch((error) => {
        // Cleared, so a failure that was transient — a chunk that did not load
        // on a flaky connection — is retried rather than remembered forever.
        mermaidPromise = null;
        throw error;
      });
  }
  return mermaidPromise;
}

/** For tests, which must not share one instance between them. */
export function resetMermaid() {
  mermaidPromise = null;
  nextId = 0;
}

/**
 * Draws one diagram, or says why it could not be drawn.
 *
 * Never throws. A diagram is one part of a message somebody is reading, and an
 * exception here would take the whole message with it — a malformed diagram
 * should cost its own block and nothing else.
 *
 * @returns {Promise<{ok: true, svg: string} | {ok: false, reason: string}>}
 */
export async function renderDiagram(source, { theme = 'light', importer } = {}) {
  const text = String(source ?? '').trim();
  if (!text) return { ok: false, reason: 'This diagram is empty.' };

  let mermaid;
  try {
    mermaid = await loadMermaid(importer);
  } catch {
    // Said plainly: this is the app's own dependency failing to load, and it is
    // nothing the person reading can do anything about except try again.
    return { ok: false, reason: 'The diagram renderer could not be loaded.' };
  }

  try {
    mermaid.initialize({ theme: THEMES[theme] ?? THEMES.light });
    const { svg } = await mermaid.render(`agentrq-diagram-${nextId++}`, text);
    return { ok: true, svg: sanitiseSvg(svg) };
  } catch (error) {
    // Mermaid's own message, because it names the line and is written for
    // whoever wrote the diagram.
    return { ok: false, reason: firstLine(error?.message) || 'This diagram could not be drawn.' };
  }
}

/**
 * What actually reaches the DOM.
 *
 * DOMPurify in SVG mode, not the default: the default profile drops `<svg>`
 * wholesale. `USE_PROFILES` keeps the SVG vocabulary and still removes scripts,
 * event handlers and `foreignObject` — which is the element that would let
 * arbitrary HTML back in through a diagram label.
 */
export function sanitiseSvg(svg) {
  return DOMPurify.sanitize(String(svg ?? ''), {
    USE_PROFILES: { svg: true, svgFilters: true },
    FORBID_TAGS: ['foreignObject', 'script', 'style'],
    FORBID_ATTR: ['onload', 'onerror', 'onclick'],
  });
}

/** Mermaid's errors are multi-line with a diagram in them; the first line is the fault. */
function firstLine(message) {
  return String(message ?? '').split('\n')[0].trim();
}
