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

/** Set once each, the first time a diagram of that kind is drawn. */
let mermaidPromise = null;
let katexPromise = null;

/** Distinct per diagram: mermaid uses it as an element id while measuring. */
let nextId = 0;

export const THEMES = Object.freeze({ light: 'default', dark: 'dark' });

/**
 * The whole configuration, every time.
 *
 * `mermaid.initialize` **replaces** the configuration rather than merging into
 * it. Calling it with just a theme — which is what setting the theme per
 * diagram looked like it needed — silently put `htmlLabels` back to its default
 * of true, and then every label was rendered as a `foreignObject`: the element
 * that carries arbitrary HTML, and the one the sanitiser strips. The visible
 * result was a diagram with no labels at all.
 *
 * The sanitiser held, which is the point of having two guards. But the
 * configuration this depends on had quietly reset, so it is built whole and
 * passed whole, and there is no partial call anywhere.
 */
export function configFor(theme) {
  return {
    startOnLoad: false,
    // Neither of these is the caller's to change: strict keeps mermaid escaping
    // its own labels, and HTML labels are what a label would otherwise be able
    // to smuggle markup through.
    securityLevel: 'strict',
    htmlLabels: false,
    flowchart: { htmlLabels: false },
    fontFamily: 'inherit',
    theme: THEMES[theme] ?? THEMES.light,
  };
}

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
        mermaid.initialize(configFor('light'));
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
  katexPromise = null;
}

/**
 * KaTeX, loaded with its stylesheet.
 *
 * The CSS is not optional decoration: without it the output is unstyled,
 * overlapping glyphs rather than an equation. It comes along on the dynamic
 * import so it stays out of the main bundle and can never be forgotten
 * separately from the code it styles.
 */
export function loadKatex(
  importer = () => Promise.all([import('katex'), import('katex/dist/katex.css')]).then(([mod]) => mod),
) {
  if (!katexPromise) {
    katexPromise = importer()
      .then((module) => module.default ?? module)
      .catch((error) => {
        katexPromise = null;
        throw error;
      });
  }
  return katexPromise;
}

/** Draws a mermaid diagram. */
async function drawMermaid(text, { theme, importer }) {
  let mermaid;
  try {
    mermaid = await loadMermaid(importer);
  } catch {
    // Said plainly: this is the app's own dependency failing to load, and it is
    // nothing the person reading can do anything about except try again.
    return { ok: false, reason: 'The diagram renderer could not be loaded.' };
  }

  try {
    // The whole configuration, not just the theme — see `configFor`.
    mermaid.initialize(configFor(theme));
    const { svg } = await mermaid.render(`agentrq-diagram-${nextId++}`, text);
    return { ok: true, html: sanitiseSvg(svg) };
  } catch (error) {
    // Mermaid's own message, because it names the line and is written for
    // whoever wrote the diagram.
    return { ok: false, reason: firstLine(error?.message) || 'This diagram could not be drawn.' };
  }
}

/**
 * Draws a TeX expression.
 *
 * Two settings here are load-bearing rather than taste:
 *
 * **`macros` is a fresh object every render.** KaTeX writes `\gdef`
 * definitions into whatever object it is given, so one shared between renders
 * lets a single expression redefine `\alpha` — or anything else — for every
 * equation drawn afterwards, in every message on the page. A module-level
 * `macros` object is the natural thing to write and exactly wrong.
 *
 * **`trust: false`** is what refuses `\href` and `\includegraphics`, so no
 * `<a href>` and no `<img>` are produced at all. The URL text still appears
 * inside the MathML `<annotation>`, which is the echoed source and is inert —
 * that is not a leak to be patched out.
 */
async function drawMath(text, { importer }) {
  let katex;
  try {
    katex = await loadKatex(importer);
  } catch {
    return { ok: false, reason: 'The maths renderer could not be loaded.' };
  }

  try {
    const html = katex.renderToString(text, {
      displayMode: true,
      throwOnError: true,
      macros: {},
      trust: false,
      // Questionable input is drawn rather than warned about on the console of
      // somebody who did not write it.
      strict: false,
    });
    return { ok: true, html: sanitiseMath(html) };
  } catch (error) {
    // KaTeX names the position in the expression, which is written for whoever
    // wrote it.
    return { ok: false, reason: firstLine(error?.message) || 'This expression could not be drawn.' };
  }
}

/**
 * Every format this can draw, and the only place that decides.
 *
 * A registry rather than an allowlist plus a hardwired renderer agreeing with
 * it by hand: adding a format is one entry here, and there is no second place
 * to fall out of step with.
 *
 * The host draws, and only the host — an extension sends source, never markup,
 * because markup from an extension on a privileged origin is the one thing the
 * view vocabulary exists to prevent. So this list is a real limit. What it is
 * not is a *validation* rule: a format nobody draws degrades to showing its
 * source, rather than destroying the view it arrived in.
 */
const DRAWERS = Object.freeze({
  mermaid: drawMermaid,
  math: drawMath,
});

/** The drawer for a format, or null when nothing here draws it. */
export function drawerFor(format) {
  return DRAWERS[String(format ?? '').toLowerCase()] ?? null;
}

