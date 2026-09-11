import { describe, it, expect, vi, afterEach } from 'vitest';

import { THEMES, loadMermaid, renderDiagram, resetMermaid, sanitiseSvg } from '../src/composables/useDiagram';

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

describe('renderDiagram', () => {
  it('draws one, and sanitises what came back', async () => {
    const mermaid = fakeMermaid();

    const result = await renderDiagram('graph TD;\n  A-->B;', { importer: importing(mermaid) });

    expect(result.ok).toBe(true);
    expect(result.svg).toContain('<svg');
    expect(mermaid.render).toHaveBeenCalledWith(expect.stringContaining('agentrq-diagram-'), 'graph TD;\n  A-->B;');
  });

  /**
   * Mermaid reads `%%{init: ...}%%` out of the source, and one of the things it
   * can set is `securityLevel` — so a diagram could otherwise turn off
   * mermaid's own escaping from inside the text being drawn. Set here, once,
   * and refused by the extension too: the two guards fail differently.
   */
  it('configures mermaid strictly, whatever the source asks for', async () => {
    const mermaid = fakeMermaid();

    await renderDiagram('graph TD;', { importer: importing(mermaid) });

    const config = mermaid.initialize.mock.calls[0][0];
    expect(config.securityLevel).toBe('strict');
    expect(config.htmlLabels).toBe(false);
    expect(config.startOnLoad).toBe(false);
  });

  it('draws in the theme it was given', async () => {
    const mermaid = fakeMermaid();

    await renderDiagram('graph TD;', { theme: 'dark', importer: importing(mermaid) });

    expect(mermaid.initialize).toHaveBeenLastCalledWith({ theme: THEMES.dark });
  });

  it('falls back to the light theme for one it does not know', async () => {
    const mermaid = fakeMermaid();

    await renderDiagram('graph TD;', { theme: 'neon', importer: importing(mermaid) });

    expect(mermaid.initialize).toHaveBeenLastCalledWith({ theme: THEMES.light });
  });

  it('gives each diagram its own id, because mermaid measures by element', async () => {
    const mermaid = fakeMermaid();

    await renderDiagram('a', { importer: importing(mermaid) });
    await renderDiagram('b', { importer: importing(mermaid) });

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

    const result = await renderDiagram('graph TD;\n  ??', { importer: importing(mermaid) });

    expect(result.ok).toBe(false);
    // The first line names the fault; the rest is the diagram echoed back.
    expect(result.reason).toBe('Parse error on line 2:');
  });

  it('has something to say about a failure with no message', async () => {
    const empty = fakeMermaid({ render: vi.fn(async () => { throw new Error(''); }) });
    expect((await renderDiagram('x', { importer: importing(empty) })).reason).toBe('This diagram could not be drawn.');

    resetMermaid();
    // Mermaid throws objects that are not Errors for some parse failures.
    const odd = fakeMermaid({ render: vi.fn(async () => { throw { detail: 'nope' } }) }); // eslint-disable-line no-throw-literal
    expect((await renderDiagram('x', { importer: importing(odd) })).reason).toBe('This diagram could not be drawn.');
  });

  it('says so when there is nothing to draw', async () => {
    expect((await renderDiagram('')).reason).toBe('This diagram is empty.');
    expect((await renderDiagram('   \n  ')).reason).toBe('This diagram is empty.');
    expect((await renderDiagram(undefined)).reason).toBe('This diagram is empty.');
  });

  it('says so plainly when the renderer itself will not load', async () => {
    const result = await renderDiagram('graph TD;', {
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

    expect((await renderDiagram('a', { importer })).ok).toBe(false);
    expect((await renderDiagram('a', { importer })).ok).toBe(true);
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
});
