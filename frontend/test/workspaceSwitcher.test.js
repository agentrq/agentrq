import { describe, it, expect } from 'vitest';

import {
  isCurrentWorkspace,
  matchWorkspaces,
  workspaceRoute,
} from '../src/composables/useWorkspaceSwitcher';

/** A workspace as the store holds one, with only the fields matching reads. */
const ws = (name, extra = {}) => ({
  id: String(name).padEnd(11, '0').slice(0, 11),
  name,
  ...extra,
});

// Name-sorted, which is the order the store keeps them in.
const WORKSPACES = [
  ws('blog'),
  ws('blog-drafts'),
  ws('changelog'),
  ws('checkout-service'),
  ws('payments-api'),
];

describe('matchWorkspaces', () => {
  it('lists everything on an empty query, in the order given', () => {
    expect(matchWorkspaces(WORKSPACES, '').map((w) => w.name)).toEqual([
      'blog',
      'blog-drafts',
      'changelog',
      'checkout-service',
      'payments-api',
    ]);
  });

  it('treats whitespace as an empty query rather than as a search', () => {
    expect(matchWorkspaces(WORKSPACES, '   ')).toHaveLength(WORKSPACES.length);
  });

  it('ranks an exact name above a longer name that starts with it', () => {
    // The case that makes exact matching worth having: without it, "blog" is
    // unreachable by typing "blog" whenever "blog-drafts" sorts first.
    const [first] = matchWorkspaces(WORKSPACES, 'blog');

    expect(first.name).toBe('blog');
  });

  it('matches a name prefix', () => {
    expect(matchWorkspaces(WORKSPACES, 'check').map((w) => w.name)).toEqual(['checkout-service']);
  });

  it('matches the middle of a hyphenated name', () => {
    // Most of how these get named, so a substring match is the difference
    // between two keystrokes and giving up.
    expect(matchWorkspaces(WORKSPACES, 'api').map((w) => w.name)).toEqual(['payments-api']);
  });

  it('ignores case and surrounding space', () => {
    expect(matchWorkspaces(WORKSPACES, '  CHECKOUT ').map((w) => w.name)).toEqual([
      'checkout-service',
    ]);
  });

  it('puts prefix matches ahead of substring matches', () => {
    const list = [ws('my-blog'), ws('blog-drafts')];

    expect(matchWorkspaces(list, 'blog').map((w) => w.name)).toEqual(['blog-drafts', 'my-blog']);
  });

  it('finds a workspace by a full pasted ID', () => {
    const target = WORKSPACES[3];

    expect(matchWorkspaces(WORKSPACES, target.id).map((w) => w.name)).toEqual([target.name]);
  });

  it('does not match a partial ID, which is never typed from memory', () => {
    // The id is deliberately unlike the name here: the shared `ws()` helper
    // derives one from the other, which would let a name prefix answer and
    // hide whether the ID rule did anything.
    const list = [{ id: '0eCTDeDXETx', name: 'checkout-service' }];

    expect(matchWorkspaces(list, '0eCTDeD')).toEqual([]);
    // The full ID still lands.
    expect(matchWorkspaces(list, '0eCTDeDXETx')).toHaveLength(1);
  });

  it('matches an ID whatever case it is pasted in', () => {
    const list = [{ id: '0eCTDeDXETx', name: 'checkout-service' }];

    expect(matchWorkspaces(list, '0ectdedxetx')).toHaveLength(1);
  });

  it('does not match an 11-character query that is not the ID', () => {
    const list = [{ id: '0eCTDeDXETx', name: 'checkout-service' }];

    expect(matchWorkspaces(list, 'notanywsid1')).toEqual([]);
  });

  it('returns nothing when no name matches', () => {
    expect(matchWorkspaces(WORKSPACES, 'nothing-like-this')).toEqual([]);
  });

  it('honours the limit', () => {
    expect(matchWorkspaces(WORKSPACES, '', 2)).toHaveLength(2);
  });

  it('survives a missing or malformed list', () => {
    expect(matchWorkspaces(undefined, 'blog')).toEqual([]);
    expect(matchWorkspaces(null, '')).toEqual([]);
    expect(matchWorkspaces('not a list', '')).toEqual([]);
  });

  it('survives rows with no name', () => {
    const list = [{ id: 'aaaaaaaaaaa' }, ws('blog')];

    expect(matchWorkspaces(list, 'blog').map((w) => w.name)).toEqual(['blog']);
    expect(matchWorkspaces(list, '')).toHaveLength(2);
  });

  it('survives a row with no id when the query is ID-shaped', () => {
    // Reaches the ID comparison with nothing to compare: the name checks have
    // to fail first, which needs a query that is 11 characters *and* matches no
    // name.
    const list = [{ name: 'blog' }];

    expect(matchWorkspaces(list, 'zzzzzzzzzzz')).toEqual([]);
  });

  it('treats a null query as empty', () => {
    expect(matchWorkspaces(WORKSPACES, null)).toHaveLength(WORKSPACES.length);
    expect(matchWorkspaces(WORKSPACES, undefined)).toHaveLength(WORKSPACES.length);
  });

  describe('archived workspaces', () => {
    const withArchived = [
      ws('active-one'),
      ws('archived-one', { archivedAt: '2026-01-01T00:00:00Z' }),
      ws('active-two'),
    ];

    it('sinks them to the bottom on an empty query', () => {
      // The sidebar lists archived workspaces, so hiding them here would make
      // the switcher disagree with the navigation beside it — but they are not
      // what someone switching workspaces is reaching for.
      expect(matchWorkspaces(withArchived, '').map((w) => w.name)).toEqual([
        'active-one',
        'active-two',
        'archived-one',
      ]);
    });

    it('still lists them, rather than hiding them', () => {
      expect(matchWorkspaces(withArchived, 'archived').map((w) => w.name)).toEqual([
        'archived-one',
      ]);
    });

    it('ranks an active workspace first when both match equally well', () => {
      const both = [
        ws('shop-archive', { archivedAt: '2026-01-01T00:00:00Z' }),
        ws('shop-active'),
      ];

      expect(matchWorkspaces(both, 'shop').map((w) => w.name)).toEqual([
        'shop-active',
        'shop-archive',
      ]);
    });

    it('lets an exact name win outright, archived or not', () => {
      // Typing a whole name is unambiguous: it should not be beaten by a
      // fuzzier match that happens to be active.
      const both = [ws('shop-active'), ws('shop', { archivedAt: '2026-01-01T00:00:00Z' })];

      expect(matchWorkspaces(both, 'shop').map((w) => w.name)).toEqual(['shop', 'shop-active']);
    });
  });
});

