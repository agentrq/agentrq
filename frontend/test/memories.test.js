import { describe, it, expect } from 'vitest';

import { renderMarkdown } from '../src/utils/markdown';
import {
  INDEX_MEMORY,
  MEMORY_LINK_ATTR,
  memoryLinkFromEvent,
  memoryLinkTarget,
  MEMORY_LIMIT_BYTES,
  MemoriesState,
  formatMemorySize,
  memoriesState,
  memoryFullness,
  memoryUpdatedAgo,
  orderMemories,
} from '../src/composables/useMemories';

const named = (...names) => names.map((name) => ({ name }));

describe('orderMemories', () => {
  it('puts the index first, because it explains the rest', () => {
    const ordered = orderMemories(named('zeta.md', 'alpha.md', INDEX_MEMORY));

    expect(ordered.map((m) => m.name)).toEqual([INDEX_MEMORY, 'alpha.md', 'zeta.md']);
  });

  it('orders the rest alphabetically', () => {
    const ordered = orderMemories(named('zeta.md', 'alpha.md', 'mid.md'));

    expect(ordered.map((m) => m.name)).toEqual(['alpha.md', 'mid.md', 'zeta.md']);
  });

  it('leaves the caller\'s array alone', () => {
    const original = named('zeta.md', INDEX_MEMORY);

    orderMemories(original);

    expect(original.map((m) => m.name)).toEqual(['zeta.md', INDEX_MEMORY]);
  });

  it('copes with nothing to order', () => {
    expect(orderMemories()).toEqual([]);
    expect(orderMemories([])).toEqual([]);
  });

  it('finds the index wherever it already sits in the list', () => {
    // Both orders, because the comparator has a branch for each side and the
    // engine's sort only ever visits one of them for a given input.
    expect(orderMemories(named('alpha.md', INDEX_MEMORY)).map((m) => m.name))
      .toEqual([INDEX_MEMORY, 'alpha.md']);
    expect(orderMemories(named(INDEX_MEMORY, 'alpha.md')).map((m) => m.name))
      .toEqual([INDEX_MEMORY, 'alpha.md']);
  });

  it('handles a list that is only the index', () => {
    expect(orderMemories(named(INDEX_MEMORY)).map((m) => m.name)).toEqual([INDEX_MEMORY]);
  });
});

describe('formatMemorySize', () => {
  it('reads small memories in bytes, which is more use than a fraction of a KB', () => {
    expect(formatMemorySize(0)).toBe('0 bytes');
    expect(formatMemorySize(1)).toBe('1 byte');
    expect(formatMemorySize(94)).toBe('94 bytes');
    expect(formatMemorySize(1023)).toBe('1023 bytes');
  });

  it('switches to kilobytes with a decimal, since the cap is only 16', () => {
    expect(formatMemorySize(1024)).toBe('1.0 KB');
    expect(formatMemorySize(1536)).toBe('1.5 KB');
    expect(formatMemorySize(MEMORY_LIMIT_BYTES)).toBe('16.0 KB');
  });

  it('says nothing rather than something wrong', () => {
    expect(formatMemorySize(undefined)).toBe('');
    expect(formatMemorySize(-1)).toBe('');
    expect(formatMemorySize('not a number')).toBe('');
  });
});

describe('memoryFullness', () => {
  it('measures against the 16 KB cap', () => {
    expect(memoryFullness(MEMORY_LIMIT_BYTES)).toBe(100);
    expect(memoryFullness(MEMORY_LIMIT_BYTES / 2)).toBe(50);
    expect(memoryFullness(0)).toBe(0);
  });

  it('never reports more than full', () => {
    // The tools refuse an over-size save, but a memory stored before a limit
    // changed should still read as full rather than as 130%.
    expect(memoryFullness(MEMORY_LIMIT_BYTES * 1.3)).toBe(100);
  });

  it('treats nonsense as empty', () => {
    expect(memoryFullness(undefined)).toBe(0);
    expect(memoryFullness(-5)).toBe(0);
  });
});

describe('memoryUpdatedAgo', () => {
  const now = new Date('2026-09-06T12:00:00Z');
  const ago = (ms) => memoryUpdatedAgo(new Date(now.getTime() - ms), now);

  it('reads in the units that answer "is this still current?"', () => {
    expect(ago(5 * 1000)).toBe('just now');
    expect(ago(5 * 60 * 1000)).toBe('5m ago');
    expect(ago(3 * 60 * 60 * 1000)).toBe('3h ago');
    expect(ago(4 * 24 * 60 * 60 * 1000)).toBe('4d ago');
  });

  it('falls back to a date once "days ago" stops being useful', () => {
    expect(ago(90 * 24 * 60 * 60 * 1000)).toMatch(/\d/);
    expect(ago(90 * 24 * 60 * 60 * 1000)).not.toContain('ago');
  });

  it('does not report a write as being in the future', () => {
    // Server and browser clocks disagree by seconds; "in 3 seconds" would read
    // as a bug rather than as rounding.
    expect(memoryUpdatedAgo(new Date(now.getTime() + 3000), now)).toBe('just now');
  });

  it('says nothing for a timestamp it cannot read', () => {
    expect(memoryUpdatedAgo('not a date', now)).toBe('');
    expect(memoryUpdatedAgo(undefined, now)).toBe('');
  });

  it('uses the current time when none is given', () => {
    expect(memoryUpdatedAgo(new Date())).toBe('just now');
  });
});

