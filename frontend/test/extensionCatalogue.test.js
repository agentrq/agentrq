import { describe, it, expect, vi, afterEach } from 'vitest';

import {
  GROUPS,
  blockedReason,
  groupFor,
  summarise,
  toRows,
  useExtensionCatalogue,
} from '../src/composables/useExtensionCatalogue';

/**
 * The catalogue is uncurated: anyone may publish by adding a topic to a
 * repository. So the screen's job is less "present a list" than "explain
 * itself" — every row that cannot be used says why, and nothing is hidden.
 */

const entry = (over = {}) => ({
  fullName: 'owner/thing',
  owner: 'owner',
  stars: 10,
  ok: true,
  compatible: true,
  reasons: [],
  manifest: { name: 'thing', displayName: 'Thing', license: 'MIT' },
  ...over,
});

afterEach(() => {
  delete window.agentrq;
});

describe('groupFor', () => {
  it('puts what is usable where it can be chosen', () => {
    expect(groupFor(entry())).toBe('available');
  });

  it('puts what is installed first, whatever else is true of it', () => {
    // An extension that stopped being compatible after an app downgrade is
    // still installed, and burying it among the unavailable is how somebody
    // fails to find the thing they need to remove.
    expect(groupFor(entry({ compatible: false }), { installed: true })).toBe('installed');
    expect(groupFor(entry({ ok: false }), { installed: true })).toBe('installed');
  });

  it('puts a broken or incompatible entry out of the way, not out of sight', () => {
    expect(groupFor(entry({ ok: false }))).toBe('unavailable');
    expect(groupFor(entry({ compatible: false }))).toBe('unavailable');
  });
});

describe('blockedReason', () => {
  it('is empty for something that works', () => {
    expect(blockedReason(entry())).toBe('');
  });

  it('reports a manifest that could not be read', () => {
    expect(blockedReason(entry({ ok: false, reason: '"license" is required.' }))).toBe(
      '"license" is required.',
    );
  });

  it('still says something when a broken entry carries no reason', () => {
    expect(blockedReason({ ok: false })).toBe('This manifest could not be read.');
  });

  it('gives every incompatibility, not just the first', () => {
    // Somebody fixing a manifest wants the whole list.
    const row = entry({
      compatible: false,
      reasons: ['Needs AgentRQ ^1.5; this is 1.4.0.', 'The workspace server does not offer "x".'],
    });

    expect(blockedReason(row)).toContain('Needs AgentRQ ^1.5');
    expect(blockedReason(row)).toContain('does not offer "x"');
  });

  it('copes with an incompatible entry that lists no reasons', () => {
    expect(blockedReason(entry({ compatible: false, reasons: undefined }))).toBe('');
  });
});

describe('toRows', () => {
  it('groups, and drops the groups that would be empty', () => {
    const index = { entries: [entry()] };

    const sections = toRows(index);

    expect(sections).toHaveLength(1);
    expect(sections[0].group).toBe('available');
  });

  it('orders the groups the way the screen reads', () => {
    const index = {
      entries: [
        entry({ fullName: 'a/broken', ok: false, reason: 'bad', manifest: undefined }),
        entry({ fullName: 'b/installed', manifest: { name: 'installed' } }),
        entry({ fullName: 'c/available' }),
      ],
    };

    const sections = toRows(index, { installedNames: ['installed'] });

    expect(sections.map((s) => s.group)).toEqual(GROUPS);
  });

  it('sorts by stars, then by name so the order is stable', () => {
    // Stars are the only signal an uncurated catalogue carries; the name breaks
    // the tie rather than leaving it to whatever the search returned.
    const index = {
      entries: [
        entry({ fullName: 'z/one', stars: 5 }),
        entry({ fullName: 'a/two', stars: 5 }),
        entry({ fullName: 'm/three', stars: 99 }),
      ],
    };

    const [{ rows }] = toRows(index);

    expect(rows.map((r) => r.fullName)).toEqual(['m/three', 'a/two', 'z/one']);
  });

  it('marks what is installed from the machine, not from the catalogue', () => {
    // An extension pulled from GitHub is still on this machine, and the row
    // needed to uninstall it is exactly the one a catalogue-driven list drops.
    const index = { entries: [entry({ manifest: { name: 'thing' } })] };

    const [{ group, rows }] = toRows(index, { installedNames: ['thing'] });

    expect(group).toBe('installed');
    expect(rows[0].installed).toBe(true);
  });

  it('treats an unparsed entry as incompatible without a manifest to judge', () => {
    const index = { entries: [entry({ ok: false, reason: 'bad', manifest: undefined })] };

    const [{ rows }] = toRows(index);

    expect(rows[0].compatible).toBe(false);
    expect(rows[0].blocked).toBe('bad');
  });

  it('copes with an empty or missing catalogue', () => {
    expect(toRows(undefined)).toEqual([]);
    expect(toRows({ entries: [] })).toEqual([]);
  });

  it('defaults stars when an entry carries none, on either side of a comparison', () => {
    // A brand-new repository has no stars at all, and it must not sort
    // unpredictably depending on which side of the comparison it lands.
    const index = {
      entries: [
        entry({ fullName: 'a/none', stars: undefined }),
        entry({ fullName: 'b/one', stars: 1 }),
        entry({ fullName: 'c/none', stars: undefined }),
      ],
    };

    const [{ rows }] = toRows(index);

    expect(rows.map((r) => r.fullName)).toEqual(['b/one', 'a/none', 'c/none']);
  });

  it('copes with a compatible entry that lists no reasons at all', () => {
    // The ordinary case once the main process has judged it: compatible, with
    // the reasons field simply absent rather than an empty array.
    const index = { entries: [entry({ reasons: undefined })] };

    const [{ rows }] = toRows(index);

    expect(rows[0].reasons).toEqual([]);
    expect(rows[0].blocked).toBe('');
  });
});

