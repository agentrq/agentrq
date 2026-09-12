import { describe, it, expect, vi, afterEach } from 'vitest';

import katex from 'katex';

import {
  THEMES,
  configFor,
  loadMermaid,
  renderDiagram,
  drawerFor,
  knownFormats,
  loadKatex,
  resetMermaid,
  sanitiseMath,
  sanitiseCss,
  sanitiseSvg,
} from '../src/composables/useDiagram';

/**
 * Drawing a diagram from its source.
 *
 * An extension sends source, never markup — so the drawing happens here, in
 * AgentRQ's own code. Mermaid then produces SVG, which *is* markup, derived
 * from text that usually came from somewhere nobody controls. The sanitiser is
 * what makes that safe, and it is the thing in this file worth reading twice.
 */

const fakeMermaid = (over = {}) => ({
  initialize: vi.fn(),
  render: vi.fn(async (id, source) => ({ svg: `<svg data-id="${id}"><text>${source}</text></svg>` })),
  ...over,
});

const importing = (mermaid) => () => Promise.resolve({ default: mermaid });

afterEach(() => {
  resetMermaid();
});

describe('sanitiseSvg', () => {
  it('keeps an ordinary diagram intact', () => {
    const svg = '<svg viewBox="0 0 10 10"><g><path d="M0 0L10 10"/><text>A</text></g></svg>';

    expect(sanitiseSvg(svg)).toContain('<path');
    expect(sanitiseSvg(svg)).toContain('<text>A</text>');
  });

  it('removes a script smuggled into a diagram', () => {
    const clean = sanitiseSvg('<svg><script>alert(1)</script><text>A</text></svg>');

    expect(clean).not.toContain('script');
    expect(clean).toContain('<text>A</text>');
  });

  /**
   * The element that matters most. `foreignObject` embeds arbitrary HTML inside
   * an SVG, which is how a diagram *label* would otherwise become markup on a
   * privileged origin.
   */
  it('removes foreignObject, which is how HTML gets back in', () => {
    const clean = sanitiseSvg('<svg><foreignObject><img src=x onerror="alert(1)"></foreignObject></svg>');

    expect(clean).not.toContain('foreignObject');
    expect(clean).not.toContain('onerror');
  });

  it('removes event handlers left on a shape', () => {
    const clean = sanitiseSvg('<svg><rect onclick="alert(1)" onload="alert(2)" width="10"/></svg>');

    expect(clean).not.toContain('onclick');
    expect(clean).not.toContain('onload');
    expect(clean).toContain('width="10"');
  });

  it('has nothing to say about nothing', () => {
    expect(sanitiseSvg('')).toBe('');
    expect(sanitiseSvg(undefined)).toBe('');
  });
});

/**
 * Mermaid puts its entire theme — four kilobytes of it — in a `<style>` element
 * inside the SVG. Stripping that element was the first thing this did, and the
 * result was a diagram of solid black boxes with invisible labels: the
 * structure right and nothing readable.
 */
describe('the stylesheet a diagram carries', () => {
  const withStyle = (css) => `<svg id="d"><style>${css}</style><g><rect width="60"/><text>A</text></g></svg>`;

  it('is kept, because a diagram without it cannot be read', () => {
    const clean = sanitiseSvg(withStyle('#d .node rect{fill:#ECECFF;stroke:#9370DB;}'));

    expect(clean).toContain('<style');
    expect(clean).toContain('fill:#ECECFF');
    expect(clean).toContain('<text>A</text>');
  });

  // Mermaid points its arrowheads at markers in the same document, so this
  // exact form has to survive or every edge loses its head.
  it('keeps a same-document reference', () => {
    expect(sanitiseSvg(withStyle('.edge{marker-end:url(#arrowhead);}'))).toContain('url(#arrowhead)');
  });

  it('drops a stylesheet pulled in from somewhere else', () => {
    const clean = sanitiseSvg(withStyle('@import url(http://elsewhere/x.css); .a{fill:red;}'));

    expect(clean).not.toContain('@import');
    expect(clean).not.toContain('elsewhere');
    // And keeps the rules either side of it.
    expect(clean).toContain('fill:red');
  });

  it('drops a request made through a url', () => {
    const clean = sanitiseSvg(withStyle('.a{background:url("http://elsewhere/x.png");}'));

    expect(clean).not.toContain('elsewhere');
  });
});

