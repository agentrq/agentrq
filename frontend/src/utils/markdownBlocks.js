/**
 * Splitting a message body so an extension can draw part of it.
 *
 * Until now a message went straight from `renderMarkdown` into `v-html`, and
 * nothing was consulted on the way — so a ```mermaid fence was a code block and
 * no extension could make it anything else. This is the seam that changes: a
 * body becomes a list of runs, and a run whose fence language somebody claimed
 * is handed to them instead of being rendered as code.
 *
 * ## Splitting rather than post-processing the HTML
 *
 * The alternative is to render the markdown and then walk the output for
 * `<code class="language-mermaid">`. That means reading back what DOMPurify
 * produced and trusting the round trip, and it means the fence's exact source —
 * which is what a diagram renderer needs — has to be recovered from escaped
 * HTML. Splitting the text first keeps the source *the source*.
 *
 * ## What counts as a fence
 *
 * Three or more backticks, a language word, and a matching or longer run of
 * backticks to close it. The longer-run rule is CommonMark's and it is what
 * lets a fence contain backticks — a diagram with a label in code ticks is not
 * exotic.
 *
 * An unclosed fence is deliberately **not** claimed. A message still being
 * streamed in ends mid-fence constantly, and drawing a half-written diagram
 * that flickers into shape is worse than showing the text until it is finished.
 */

/** A fence opener: indent, the ticks, then the language word. */
const FENCE = /^([ \t]{0,3})(`{3,})[ \t]*([A-Za-z0-9_+-]*)[ \t]*$/;

/**
 * Splits a body into markdown runs and claimed fenced blocks.
 *
 * @param {string} text
 * @param {string[]} languages  Lowercased languages somebody has claimed.
 * @returns {Array<{type: 'markdown', text: string} | {type: 'block', language: string, source: string, fence: string}>}
 */
export function splitFences(text, languages = []) {
  const body = String(text ?? '');
  const claimed = new Set(languages.map((language) => String(language).toLowerCase()));
  if (claimed.size === 0 || !body) return body ? [{ type: 'markdown', text: body }] : [];

  const lines = body.split('\n');
  const segments = [];
  let run = [];

  const flush = () => {
    if (run.length > 0) segments.push({ type: 'markdown', text: run.join('\n') });
    run = [];
  };

  for (let i = 0; i < lines.length; i += 1) {
    const opener = FENCE.exec(lines[i]);
    if (!opener || !claimed.has(opener[3].toLowerCase())) {
      run.push(lines[i]);
      continue;
    }

    const [, indent, ticks, language] = opener;
    const closer = findCloser(lines, i + 1, ticks);
    if (closer === -1) {
      // Unclosed: still being typed, or never finished. Left as text.
      run.push(lines[i]);
      continue;
    }

    flush();
    segments.push({
      type: 'block',
      language: language.toLowerCase(),
      // The source exactly as written, minus the fence's own indentation —
      // which is the opener's, not each line's, so a diagram keeps its shape.
      source: lines.slice(i + 1, closer).map((line) => stripIndent(line, indent.length)).join('\n'),
      fence: lines.slice(i, closer + 1).join('\n'),
    });
    i = closer;
  }

  flush();
  return segments;
}

/** The first line closing this fence: the same ticks or more, and nothing else. */
function findCloser(lines, from, ticks) {
  for (let i = from; i < lines.length; i += 1) {
    const match = /^[ \t]{0,3}(`{3,})[ \t]*$/.exec(lines[i]);
    if (match && match[1].length >= ticks.length) return i;
  }
  return -1;
}

/** Removes up to `width` leading spaces, the way a fenced block is dedented. */
function stripIndent(line, width) {
  let cut = 0;
  while (cut < width && (line[cut] === ' ' || line[cut] === '\t')) cut += 1;
  return line.slice(cut);
}

/**
 * Whether a body holds anything worth asking an extension about.
 *
 * Every message is checked, so this has to be cheap and is: a body with no
 * backticks at all — most of them — never reaches the splitter.
 */
export function mayHaveBlocks(text, languages = []) {
  return languages.length > 0 && String(text ?? '').includes('```');
}
