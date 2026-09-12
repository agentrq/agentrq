// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

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
  // The four that take something back. Everything above describes what to show;
  // these describe what to ask for, and they are what lets an extension own a
  // settings screen without owning any of its styling.
  field: ['key', 'label', 'input', 'value', 'placeholder'],
  select: ['key', 'label', 'value', 'options'],
  toggle: ['key', 'label', 'value'],
  form: ['action', 'submit', 'children'],
});

/**
 * What a `field` may ask for.
 *
 * Deliberately short. Each one is a real control this application already
 * styles, and a type nobody recognises falls back to `text` rather than failing
 * the view — the difference between a password box and a text box is worth
 * getting right, and not worth a blank panel when an author typos it.
 *
 * `secret` is the one with a rule attached: it renders masked, and an
 * extension is expected to put what comes back into `ctx.storage.secret` rather
 * than its ordinary storage. Nothing here can enforce that — it is the author's
 * to get right — but naming the type is what makes the omission visible.
 */
export const INPUT_TYPES = Object.freeze(['text', 'multiline', 'number', 'secret']);

/** A settings key: an identifier an extension will read back by name. */
export const KEY_RE = /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$/;

/** As many choices as a select may offer before it should have been a search. */
export const MAX_OPTIONS = 50;

/**
 * What a diagram format has to *look* like, which is not the same as one this
 * app can draw.
 *
 * `format` is two things at once: a lookup key into the drawer registry, and a
 * label shown to the reader when there is no title. So its shape is checked —
 * a word, optionally hyphenated, bounded — and nothing more.
 *
 * Membership is deliberately not checked here. It used to be, against a list of
 * one, and the cost was out of all proportion: an unknown format failed
 * `normaliseView`, which fails the *whole view*, which `MarkdownBody` then drops
 * entirely — so the fence fell back to an ordinary code block and nothing, to
 * the reader or the author, said why. A format nobody draws is now a valid node
 * that degrades to showing its source, which is what the reader wanted from it
 * anyway. See `createDrawerSource` in `useDrawerFrame.js`.
 */
export const DIAGRAM_FORMAT = /^[a-z][a-z0-9-]{0,31}$/i;

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

  // A form holds children and carries the action its submit button fires. Read
  // before `group`/`rows` would have been, because it nests the same way and
  // needs the same recursion with one extra field.
  if (type === 'form') {
    if (!Array.isArray(node.children)) return fail('A "form" needs a list of elements.');

    const normalised = [];
    for (const child of node.children) {
      const result = normaliseNode(child, depth + 1, budget);
      if (!result.ok) return result;
      normalised.push(result.node);
    }
    const action = String(node.action ?? '');
    // Refused rather than defaulted: a form with nothing to submit to is a
    // screen where typing does nothing, which is the most confusing thing this
    // vocabulary could produce.
    if (!action) return fail('A "form" needs an "action" to submit to.');

    out.action = action;
    out.submit = text(node.submit) || 'Save';
    out.children = normalised;
    return { ok: true, node: out };
  }

  if (type === 'field' || type === 'select' || type === 'toggle') {
    const key = String(node.key ?? '');
    if (!KEY_RE.test(key)) {
      // Named, because this is the field an extension reads back by name and a
      // view that silently renamed it would hand back values nobody asked for.
      return fail(`"${key || '(none)'}" is not a settings key. Use letters, digits, dot, dash or underscore.`);
    }
    out.key = key;
    out.label = text(node.label);

    if (type === 'toggle') {
      out.value = Boolean(node.value);
      return { ok: true, node: out };
    }

    if (type === 'select') {
      if (!Array.isArray(node.options) || node.options.length === 0) {
        return fail(`The select "${key}" needs a list of options.`);
      }
      if (node.options.length > MAX_OPTIONS) {
        return fail(`The select "${key}" offers more than ${MAX_OPTIONS} options.`);
      }
      // An option is a value and what to call it. A bare string is accepted as
      // both, because half the selects anybody writes have nothing else to say.
      out.options = node.options.map((option) =>
        typeof option === 'object' && option !== null
          ? { value: text(option.value), label: text(option.label) || text(option.value) }
          : { value: text(option), label: text(option) },
      );
      out.value = text(node.value);
      return { ok: true, node: out };
    }

    // A field. An unrecognised input falls back rather than failing the view:
    // a text box where somebody meant a number box is a small wrong thing, and
    // a blank panel is a large one.
    out.input = INPUT_TYPES.includes(node.input) ? node.input : 'text';
    out.placeholder = text(node.placeholder);
    // Never echoed back. A secret this application has stored is not shown in
    // `config`, and a view drawing one into an input would put it on screen, in
    // a screenshot, and in whatever the renderer's memory ends up in.
    out.value = out.input === 'secret' ? '' : text(node.value);
    return { ok: true, node: out };
  }

  if (type === 'diagram') {
    const format = String(node.format ?? '');
    if (!DIAGRAM_FORMAT.test(format)) {
      // Still refused, because a malformed format is a malformed node: it is a
      // lookup key and a visible label, and neither tolerates arbitrary text.
      return fail(`"${format || '(none)'}" is not a diagram format. A format is a word, like "mermaid".`);
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

  return { ok: true, view: { title: text(spec.title ?? ''), nodes: out, values: initialValues(out) } };
}

/**
 * What every input in a view starts out holding.
 *
 * Collected here, once, rather than read off each input as it renders — because
 * what the user is typing has to survive the view being redrawn. An extension
 * answers a submit with a fresh view, and if the draft lived in the inputs
 * themselves every redraw would wipe whatever had not been submitted yet.
 *
 * Keyed by the extension's own key, so two inputs sharing one are one value.
 * That is the author's decision to make, not something to rename around: a
 * `field` and a `toggle` called the same thing is a mistake worth them seeing,
 * and inventing `key-2` would hide it.
 */
export function initialValues(nodes, into = {}) {
  for (const node of nodes ?? []) {
    if (node.type === 'form' || node.type === 'group') initialValues(node.children, into);
    else if (node.type === 'rows') initialValues(node.items, into);
    else if (node.key) into[node.key] = node.value;
  }
  return into;
}