/** The formats that will actually draw, for anything that needs to say so. */
export function knownFormats() {
  return Object.keys(DRAWERS);
}

/**
 * Draws one diagram, or says why it could not be drawn.
 *
 * Never throws. A diagram is one part of a message somebody is reading, and an
 * exception here would take the whole message with it — a malformed diagram
 * should cost its own block and nothing else. An unknown format is the same
 * kind of answer: a reason and the source, not a missing block.
 *
 * @returns {Promise<{ok: true, html: string} | {ok: false, reason: string}>}
 */
export async function renderDiagram(format, source, { theme = 'light', importer } = {}) {
  const text = String(source ?? '').trim();
  if (!text) return { ok: false, reason: 'This diagram is empty.' };

  const draw = drawerFor(format);
  if (!draw) {
    // Named, so the author sees which word was wrong, and the block still shows
    // the source underneath — which is what the reader wanted from it anyway.
    return { ok: false, reason: `Nothing here draws \`${format}\`. This app can draw: ${knownFormats().join(', ')}.` };
  }

  return draw(text, { theme, importer });
}

/**
 * What reaches the DOM from KaTeX.
 *
 * A separate pass from `sanitiseSvg`, because KaTeX emits HTML and MathML
 * rather than SVG — that profile alone would strip it to nothing.
 *
 * **All three profiles, measured rather than assumed.** `html` and `mathMl` are
 * the obvious two and they are not enough: KaTeX draws radicals and stretchy
 * delimiters as inline `<svg><path>`, so `\sqrt{x}` came back without its
 * sign — the expression still rendered, silently missing a symbol, which is the
 * worst way for this to be wrong. The inline `style` attributes stay too:
 * KaTeX positions every glyph with them, and removing them leaves an equation
 * in a heap.
 *
 * Turning `svg` on costs nothing here, checked against hostile input: scripts,
 * `<img>`, `<a>`, inline handlers and `foreignObject` are all still removed.
 * `foreignObject` is named explicitly for the same reason it is in
 * `sanitiseSvg` — it is the element that carries arbitrary HTML into SVG.
 */
export function sanitiseMath(html) {
  return DOMPurify.sanitize(String(html ?? ''), {
    USE_PROFILES: { html: true, mathMl: true, svg: true },
    // `trust: false` already refuses the markup that would produce these; this
    // is the second guard, and the two fail differently.
    FORBID_TAGS: ['script', 'style', 'a', 'img', 'iframe', 'object', 'embed', 'foreignObject'],
    FORBID_ATTR: ['onload', 'onerror', 'onclick'],
  });
}

/**
 * A diagram's own stylesheet, with the two things CSS can reach out with gone.
 *
 * Mermaid puts its entire theme — four kilobytes of it — in a `<style>` element
 * inside the SVG: every fill, stroke and label colour. Stripping that element
 * was the first thing this did, and the result was a diagram of solid black
 * boxes with invisible text. The structure was right and nothing could be read.
 *
 * So the element stays and its content is filtered instead. Two things in CSS
 * can make a request or pull in rules from elsewhere:
 *
 * **`@import`** loads another stylesheet, by URL.
 *
 * **`url(...)`** fetches whatever it names — except `url(#id)`, which points at
 * an element in the same document. Mermaid needs exactly that form for its
 * arrowheads, so the internal ones are kept and everything else is dropped.
 *
 * A filter rather than a refusal: a diagram whose theme failed to parse should
 * lose its colours, not its existence.
 */
export function sanitiseCss(css) {
  return String(css ?? '')
    // Everything up to the semicolon or the end of the rule.
    .replace(/@import[^;}]*[;}]?/gi, '')
    // Any url() that is not a same-document reference.
    .replace(/url\(\s*(['"]?)(?!#)[^)]*\1\s*\)/gi, 'none');
}

/**
 * What actually reaches the DOM.
 *
 * DOMPurify in SVG mode, not the default: the default profile drops `<svg>`
 * wholesale. `USE_PROFILES` keeps the SVG vocabulary and still removes scripts,
 * event handlers and `foreignObject` — which is the element that would let
 * arbitrary HTML back in through a diagram label.
 *
 * `<style>` is allowed, because a diagram without its stylesheet is unreadable
 * — see `sanitiseCss` for what is taken out of it. DOMPurify parses stylesheets
 * itself; the filter runs first so the rule is ours and has a test on it rather
 * than being a property of whichever version of a dependency is installed.
 */
export function sanitiseSvg(svg) {
  const filtered = String(svg ?? '').replace(
    /(<style[^>]*>)([\s\S]*?)(<\/style>)/gi,
    (_match, open, css, close) => `${open}${sanitiseCss(css)}${close}`,
  );

  return DOMPurify.sanitize(filtered, {
    USE_PROFILES: { svg: true, svgFilters: true },
    // `style` is deliberately not here; `foreignObject` is what carries HTML.
    FORBID_TAGS: ['foreignObject', 'script'],
    FORBID_ATTR: ['onload', 'onerror', 'onclick'],
  });
}

/** Mermaid's errors are multi-line with a diagram in them; the first line is the fault. */
function firstLine(message) {
  return String(message ?? '').split('\n')[0].trim();
}
