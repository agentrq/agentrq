/**
 * What an extension is allowed to draw, and what happens to what it sends.
 *
 * Extensions describe; AgentRQ renders. A page or panel is a JSON spec in a
 * closed vocabulary, drawn with this application's own components — there is no
 * third-party markup in the renderer and no iframe, which is what keeps
 * extension UI looking like the rest of the product instead of approximating it,
 * and keeps the renderer's privileged origin free of anybody else's code.
 *
 * ## The vocabulary is closed, and unknown nodes are refused
 *
 * A node type nobody recognises is **rejected, not skipped**. Skipping it would
 * render a page that is quietly missing whatever the author thought they had
 * written — worse for them than being told, and worse for the user than seeing
 * nothing at all.
 *
 * ## Everything an extension sends is untrusted text
 *
 * Not because extensions are assumed hostile — they run as trusted code and
 * could do far worse than inject markup — but because text arriving from an
 * extension often did not originate there. It came from an issue title, a commit
 * message, a webhook payload. Treating it as untrusted at the boundary is what
 * stops that being anybody's problem to remember.
 */

/** Every node an extension may use. Anything else is a mistake worth reporting. */
export const NODE_TYPES = Object.freeze({
  text: ['value', 'tone'],
  heading: ['value'],
  rows: ['items'],
  row: ['label', 'value', 'href'],
  badge: ['value', 'tone'],
  button: ['label', 'action', 'tone'],
  link: ['label', 'href'],
  empty: ['value'],
  group: ['label', 'children'],
  // A diagram is a *language*, not markup: the extension sends the source and
  // AgentRQ draws it, exactly as it already draws untrusted markdown. Letting
  // an extension hand back SVG instead would put third-party markup on a
  // privileged origin, which is the one thing this vocabulary exists to avoid.
  diagram: ['format', 'source', 'label'],
});

/** Diagram languages AgentRQ knows how to draw. */
export const DIAGRAM_FORMATS = Object.freeze(['mermaid']);

/**
 * How much diagram source is worth drawing.
 *
 * Far larger than `MAX_TEXT`, because a diagram is a program and a real one is
 * long — and still bounded, because the renderer that parses it runs in the
 * window somebody is reading in.
 */
export const MAX_SOURCE = 20000;

/** Tones map to our own palette; an unknown one falls back rather than leaking a colour. */
export const TONES = Object.freeze(['default', 'muted', 'positive', 'warning', 'critical']);

/** Bounded so one extension cannot render a page nobody can scroll past. */
export const MAX_NODES = 200;
export const MAX_DEPTH = 5;
export const MAX_TEXT = 2000;

const fail = (reason) => ({ ok: false, reason });

/**
 * Whether a link is one we will make clickable.
 *
 * The same instinct `markdown.js` applies to agent-written message bodies, for
 * the same reason: a `file:` URL is how a link reaches the machine, and
 * `javascript:` needs no explanation. Anything not plainly http(s) keeps its
 * text and loses its href.
 */
export function safeHref(href) {
  const value = String(href ?? '').trim();
  if (!value) return '';
  return /^https?:\/\//i.test(value) ? value : '';
}

/** Clamps text, so a runaway value truncates rather than filling the screen. */
function text(value) {
  const string = typeof value === 'string' ? value : String(value ?? '');
  return string.length > MAX_TEXT ? `${string.slice(0, MAX_TEXT)}…` : string;
}

function tone(value) {
  return TONES.includes(value) ? value : 'default';
}

/**
 * Validates and normalises one node, recursively.
 *
 * Returns the node the renderer will draw — with every string clamped, every
 * tone known, and every href either safe or absent — or a reason it will draw
 * nothing at all.
 */
function normaliseNode(node, depth, budget) {
  if (depth > MAX_DEPTH) return fail(`This view nests deeper than ${MAX_DEPTH} levels.`);
  if (budget.count >= MAX_NODES) return fail(`This view has more than ${MAX_NODES} elements.`);
  if (!node || typeof node !== 'object' || Array.isArray(node)) {
    return fail('Every element of a view must be an object.');
  }

  const type = String(node.type ?? '');
  if (!(type in NODE_TYPES)) {
    // Named, and the alternatives listed: an author who mistyped "headding" is
    // one line from working, and a bare "unknown type" leaves them guessing.
    return fail(`"${type || '(none)'}" is not something a view can contain. Use one of: ${Object.keys(NODE_TYPES).join(', ')}.`);
  }

  budget.count += 1;
  const out = { type };

  if (type === 'group' || type === 'rows') {
    const children = node[type === 'group' ? 'children' : 'items'];
    if (!Array.isArray(children)) return fail(`A "${type}" needs a list of elements.`);

    const normalised = [];
    for (const child of children) {
      const result = normaliseNode(child, depth + 1, budget);
      if (!result.ok) return result;
      normalised.push(result.node);
    }
    if (type === 'group') out.label = text(node.label);
    out[type === 'group' ? 'children' : 'items'] = normalised;
    return { ok: true, node: out };
  }

  if (type === 'diagram') {
    const format = String(node.format ?? '');
    if (!DIAGRAM_FORMATS.includes(format)) {
      // Named, and the alternatives listed, the same way an unknown node type
      // is: an author who wrote "mermaidjs" is one word from working.
      return fail(`"${format || '(none)'}" is not a diagram this can draw. Use one of: ${DIAGRAM_FORMATS.join(', ')}.`);
    }
    const source = typeof node.source === 'string' ? node.source : '';
    if (!source.trim()) return fail('A diagram needs source to draw.');
    if (source.length > MAX_SOURCE) return fail(`This diagram is longer than ${MAX_SOURCE} characters.`);

    // Not clamped like text: half a diagram is not a shorter diagram, it is a
    // syntax error. Too long is refused above instead.
    out.format = format;
    out.source = source;
    out.label = text(node.label);
    return { ok: true, node: out };
  }

  if ('value' in node) out.value = text(node.value);
  if ('label' in node) out.label = text(node.label);
  if ('tone' in node) out.tone = tone(node.tone);
  if ('href' in node) out.href = safeHref(node.href);
  // An action names something the extension registered; it is passed back
  // verbatim on click and is never interpreted here.
  if ('action' in node) out.action = String(node.action ?? '');

  return { ok: true, node: out };
}

/**
 * Turns what an extension returned into what the renderer will draw.
 *
 * One reason rather than a list, deliberately: a spec is generated by code, so
 * the first thing wrong with it is the thing to fix, and the rest are usually
 * the same mistake repeated.
 */
export function normaliseView(spec) {
  if (!spec || typeof spec !== 'object') return fail('This view returned nothing to draw.');

  const nodes = Array.isArray(spec) ? spec : spec.nodes;
  if (!Array.isArray(nodes)) return fail('A view must be a list of elements.');

  // An empty view is a legitimate answer — "nothing to show" is a state, not a
  // failure — and it keeps its title, because a heading over an empty panel is
  // how a person can tell which panel is empty.
  const budget = { count: 0 };
  const out = [];
  for (const node of nodes) {
    const result = normaliseNode(node, 1, budget);
    if (!result.ok) return result;
    out.push(result.node);
  }

  return { ok: true, view: { title: text(spec.title ?? ''), nodes: out } };
}