describe('configFor', () => {
  // Every call carries the whole thing, because a partial one resets the rest.
  it('always carries the guards, whatever theme is asked for', () => {
    for (const theme of ['light', 'dark', 'neon', undefined]) {
      expect(configFor(theme)).toMatchObject({
        securityLevel: 'strict',
        htmlLabels: false,
        flowchart: { htmlLabels: false },
        startOnLoad: false,
      });
    }
  });

  it('names the theme mermaid knows, not the one we call it', () => {
    expect(configFor('dark').theme).toBe(THEMES.dark);
    expect(configFor('light').theme).toBe(THEMES.light);
    expect(configFor('neon').theme).toBe(THEMES.light);
  });
});

describe('the label a node carries', () => {
  // With HTML labels off, mermaid writes a <text>. If the configuration ever
  // resets, it writes a <foreignObject> instead — which the sanitiser removes,
  // and the diagram arrives with nothing written on it.
  it('survives sanitising as text', () => {
    const svg = '<svg><g class="node"><rect width="60"/><text><tspan>A</tspan></text></g></svg>';

    const clean = sanitiseSvg(svg);

    expect(clean).toContain('<text');
    expect(clean).toContain('A');
  });

  it('is removed when it arrives as embedded HTML, which is the point', () => {
    const svg = '<svg><g class="node"><foreignObject><div>A</div></foreignObject></g></svg>';

    expect(sanitiseSvg(svg)).not.toContain('foreignObject');
  });
});

describe('sanitiseCss', () => {
  it('leaves an ordinary stylesheet alone', () => {
    const css = '#d{font-size:16px;fill:#333;}#d .node{stroke-width:1px;}';

    expect(sanitiseCss(css)).toBe(css);
  });

  it('takes out every form of reaching outside the document', () => {
    expect(sanitiseCss('@IMPORT "x.css";a{fill:red}')).not.toContain('@IMPORT');
    expect(sanitiseCss("a{b:url('http://x/y')}")).toBe('a{b:none}');
    expect(sanitiseCss('a{b:url( http://x/y )}')).toBe('a{b:none}');
    expect(sanitiseCss('a{b:URL(//x/y)}')).toBe('a{b:none}');
  });

  it('has nothing to say about nothing', () => {
    expect(sanitiseCss('')).toBe('');
    expect(sanitiseCss(undefined)).toBe('');
  });
});

