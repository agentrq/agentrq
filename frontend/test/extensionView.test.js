import { describe, it, expect } from 'vitest';

import {
  MAX_DEPTH,
  MAX_NODES,
  MAX_TEXT,
  NODE_TYPES,
  TONES,
  normaliseView,
  safeHref,
} from '../src/composables/useExtensionView';

/**
 * Extensions describe; AgentRQ renders. The vocabulary is closed, so a page
 * looks like the rest of the product rather than approximating it, and the
 * renderer's privileged origin never carries anybody else's markup.
 */

const view = (nodes, title = 'Issues') => ({ title, nodes });

describe('safeHref', () => {
  it('keeps an ordinary web link', () => {
    expect(safeHref('https://linear.app/x')).toBe('https://linear.app/x');
    expect(safeHref('http://internal/x')).toBe('http://internal/x');
  });

  it('drops anything that is not plainly the web', () => {
    // The same instinct markdown.js applies to agent-written bodies: a file:
    // URL is how a link reaches the machine, and javascript: needs no
    // explanation.
    for (const href of ['file:///etc/passwd', 'javascript:alert(1)', 'data:text/html,x', 'mailto:a@b']) {
      expect(safeHref(href), href).toBe('');
    }
    expect(safeHref(undefined)).toBe('');
  });
});

describe('normaliseView', () => {
  it('accepts every node type it offers', () => {
    const nodes = Object.keys(NODE_TYPES).map((type) => {
      if (type === 'group') return { type, label: 'g', children: [{ type: 'text', value: 'x' }] };
      if (type === 'rows') return { type, items: [{ type: 'row', label: 'a', value: 'b' }] };
      return { type, value: 'x', label: 'x' };
    });

    expect(normaliseView(view(nodes)).ok).toBe(true);
  });

  it('takes a bare list as well as a titled view', () => {
    expect(normaliseView([{ type: 'text', value: 'x' }]).view.nodes).toHaveLength(1);
    expect(normaliseView(view([{ type: 'text', value: 'x' }])).view.title).toBe('Issues');
  });

  it('names an unknown node type and lists what it could have been', () => {
    // An author who mistyped "headding" is one line from working; a bare
    // "unknown type" leaves them guessing.
    const { ok, reason } = normaliseView(view([{ type: 'headding', value: 'x' }]));

    expect(ok).toBe(false);
    expect(reason).toContain('"headding" is not something a view can contain');
    expect(reason).toContain('heading');
  });

  it('rejects rather than skips, so nothing renders half a page', () => {
    const { ok } = normaliseView(view([{ type: 'text', value: 'fine' }, { type: 'iframe' }]));

    expect(ok).toBe(false);
  });

  it('refuses an element that is not an object', () => {
    expect(normaliseView(view(['just text'])).reason).toContain('must be an object');
    expect(normaliseView(view([null])).ok).toBe(false);
    expect(normaliseView(view([['nested']])).ok).toBe(false);
  });

  it('refuses a node with no type at all', () => {
    expect(normaliseView(view([{ value: 'x' }])).reason).toContain('"(none)"');
  });

  it('refuses anything that is not a view', () => {
    expect(normaliseView(undefined).reason).toBe('This view returned nothing to draw.');
    expect(normaliseView('a string').ok).toBe(false);
    expect(normaliseView({ title: 'x' }).reason).toBe('A view must be a list of elements.');
  });

  it('accepts an empty view, which is a legitimate answer', () => {
    // "Nothing to show" is a state, not a failure.
    const { ok, view: out } = normaliseView(view([]));

    expect(ok).toBe(true);
    expect(out.nodes).toEqual([]);
  });

  it('nests groups, and refuses nesting past the limit', () => {
    const nest = (depth) =>
      depth === 0 ? { type: 'text', value: 'deep' } : { type: 'group', children: [nest(depth - 1)] };

    expect(normaliseView(view([nest(MAX_DEPTH - 1)])).ok).toBe(true);
    expect(normaliseView(view([nest(MAX_DEPTH + 1)])).reason).toContain('nests deeper');
  });

  it('refuses a group or rows with no list in it', () => {
    expect(normaliseView(view([{ type: 'group', label: 'g' }])).reason).toContain('needs a list');
    expect(normaliseView(view([{ type: 'rows' }])).reason).toContain('needs a list');
  });

  it('caps how much one extension can put on a page', () => {
    const many = Array.from({ length: MAX_NODES + 1 }, () => ({ type: 'text', value: 'x' }));

    expect(normaliseView(view(many)).reason).toContain(`more than ${MAX_NODES} elements`);
  });

  it('counts nested nodes against the same budget', () => {
    const children = Array.from({ length: MAX_NODES }, () => ({ type: 'text', value: 'x' }));

    expect(normaliseView(view([{ type: 'group', children }])).ok).toBe(false);
  });

  it('truncates a runaway string rather than rendering it', () => {
    const { view: out } = normaliseView(view([{ type: 'text', value: 'x'.repeat(MAX_TEXT + 100) }]));

    expect(out.nodes[0].value).toHaveLength(MAX_TEXT + 1);
    expect(out.nodes[0].value.endsWith('…')).toBe(true);
  });

  it('coerces a value that is not a string at all', () => {
    expect(normaliseView(view([{ type: 'text', value: 42 }])).view.nodes[0].value).toBe('42');
    expect(normaliseView(view([{ type: 'text', value: null }])).view.nodes[0].value).toBe('');
  });

  it('falls back on an unknown tone rather than letting a colour through', () => {
    expect(normaliseView(view([{ type: 'badge', value: 'x', tone: 'neon' }])).view.nodes[0].tone).toBe(
      'default',
    );
    for (const tone of TONES) {
      expect(normaliseView(view([{ type: 'badge', value: 'x', tone }])).view.nodes[0].tone).toBe(tone);
    }
  });

  it('strips an href it will not follow, keeping the text', () => {
    const { view: out } = normaliseView(view([{ type: 'link', label: 'Home', href: 'file:///' }]));

    expect(out.nodes[0].label).toBe('Home');
    expect(out.nodes[0].href).toBe('');
  });

  it('passes an action back verbatim without interpreting it', () => {
    // It names something the extension registered; this is not the place to
    // decide what it means.
    const { view: out } = normaliseView(view([{ type: 'button', label: 'Go', action: 'create-issue' }]));

    expect(out.nodes[0].action).toBe('create-issue');
    expect(normaliseView(view([{ type: 'button', action: null }])).view.nodes[0].action).toBe('');
  });

  it('drops a title that is not a string', () => {
    expect(normaliseView({ title: 7, nodes: [] }).view.title).toBe('7');
    expect(normaliseView({ nodes: [] }).view.title).toBe('');
  });
});