describe('memoriesState', () => {
  it('is loading while the request is in flight', () => {
    expect(memoriesState({ loading: true, error: null, memories: [] })).toBe(MemoriesState.Loading);
  });

  it('is ready when there is something to show', () => {
    expect(memoriesState({ loading: false, error: null, memories: named('a.md') })).toBe(MemoriesState.Ready);
  });

  it('is empty when the workspace simply has none', () => {
    expect(memoriesState({ loading: false, error: null, memories: [] })).toBe(MemoriesState.Empty);
  });

  it('distinguishes a failed fetch from an empty workspace', () => {
    // Both leave the list empty. Telling someone their agents have remembered
    // nothing when the request failed is the one wrong answer here.
    expect(memoriesState({ loading: false, error: new Error('offline'), memories: [] }))
      .toBe(MemoriesState.Failed);
  });

  it('reports a failure even when stale memories are still on screen', () => {
    expect(memoriesState({ loading: false, error: new Error('offline'), memories: named('a.md') }))
      .toBe(MemoriesState.Failed);
  });

  it('survives a state with nothing in it', () => {
    expect(memoriesState({ loading: false, error: null, memories: undefined })).toBe(MemoriesState.Empty);
  });
});

describe('memoryLinkTarget', () => {
  it('reads the memory a link names', () => {
    expect(memoryLinkTarget('memory://deploys.md')).toBe('deploys.md');
  });

  it('canonicalises the name the way the tools store it', () => {
    // memory://MEMORY.md and memory://memory.md are the same memory, so the
    // link has to resolve to the one stored spelling.
    expect(memoryLinkTarget('memory://MEMORY.md')).toBe('memory.md');
    expect(memoryLinkTarget('memory://Release-Notes.MD')).toBe('release-notes.md');
  });

  it('accepts the single-slash spelling too', () => {
    expect(memoryLinkTarget('memory:/deploys.md')).toBe('deploys.md');
    expect(memoryLinkTarget('memory:deploys.md')).toBe('deploys.md');
  });

  it('is not fooled by something that merely mentions memory', () => {
    expect(memoryLinkTarget('https://example.com/memory://x.md')).toBe('');
    expect(memoryLinkTarget('deploys.md')).toBe('');
    expect(memoryLinkTarget('')).toBe('');
    expect(memoryLinkTarget(undefined)).toBe('');
  });
});

describe('memory links in rendered markdown', () => {
  const render = (md) => {
    document.body.innerHTML = `<div id="root">${renderMarkdown(md)}</div>`;
    return document.querySelector('#root');
  };

  it('renders a link the app answers rather than one the browser follows', () => {
    const root = render('[how we ship](memory://deploys.md)');
    const anchor = root.querySelector('a');

    expect(anchor.getAttribute(MEMORY_LINK_ATTR)).toBe('deploys.md');
    // No href at all: the browser must never navigate to this.
    expect(anchor.hasAttribute('href')).toBe(false);
    expect(anchor.textContent).toBe('how we ship');
  });

  it('offers to copy the memory name', () => {
    const root = render('[i](memory://deploys.md)');

    expect(root.querySelector('button')?.getAttribute('data-copy-text')).toBe('deploys.md');
  });

  it('renders a link the app cannot follow as text, not as a dead anchor', () => {
    // This is the bug it exists for: a relative link used to navigate the whole
    // page to a path no route matches, blanking the screen.
    const root = render('[notes](deploys.md)');

    expect(root.querySelector('a')).toBeNull();
    expect(root.querySelector('.md-dead-link')?.textContent).toBe('notes');
    // The target is kept where it can still be read.
    expect(root.querySelector('.md-dead-link')?.getAttribute('title')).toBe('deploys.md');
  });

  it('leaves links that do work alone', () => {
    for (const [md, href] of [
      ['[web](https://agentrq.com)', 'https://agentrq.com'],
      ['[mail](mailto:hi@agentrq.com)', 'mailto:hi@agentrq.com'],
    ]) {
      const root = render(md);
      expect(root.querySelector('a')?.getAttribute('href')).toBe(href);
    }
  });

  it('still renders a local file link as one', () => {
    const root = render('[plan](file:///Users/mt/plan.md)');

    expect(root.querySelector('a')?.getAttribute('data-file-url')).toBe('file:///Users/mt/plan.md');
  });
});

describe('memoryLinkFromEvent', () => {
  const mount = (md) => {
    document.body.innerHTML = `<div id="root">${renderMarkdown(md)}</div>`;
    return document.querySelector('a');
  };

  it('finds the memory a click landed on', () => {
    const anchor = mount('[how we ship](memory://deploys.md)');

    expect(memoryLinkFromEvent({ target: anchor })).toBe('deploys.md');
  });

  it('finds it from a click on text inside the link', () => {
    const anchor = mount('[**bold**](memory://deploys.md)');

    expect(memoryLinkFromEvent({ target: anchor.querySelector('strong') })).toBe('deploys.md');
  });

  it('ignores a click on anything else', () => {
    mount('[web](https://agentrq.com)');

    expect(memoryLinkFromEvent({ target: document.querySelector('a') })).toBe('');
    expect(memoryLinkFromEvent({ target: null })).toBe('');
    expect(memoryLinkFromEvent(undefined)).toBe('');
  });
});