describe('renderDiagram', () => {
  it('draws one, and sanitises what came back', async () => {
    const mermaid = fakeMermaid();

    const result = await renderDiagram('mermaid', 'graph TD;\n  A-->B;', { importer: importing(mermaid) });

    expect(result.ok).toBe(true);
    expect(result.html).toContain('<svg');
    expect(mermaid.render).toHaveBeenCalledWith(expect.stringContaining('agentrq-diagram-'), 'graph TD;\n  A-->B;');
  });

  /**
   * Mermaid reads `%%{init: ...}%%` out of the source, and one of the things it
   * can set is `securityLevel` — so a diagram could otherwise turn off
   * mermaid's own escaping from inside the text being drawn. Set here, and
   * refused by the extension too: the two guards fail differently.
   */
  it('configures mermaid strictly, whatever the source asks for', async () => {
    const mermaid = fakeMermaid();

    await renderDiagram('mermaid', 'graph TD;', { importer: importing(mermaid) });

    const config = mermaid.initialize.mock.calls[0][0];
    expect(config.securityLevel).toBe('strict');
    expect(config.htmlLabels).toBe(false);
    expect(config.startOnLoad).toBe(false);
  });

  /**
   * The bug this test exists for, and it was not cosmetic.
   *
   * `mermaid.initialize` **replaces** the configuration rather than merging
   * into it, so setting the theme with a partial call put `htmlLabels` back to
   * its default of true. Every label then became a `foreignObject` — the
   * element that carries arbitrary HTML — and the sanitiser stripped them,
   * leaving a diagram with no labels at all. The guard held; the configuration
   * it was backing up had quietly reset.
   */
  it('sends the whole configuration every time, not just the theme', async () => {
    const mermaid = fakeMermaid();

    await renderDiagram('mermaid', 'graph TD;', { theme: 'dark', importer: importing(mermaid) });

    for (const [config] of mermaid.initialize.mock.calls) {
      expect(config.securityLevel, JSON.stringify(config)).toBe('strict');
      expect(config.htmlLabels, JSON.stringify(config)).toBe(false);
    }
    expect(mermaid.initialize).toHaveBeenLastCalledWith(configFor('dark'));
  });

  it('draws in the theme it was given', async () => {
    const mermaid = fakeMermaid();

    await renderDiagram('mermaid', 'graph TD;', { theme: 'dark', importer: importing(mermaid) });

    expect(mermaid.initialize.mock.calls.at(-1)[0].theme).toBe(THEMES.dark);
  });

  it('falls back to the light theme for one it does not know', async () => {
    const mermaid = fakeMermaid();

    await renderDiagram('mermaid', 'graph TD;', { theme: 'neon', importer: importing(mermaid) });

    expect(mermaid.initialize.mock.calls.at(-1)[0].theme).toBe(THEMES.light);
  });

  it('gives each diagram its own id, because mermaid measures by element', async () => {
    const mermaid = fakeMermaid();

    await renderDiagram('mermaid', 'a', { importer: importing(mermaid) });
    await renderDiagram('mermaid', 'b', { importer: importing(mermaid) });

    const [first, second] = mermaid.render.mock.calls.map((call) => call[0]);
    expect(first).not.toBe(second);
  });

  // A diagram is one part of a message somebody is reading: a malformed one
  // should cost its own block and nothing else.
  it('reports a diagram that will not parse, using mermaid own words', async () => {
    const mermaid = fakeMermaid({
      render: vi.fn(async () => {
        throw new Error('Parse error on line 2:\n  ...a whole diagram...');
      }),
    });

    const result = await renderDiagram('mermaid', 'graph TD;\n  ??', { importer: importing(mermaid) });

    expect(result.ok).toBe(false);
    // The first line names the fault; the rest is the diagram echoed back.
    expect(result.reason).toBe('Parse error on line 2:');
  });

  it('has something to say about a failure with no message', async () => {
    const empty = fakeMermaid({ render: vi.fn(async () => { throw new Error(''); }) });
    expect((await renderDiagram('mermaid', 'x', { importer: importing(empty) })).reason).toBe('This diagram could not be drawn.');

    resetMermaid();
    // Mermaid throws objects that are not Errors for some parse failures.
    const odd = fakeMermaid({ render: vi.fn(async () => { throw { detail: 'nope' } }) }); // eslint-disable-line no-throw-literal
    expect((await renderDiagram('mermaid', 'x', { importer: importing(odd) })).reason).toBe('This diagram could not be drawn.');
  });

  it('says so when there is nothing to draw', async () => {
    expect((await renderDiagram('mermaid', '')).reason).toBe('This diagram is empty.');
    expect((await renderDiagram('mermaid', '   \n  ')).reason).toBe('This diagram is empty.');
    expect((await renderDiagram(undefined)).reason).toBe('This diagram is empty.');
  });

  it('says so plainly when the renderer itself will not load', async () => {
    const result = await renderDiagram('mermaid', 'graph TD;', {
      importer: () => Promise.reject(new Error('chunk failed')),
    });

    expect(result).toEqual({ ok: false, reason: 'The diagram renderer could not be loaded.' });
  });

  // A chunk that did not load on a flaky connection should be retried, not
  // remembered as broken for the life of the tab.
  it('tries the import again after one that failed', async () => {
    const mermaid = fakeMermaid();
    let attempt = 0;
    const importer = () => {
      attempt += 1;
      return attempt === 1 ? Promise.reject(new Error('chunk failed')) : Promise.resolve({ default: mermaid });
    };

    expect((await renderDiagram('mermaid', 'a', { importer })).ok).toBe(false);
    expect((await renderDiagram('mermaid', 'a', { importer })).ok).toBe(true);
  });
});

