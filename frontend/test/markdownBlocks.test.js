import { describe, it, expect } from 'vitest';

import { mayHaveBlocks, splitFences } from '../src/utils/markdownBlocks';

/**
 * The seam that lets an extension draw part of a message.
 *
 * Splitting the text rather than walking the rendered HTML is what keeps the
 * fence's source *the source*: a diagram renderer needs exactly what was
 * written, and recovering that from escaped markup is a round trip through
 * something that was never meant to be read back.
 */

const claimed = ['mermaid'];

describe('splitFences', () => {
  it('leaves a body with no claimed fence as one run', () => {
    expect(splitFences('just words', claimed)).toEqual([{ type: 'markdown', text: 'just words' }]);
  });

  it('pulls a claimed fence out, keeping what is either side', () => {
    const body = ['before', '```mermaid', 'graph TD;', '  A-->B;', '```', 'after'].join('\n');

    const segments = splitFences(body, claimed);

    expect(segments).toHaveLength(3);
    expect(segments[0]).toEqual({ type: 'markdown', text: 'before' });
    expect(segments[1]).toMatchObject({ type: 'block', language: 'mermaid', source: 'graph TD;\n  A-->B;' });
    expect(segments[2]).toEqual({ type: 'markdown', text: 'after' });
  });

  // The toggle shows this, so it has to be the fence as typed rather than a
  // reconstruction of it.
  it('keeps the original fence, for showing the text again', () => {
    const body = '```mermaid\ngraph TD;\n```';

    expect(splitFences(body, claimed)[0].fence).toBe(body);
  });

  it('leaves a fence nobody claimed alone', () => {
    const body = '```js\nconst a = 1;\n```';

    expect(splitFences(body, claimed)).toEqual([{ type: 'markdown', text: body }]);
  });

  it('claims one whatever case it was written in', () => {
    expect(splitFences('```Mermaid\ngraph TD;\n```', claimed)[0].type).toBe('block');
    expect(splitFences('```mermaid\ngraph TD;\n```', ['MERMAID'])[0].type).toBe('block');
  });

  it('takes several out of one body', () => {
    const body = ['```mermaid', 'a', '```', 'between', '```mermaid', 'b', '```'].join('\n');

    const segments = splitFences(body, claimed);

    expect(segments.map((s) => s.type)).toEqual(['block', 'markdown', 'block']);
    expect(segments[2].source).toBe('b');
  });

  // CommonMark: a longer run closes a shorter one, which is what lets a fence
  // hold backticks. A diagram with a label in code ticks is not exotic.
  it('honours a longer closing run, and ignores a shorter one', () => {
    const body = ['````mermaid', 'graph TD;', '```', 'still inside', '````'].join('\n');

    const [block] = splitFences(body, claimed);

    expect(block.source).toBe('graph TD;\n```\nstill inside');
  });

  /**
   * A message still streaming in ends mid-fence constantly. Drawing a
   * half-written diagram that flickers into shape is worse than showing the
   * text until it is finished.
   */
  it('leaves an unclosed fence as text', () => {
    const body = '```mermaid\ngraph TD;\n  A-->B;';

    expect(splitFences(body, claimed)).toEqual([{ type: 'markdown', text: body }]);
  });

  it('dedents a tab-indented fence as well as a spaced one', () => {
    const body = ['\t```mermaid', '\tgraph TD;', '\t```'].join('\n');

    expect(splitFences(body, claimed)[0].source).toBe('graph TD;');
  });

  it('dedents by the fence indentation, not by each line', () => {
    // The diagram keeps its own shape; only the fence's offset is removed.
    const body = ['  ```mermaid', '  graph TD;', '      A-->B;', '  ```'].join('\n');

    expect(splitFences(body, claimed)[0].source).toBe('graph TD;\n    A-->B;');
  });

  it('takes a fence that is the whole body', () => {
    const segments = splitFences('```mermaid\ngraph TD;\n```', claimed);

    expect(segments).toHaveLength(1);
    expect(segments[0].type).toBe('block');
  });

  it('asks nothing of a body when nobody claimed anything', () => {
    const body = '```mermaid\ngraph TD;\n```';

    expect(splitFences(body, [])).toEqual([{ type: 'markdown', text: body }]);
  });

  it('has nothing to say about nothing', () => {
    expect(splitFences('', claimed)).toEqual([]);
    expect(splitFences(undefined, claimed)).toEqual([]);
    expect(splitFences(null, [])).toEqual([]);
  });

  it('does not treat a fence with no language as claimed', () => {
    const body = '```\ngraph TD;\n```';

    expect(splitFences(body, claimed)).toEqual([{ type: 'markdown', text: body }]);
  });

  it('keeps an empty diagram out of the markdown run it came from', () => {
    // Whether an empty source is worth drawing is the renderer's call, not the
    // splitter's — it reports what was written.
    expect(splitFences('```mermaid\n```', claimed)[0]).toMatchObject({ type: 'block', source: '' });
  });
});

describe('mayHaveBlocks', () => {
  // Every message is checked, so the common answer has to be cheap.
  it('is false for a body that could not contain one', () => {
    expect(mayHaveBlocks('just words', claimed)).toBe(false);
    expect(mayHaveBlocks('', claimed)).toBe(false);
    expect(mayHaveBlocks(undefined, claimed)).toBe(false);
  });

  it('is false when nobody has claimed a language', () => {
    expect(mayHaveBlocks('```mermaid\na\n```', [])).toBe(false);
  });

  it('is true for a body that might', () => {
    expect(mayHaveBlocks('```mermaid\na\n```', claimed)).toBe(true);
    // Deliberately a cheap guess rather than a parse: it says "worth looking".
    expect(mayHaveBlocks('```js\na\n```', claimed)).toBe(true);
  });
});
