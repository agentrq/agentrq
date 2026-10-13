// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi } from 'vitest';

import { renderMarkdown } from '../src/utils/markdown';
import {
  INDEX_MEMORY,
  MEMORY_LINK_ATTR,
  memoryLinkFromEvent,
  memoryLinkTarget,
  MemoriesState,
  formatMemorySize,
  memoriesState,
  memoryLinksIn,
  memoryTitle,
  orderMemories,
  unlinkedMemories,
  useMemoryReader,
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
    expect(formatMemorySize(16 * 1024)).toBe('16.0 KB');
  });

  it('says nothing rather than something wrong', () => {
    expect(formatMemorySize(undefined)).toBe('');
    expect(formatMemorySize(-1)).toBe('');
    expect(formatMemorySize('not a number')).toBe('');
  });
});

describe('memoriesState', () => {
  it('is loading while the request is in flight', () => {
    expect(memoriesState({ loading: true, error: null, memories: [] })).toBe(MemoriesState.Loading);
  });

  it('is ready when there is an index to show', () => {
    expect(memoriesState({ loading: false, error: null, memories: named('a.md', INDEX_MEMORY) })).toBe(MemoriesState.Ready);
  });

  it('tells memories with no index apart from an empty workspace', () => {
    expect(memoriesState({ loading: false, error: null, memories: named('a.md') })).toBe(MemoriesState.NoIndex);
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
    expect(root.querySelector('.md-dead-link')?.hasAttribute('data-link-title')).toBe(false);
  });

  it('keeps a dead link\'s own title, for a page that reads it', () => {
    const root = render('[@ops](/workspaces/w/tasks/t "ongoing")');
    expect(root.querySelector('.md-dead-link')?.dataset.linkTitle).toBe('ongoing');
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

describe('memoryTitle', () => {
  it('shows the index as MEMORY.md, the name people know it by', () => {
    expect(memoryTitle(INDEX_MEMORY)).toBe('MEMORY.md');
  });

  it('shows every other memory as stored', () => {
    expect(memoryTitle('deploys.md')).toBe('deploys.md');
  });
});

describe('memoryLinksIn', () => {
  it('lists the memories the markdown links to, once each and in order', () => {
    const md = '- [ship](memory://deploys.md)\n- [tests](memory:Tests.md)\n- again [ship](memory://deploys.md)';

    expect(memoryLinksIn(md)).toEqual(['deploys.md', 'tests.md']);
  });

  it('finds links inside tables and autolinks', () => {
    expect(memoryLinksIn('see <memory://a.md>\n\n| x |\n|---|\n| [b](memory://b.md) |')).toEqual(['a.md', 'b.md']);
  });

  it('ignores a name that is not a link, and links that are not memories', () => {
    expect(memoryLinksIn('`memory://code.md` and memory words, [site](https://example.com)')).toEqual([]);
  });

  it('survives nothing', () => {
    expect(memoryLinksIn(undefined)).toEqual([]);
  });
});

describe('unlinkedMemories', () => {
  it('lists what the index leaves out, alphabetically, never the index itself', () => {
    const memories = named('zeta.md', INDEX_MEMORY, 'linked.md', 'alpha.md');

    expect(unlinkedMemories(memories, '[l](memory://linked.md)')).toEqual(['alpha.md', 'zeta.md']);
  });

  it('lists every memory when there is no index to read', () => {
    expect(unlinkedMemories(named('b.md', 'a.md'))).toEqual(['a.md', 'b.md']);
    expect(unlinkedMemories()).toEqual([]);
  });
});

describe('useMemoryReader', () => {
  const flush = () => new Promise((r) => setTimeout(r, 0));

  function reader({ list = named(INDEX_MEMORY, 'a.md', 'b.md'), contents = {}, listError, oneError } = {}) {
    const fetchList = vi.fn(async () => {
      if (listError) throw listError;
      return { memories: list };
    });
    const fetchOne = vi.fn(async (_id, name) => {
      if (oneError) throw oneError;
      return { memory: { content: contents[name] ?? `# ${name}` } };
    });
    return { fetchList, fetchOne, r: useMemoryReader(() => 'W1', { fetchList, fetchOne }) };
  }

  it('opens memory.md once the list arrives', async () => {
    const { r, fetchList, fetchOne } = reader();

    const done = r.load();
    expect(r.state.value).toBe(MemoriesState.Loading);
    await done;
    await flush();

    expect(fetchList).toHaveBeenCalledWith('W1');
    expect(fetchOne).toHaveBeenCalledWith('W1', INDEX_MEMORY);
    expect(r.state.value).toBe(MemoriesState.Ready);
    expect(r.current).toMatchObject({ name: INDEX_MEMORY, content: `# ${INDEX_MEMORY}`, loading: false });
  });

  it('lists what the index does not link to, only on the index', async () => {
    const { r } = reader({ contents: { [INDEX_MEMORY]: '[a](memory://a.md)' } });
    await r.load();
    await flush();

    expect(r.listed.value).toEqual(['b.md']);

    r.open('a.md');
    expect(r.listed.value).toEqual([]);
  });

  it('follows a link in place, and Back retraces the way', async () => {
    const { r } = reader();
    await r.load();
    await flush();

    const link = document.createElement('a');
    link.setAttribute(MEMORY_LINK_ATTR, 'a.md');
    const event = { target: link, preventDefault: vi.fn() };
    r.follow(event);
    await flush();

    expect(event.preventDefault).toHaveBeenCalled();
    expect(r.current).toMatchObject({ name: 'a.md', content: '# a.md' });
    expect(r.history.value).toEqual([INDEX_MEMORY]);

    r.back();
    await flush();
    expect(r.current.name).toBe(INDEX_MEMORY);
    expect(r.history.value).toEqual([]);
  });

  it('leaves a click that is not on a memory link alone', async () => {
    const { r } = reader();
    await r.load();
    await flush();
    const event = { target: document.createElement('p'), preventDefault: vi.fn() };

    r.follow(event);

    expect(event.preventDefault).not.toHaveBeenCalled();
    expect(r.history.value).toEqual([]);
  });

  it('does not stack the memory already open, or nothing at all', async () => {
    const { r } = reader();
    await r.load();

    r.open(INDEX_MEMORY);
    r.open('');

    expect(r.history.value).toEqual([]);
  });

  it('says in place when a link names a memory nobody saved', async () => {
    const { r, fetchOne } = reader();
    await r.load();
    await flush();
    fetchOne.mockClear();

    r.open('later.md');

    expect(fetchOne).not.toHaveBeenCalled();
    expect(r.current).toMatchObject({ name: 'later.md', missing: 'Nothing saved under later.md yet.', loading: false });
  });

  it('reports a memory that would not load', async () => {
    const { r } = reader({ oneError: new Error('offline') });
    await r.load();
    await flush();

    expect(r.current).toMatchObject({ name: INDEX_MEMORY, error: 'Could not load this memory.', loading: false });
    expect(r.listed.value).toEqual([]);
  });

  it('reads an empty memory as empty', async () => {
    const { r, fetchOne } = reader();
    fetchOne.mockResolvedValue({});
    await r.load();
    await flush();

    expect(r.current.content).toBe('');
  });

  it('starts with nothing open when there is no index, and Back returns there', async () => {
    const { r, fetchOne } = reader({ list: named('b.md', 'a.md') });
    await r.load();

    expect(r.state.value).toBe(MemoriesState.NoIndex);
    expect(fetchOne).not.toHaveBeenCalled();
    expect(r.current.name).toBe('');
    expect(r.listed.value).toEqual(['a.md', 'b.md']);

    r.open('a.md');
    await flush();
    expect(r.current.content).toBe('# a.md');

    r.back();
    expect(r.current.name).toBe('');
    expect(r.listed.value).toEqual(['a.md', 'b.md']);

    r.back();
    expect(r.current.name).toBe('');
  });

  it('is empty for a workspace with no memories', async () => {
    const { r } = reader({ list: [] });
    r.load();
    await flush();
    expect(r.state.value).toBe(MemoriesState.Empty);

    const { r: r2, fetchList } = reader();
    fetchList.mockResolvedValue({});
    await r2.load();
    expect(r2.state.value).toBe(MemoriesState.Empty);
  });

  it('fails rather than reading as empty when the list will not load', async () => {
    const { r } = reader({ listError: new Error('offline') });
    await r.load();

    expect(r.state.value).toBe(MemoriesState.Failed);
    expect(r.memories.value).toEqual([]);
  });

  it('lets only the latest load write', async () => {
    let resolveFirst;
    let rejectSecond;
    const fetchList = vi.fn()
      .mockImplementationOnce(() => new Promise((res) => { resolveFirst = res; }))
      .mockImplementationOnce(() => new Promise((_, rej) => { rejectSecond = rej; }))
      .mockImplementationOnce(async () => ({ memories: named(INDEX_MEMORY) }));
    let resolveOne;
    let rejectOne;
    const fetchOne = vi.fn()
      .mockImplementationOnce(() => new Promise((res) => { resolveOne = res; }))
      .mockImplementationOnce(() => new Promise((_, rej) => { rejectOne = rej; }))
      .mockImplementation(async () => ({ memory: { content: 'latest' } }));
    const r = useMemoryReader(() => 'W1', { fetchList, fetchOne });

    const first = r.load();
    const second = r.load();
    const third = r.load();
    resolveFirst({ memories: named('stale.md') });
    rejectSecond(new Error('superseded request failed'));
    await Promise.all([first, second, third]);
    expect(r.memories.value.map((m) => m.name)).toEqual([INDEX_MEMORY]);
    expect(r.state.value).toBe(MemoriesState.Ready);

    // Three reads of the index overlap; the answers to the earlier two land
    // after the last and must not replace it.
    r.open('x.md');
    r.back();
    r.open('x.md');
    r.back();
    await flush();
    const shown = () => r.current.content;
    expect(shown()).toBe('latest');
    resolveOne({ memory: { content: 'stale' } });
    rejectOne(new Error('superseded request failed'));
    await flush();
    expect(shown()).toBe('latest');
    expect(r.current.error).toBe('');
    expect(r.current.loading).toBe(false);
  });
});