describe('loadMermaid', () => {
  it('loads once, however many diagrams ask', async () => {
    const mermaid = fakeMermaid();
    const importer = vi.fn(importing(mermaid));

    await Promise.all([loadMermaid(importer), loadMermaid(importer), loadMermaid(importer)]);

    expect(importer).toHaveBeenCalledOnce();
  });

  it('takes a module that is not a default export', async () => {
    const mermaid = fakeMermaid();

    expect(await loadMermaid(() => Promise.resolve(mermaid))).toBe(mermaid);
  });

  /**
   * The real import, once.
   *
   * Every other test here injects a fake, which proves the wiring and nothing
   * about the dependency — and the dependency is the part that can silently
   * change shape on an upgrade. This loads the actual mermaid and configures
   * it, which is what says the import path and the configuration still fit.
   *
   * Drawing with it needs a real browser: mermaid measures text with
   * `getBBox`, which jsdom does not implement. That half is checked in
   * `desktop/scripts/verify-extensions.mjs`, in a real window.
   */
  it('loads the real mermaid and configures it', async () => {
    const mermaid = await loadMermaid();

    expect(typeof mermaid.render).toBe('function');
    expect(typeof mermaid.initialize).toBe('function');
  }, 30000);

  /**
   * The same for KaTeX, and this one carries a second claim: the default
   * importer pulls `katex/dist/katex.css` alongside the code.
   *
   * That stylesheet is not decoration. Without it KaTeX's output is unstyled
   * overlapping glyphs rather than an equation — it positions everything with
   * classes the stylesheet defines. Loading it on the same dynamic import is
   * what stops it being forgotten separately from the code it styles, and this
   * test is what says that import path still resolves.
   */
  it('loads the real KaTeX, and its stylesheet with it', async () => {
    const katexModule = await loadKatex();

    expect(typeof katexModule.renderToString).toBe('function');
    expect(katexModule.renderToString('x^2', { macros: {} })).toContain('katex');
  }, 30000);
});

/**
 * A format is a lookup, not an allowlist.
 *
 * The host still only *draws* what it has a drawer for — an extension cannot
 * ship drawing code, because that would put third-party markup on a privileged
 * origin, which is the one thing the view vocabulary exists to prevent. What
 * changed is the cost of naming a format nobody draws: it used to destroy the
 * whole view silently, and now it shows its source with a note.
 */
describe('the drawer registry', () => {
  it('finds a drawer for what it draws, whatever the case', () => {
    expect(drawerFor('mermaid')).toBeTypeOf('function');
    expect(drawerFor('math')).toBeTypeOf('function');
    expect(drawerFor('MERMAID')).toBe(drawerFor('mermaid'));
  });

  it('finds none for what it does not', () => {
    expect(drawerFor('vega-lite')).toBeNull();
    expect(drawerFor('')).toBeNull();
    expect(drawerFor(undefined)).toBeNull();
  });

  it('says what it can draw, which is the one place that decides', () => {
    expect(knownFormats()).toEqual(['mermaid', 'math']);
  });
});

describe('renderDiagram, for a format nothing draws', () => {
  // The whole point: a block that explains itself instead of a view that
  // vanishes. The source comes with it, which is what the reader wanted.
  it('names the format rather than failing silently', async () => {
    const result = await renderDiagram('vega-lite', '{ "mark": "bar" }');

    expect(result.ok).toBe(false);
    expect(result.reason).toContain('vega-lite');
    expect(result.reason).toContain('mermaid, math');
  });

  it('still refuses an empty source before anything else', async () => {
    expect((await renderDiagram('vega-lite', '   ')).reason).toBe('This diagram is empty.');
  });

  // Never throws: a diagram is one part of a message somebody is reading, and
  // an exception here would take the whole message with it.
  it('never throws, whatever it is handed', async () => {
    for (const [format, source] of [[undefined, undefined], [null, null], [{}, []], ['', ''], ['math', undefined]]) {
      await expect(renderDiagram(format, source)).resolves.toHaveProperty('ok');
    }
  });
});