describe('workspaceRoute', () => {
  it('opens the workspace, where its sidebar entry and its card both go', () => {
    expect(workspaceRoute({ id: 'ws1' })).toBe('/workspaces/ws1');
  });

  it('accepts a bare id', () => {
    expect(workspaceRoute('ws1')).toBe('/workspaces/ws1');
  });

  it('falls back to the overview when there is nothing to open', () => {
    expect(workspaceRoute(null)).toBe('/');
    expect(workspaceRoute(undefined)).toBe('/');
    expect(workspaceRoute({})).toBe('/');
    expect(workspaceRoute('')).toBe('/');
  });
});

describe('isCurrentWorkspace', () => {
  it('recognises the workspace on screen', () => {
    expect(isCurrentWorkspace({ id: 'ws1' }, 'ws1')).toBe(true);
  });

  it('compares as strings, since a route param is one and the store may not be', () => {
    expect(isCurrentWorkspace({ id: 42 }, '42')).toBe(true);
    expect(isCurrentWorkspace({ id: '42' }, 42)).toBe(true);
  });

  it('is false for a different workspace', () => {
    expect(isCurrentWorkspace({ id: 'ws1' }, 'ws2')).toBe(false);
  });

  it('is false when there is no workspace in context', () => {
    // The overview has no workspace, and marking every row "current" there
    // would be worse than marking none.
    expect(isCurrentWorkspace({ id: 'ws1' }, '')).toBe(false);
    expect(isCurrentWorkspace({ id: 'ws1' }, null)).toBe(false);
    expect(isCurrentWorkspace({ id: 'ws1' }, undefined)).toBe(false);
  });

  it('is false when the row has no id', () => {
    expect(isCurrentWorkspace({}, 'ws1')).toBe(false);
    expect(isCurrentWorkspace(null, 'ws1')).toBe(false);
  });
});