describe('summarise', () => {
  const sectionsFor = (index) => toRows(index);

  it('says so when there is nothing yet', () => {
    expect(summarise({ entries: [] }, [])).toBe('No extensions found yet.');
  });

  it('counts what there is, in the singular where it should', () => {
    const index = { entries: [entry()] };
    expect(summarise(index, sectionsFor(index))).toBe('1 extension');

    const two = { entries: [entry(), entry({ fullName: 'b/x' })] };
    expect(summarise(two, sectionsFor(two))).toBe('2 extensions');
  });

  it('calls out how many cannot be used', () => {
    const index = { entries: [entry(), entry({ fullName: 'b/x', ok: false, reason: 'bad' })] };
    expect(summarise(index, sectionsFor(index))).toBe('2 extensions · 1 unavailable');
  });

  it('says out loud when the list is incomplete', () => {
    // A truncated catalogue is indistinguishable from a complete one unless it
    // says so, and this one can be: GitHub returns at most 1000 results.
    const index = { entries: [entry()], truncated: true };
    expect(summarise(index, sectionsFor(index))).toBe('1 extension · list is incomplete');
  });

  it('copes with no index at all', () => {
    expect(summarise(undefined, [])).toBe('No extensions found yet.');
  });
});

describe('useExtensionCatalogue', () => {
  const fakeBridge = (over = {}) => ({
    state: vi.fn(async () => ({ index: { entries: [entry()] }, installed: [] })),
    refresh: vi.fn(async () => ({ ok: true, index: { entries: [entry(), entry({ fullName: 'b/x' })] } })),
    ...over,
  });

  it('stays inert with no bridge, so the web build is absent rather than broken', async () => {
    const catalogue = useExtensionCatalogue({ bridge: undefined });

    expect(catalogue.available).toBe(false);
    await catalogue.load();
    await catalogue.refresh();

    expect(catalogue.sections.value).toEqual([]);
    expect(catalogue.error.value).toBe('');
  });

  it('reads what is already known without going anywhere', async () => {
    // Ten searches a minute is not a budget to spend on somebody opening a
    // screen and closing it again.
    const bridge = fakeBridge();
    const catalogue = useExtensionCatalogue({ bridge });

    await catalogue.load();

    expect(bridge.state).toHaveBeenCalledOnce();
    expect(bridge.refresh).not.toHaveBeenCalled();
    expect(catalogue.sections.value[0].rows).toHaveLength(1);
  });

  it('searches only when asked', async () => {
    const bridge = fakeBridge();
    const catalogue = useExtensionCatalogue({ bridge });

    await catalogue.refresh();

    expect(bridge.refresh).toHaveBeenCalledOnce();
    expect(catalogue.sections.value[0].rows).toHaveLength(2);
  });

  it('shows why a refresh failed, and keeps what it had', async () => {
    const bridge = fakeBridge();
    const catalogue = useExtensionCatalogue({ bridge });
    await catalogue.load();

    bridge.refresh.mockResolvedValueOnce({ ok: false, reason: 'GitHub rate limit reached.' });
    await catalogue.refresh();

    expect(catalogue.error.value).toBe('GitHub rate limit reached.');
    expect(catalogue.sections.value[0].rows).toHaveLength(1);
  });

  it('reports a refusal that carries no reason', async () => {
    const bridge = fakeBridge({ refresh: vi.fn(async () => ({ ok: false })) });
    const catalogue = useExtensionCatalogue({ bridge });

    await catalogue.refresh();

    expect(catalogue.error.value).toBe('Could not refresh.');
  });

  it('survives a bridge that throws', async () => {
    const bridge = fakeBridge({
      state: vi.fn(async () => {
        throw new Error('bridge gone');
      }),
      refresh: vi.fn(async () => {
        throw new Error('bridge gone');
      }),
    });
    const catalogue = useExtensionCatalogue({ bridge });

    await catalogue.load();
    expect(catalogue.error.value).toBe('bridge gone');

    await catalogue.refresh();
    expect(catalogue.error.value).toBe('bridge gone');
    expect(catalogue.loading.value).toBe(false);
  });

  it('says something even when the failure carries no message', async () => {
    const bridge = fakeBridge({
      state: vi.fn(async () => {
        throw {};
      }),
      refresh: vi.fn(async () => {
        throw {};
      }),
    });
    const catalogue = useExtensionCatalogue({ bridge });

    await catalogue.load();
    expect(catalogue.error.value).toBe('Could not read the extension catalogue.');

    await catalogue.refresh();
    expect(catalogue.error.value).toBe('Could not refresh the extension catalogue.');
  });

  it('keeps what it had when the bridge answers with nothing', async () => {
    const bridge = fakeBridge({ state: vi.fn(async () => undefined), refresh: vi.fn(async () => undefined) });
    const catalogue = useExtensionCatalogue({ bridge });

    await catalogue.load();
    await catalogue.refresh();

    expect(catalogue.sections.value).toEqual([]);
    expect(catalogue.error.value).toBe('');
  });

  it('finds the bridge on the window when none is passed', () => {
    window.agentrq = { extensions: fakeBridge() };
    expect(useExtensionCatalogue().available).toBe(true);
  });
});