describe('sanitiseMath', () => {
  // KaTeX draws radicals and stretchy delimiters as inline <svg><path>. The
  // obvious profile — html and mathMl — strips both, and the expression then
  // renders silently missing a symbol, which is the worst way to be wrong.
  it('keeps the parts an equation is actually made of', () => {
    const drawn = sanitiseMath(katex.renderToString('\\frac{a}{b} = \\sqrt{x^2}', { displayMode: true, macros: {} }));

    expect(drawn).toContain('<svg');
    expect(drawn).toContain('<path');
    expect(drawn).toMatch(/<math/i);
    // KaTeX positions every glyph with an inline style; without them an
    // equation is a heap of characters.
    expect(drawn).toContain('style="');
  });

  it('removes everything that could act, even with SVG allowed', () => {
    const hostile =
      '<span onclick="x()">a</span><script>alert(1)</script><img src=x onerror=y>' +
      '<a href="javascript:1">l</a><foreignObject><b>html</b></foreignObject>';

    const clean = sanitiseMath(hostile);

    expect(clean).not.toMatch(/<script/i);
    expect(clean).not.toMatch(/<img/i);
    expect(clean).not.toMatch(/<a[\s>]/i);
    expect(clean).not.toMatch(/onclick|onerror/i);
    expect(clean).not.toMatch(/foreignobject/i);
  });

  it('has nothing to say about nothing', () => {
    expect(sanitiseMath('')).toBe('');
    expect(sanitiseMath(undefined)).toBe('');
  });
});

describe('renderDiagram, drawing maths', () => {
  const importing_ = () => () => Promise.resolve({ default: katex });

  it('draws an expression', async () => {
    const result = await renderDiagram('math', 'E = mc^2', { importer: importing_() });

    expect(result.ok).toBe(true);
    expect(result.html).toMatch(/<math|katex/i);
  });

  /**
   * The trap, and the reason `macros` is built fresh inside the drawer.
   *
   * KaTeX writes `\gdef` definitions into whatever object it is handed, so a
   * module-level `macros` — the natural thing to write — lets one expression
   * redefine `\alpha` for every equation drawn afterwards, in every message on
   * the page. This is that, driven through the real renderer.
   */
  it('does not let one expression redefine a symbol for the next', async () => {
    await renderDiagram('math', '\\gdef\\alpha{\\text{PWNED}} \\alpha', { importer: importing_() });

    const after = await renderDiagram('math', '\\alpha', { importer: importing_() });

    expect(after.ok).toBe(true);
    expect(after.html).not.toContain('PWNED');
  });

  // `trust: false` refuses \href and \includegraphics outright, so neither is
  // ever produced. The URL text survives inside the echoed source, inert.
  it('produces no link and no image, whatever the expression asks for', async () => {
    const result = await renderDiagram('math', '\\href{https://evil.example}{click}', { importer: importing_() });

    if (result.ok) {
      expect(result.html).not.toMatch(/<a[\s>]/i);
      expect(result.html).not.toMatch(/<img/i);
    } else {
      expect(result.reason).toBeTruthy();
    }
  });

  it('reports an expression that will not parse, in KaTeX own words', async () => {
    const result = await renderDiagram('math', '\\frac{', { importer: importing_() });

    expect(result.ok).toBe(false);
    expect(result.reason).toBeTruthy();
    expect(result.reason).not.toContain('\n');
  });

  it('says so plainly when the renderer itself will not load', async () => {
    const failing = () => Promise.reject(new Error('chunk failed'));

    expect((await renderDiagram('math', 'x', { importer: failing })).reason).toBe(
      'The maths renderer could not be loaded.',
    );
  });

  // Some builds hand back the namespace itself rather than a default export,
  // and an equation should not depend on which.
  it('takes the module however the bundler hands it over', async () => {
    const namespace = { renderToString: katex.renderToString };

    const result = await renderDiagram('math', 'x^2', { importer: () => Promise.resolve(namespace) });

    expect(result.ok).toBe(true);
  });

  it('has something to say about a failure with no message', async () => {
    const mute = { renderToString: () => { throw new Error(''); } };

    expect((await renderDiagram('math', 'x', { importer: () => Promise.resolve({ default: mute }) })).reason).toBe(
      'This expression could not be drawn.',
    );
  });

  // Loaded once and remembered, the way mermaid is — and a failed load is
  // forgotten so a flaky connection is retried rather than remembered forever.
  it('loads KaTeX once, and retries after a failure', async () => {
    const importer = vi.fn(() => Promise.resolve({ default: katex }));
    await renderDiagram('math', 'x', { importer });
    await renderDiagram('math', 'y', { importer });
    expect(importer).toHaveBeenCalledOnce();

    resetMermaid();
    const failing = vi.fn(() => Promise.reject(new Error('offline')));
    await renderDiagram('math', 'x', { importer: failing });
    await renderDiagram('math', 'x', { importer: failing });
    expect(failing).toHaveBeenCalledTimes(2);
  });
});
