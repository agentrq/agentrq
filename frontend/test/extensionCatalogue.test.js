import { describe, it, expect, vi, afterEach } from 'vitest';

import {
  GROUPS,
  INSTALL_STEP,
  blockedReason,
  groupFor,
  asksFor,
  installedRow,
  statusOf,
  summarise,
  toRows,
  useExtensionCatalogue,
} from '../src/composables/useExtensionCatalogue';

/**
 * The catalogue is uncurated: anyone may publish by adding a topic to a
 * repository. So the screen's job is less "present a list" than "explain
 * itself" — every row that cannot be used says why, and nothing is hidden.
 */

/**
 * A published release, which is what makes an entry installable at all: the
 * asset is fetched by name and pinned by digest.
 */
const RELEASE = {
  release: 'v1.0.0',
  asset: 'thing-1.0.0.tgz',
  sha256: 'a'.repeat(64),
};

const entry = (over = {}) => ({
  fullName: 'owner/thing',
  owner: 'owner',
  stars: 10,
  ok: true,
  compatible: true,
  reasons: [],
  manifest: { name: 'thing', displayName: 'Thing', license: 'MIT', artifact: { ...RELEASE } },
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

  // The catalogue is uncurated, and an author who has not cut a release yet
  // looks exactly like one who has — until you press a button that was never
  // going to work. Saying so is the answer somebody actually needs.
  it('says when there is no release to install', () => {
    const noRelease = entry({ manifest: { name: 'thing', displayName: 'Thing', license: 'MIT' } });

    expect(blockedReason(noRelease)).toBe(
      'This extension has not published a release yet, so there is nothing to install.',
    );
    expect(groupFor(noRelease)).toBe('unavailable');
  });

  it('says when the release names no usable checksum', () => {
    // A tag is mutable: an author can move v1.2.0 to different code after you
    // installed it, so without a digest "install v1.2.0" promises nothing.
    const placeholder = entry({
      manifest: { name: 'thing', license: 'MIT', artifact: { ...RELEASE, sha256: '0'.repeat(64) } },
    });
    const nonsense = entry({
      manifest: { name: 'thing', license: 'MIT', artifact: { ...RELEASE, sha256: 'not-a-digest' } },
    });
    const absent = entry({
      manifest: { name: 'thing', license: 'MIT', artifact: { release: 'v1', asset: 'a.tgz' } },
    });

    for (const row of [placeholder, nonsense, absent]) {
      expect(blockedReason(row)).toBe(
        'This extension names a release but no checksum for it, so it cannot be verified.',
      );
      expect(groupFor(row)).toBe('unavailable');
    }
  });

  // Half a release is not a release: an asset with no tag cannot be fetched.
  it('says when the release is only half named', () => {
    const noAsset = entry({
      manifest: { name: 'thing', license: 'MIT', artifact: { release: 'v1', sha256: 'a'.repeat(64) } },
    });

    expect(blockedReason(noAsset)).toContain('has not published a release');
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

    const sections = toRows(index, { installed: [{ name: 'installed', enabled: true, loaded: true }] });

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

  it('builds installed rows from the machine, not from the catalogue', () => {
    // Most installed extensions are not in the catalogue at all: one installed
    // from a folder was never published, and one that was can be taken down.
    // A catalogue-driven list drops exactly the row needed to remove it.
    const index = { entries: [entry({ manifest: { name: 'thing' } })] };

    const [{ group, rows }] = toRows(index, {
      installed: [{ name: 'thing', displayName: 'Thing', enabled: true, loaded: true, contributes: { ui: 1 } }],
    });

    expect(group).toBe('installed');
    expect(rows).toHaveLength(1);
    expect(rows[0].installed).toBe(true);
  });

  it('lists an extension that is on the machine and in no catalogue', () => {
    const [{ group, rows }] = toRows({ entries: [] }, {
      installed: [{ name: 'standup', displayName: 'Standup', enabled: true, loaded: true, contributes: {} }],
    });

    expect(group).toBe('installed');
    expect(rows[0].manifest.displayName).toBe('Standup');
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

describe('statusOf', () => {
  const running = { name: 'standup', enabled: true, loaded: true, failures: 0, contributes: { ui: 1, shortcuts: 1 } };

  it('counts what an extension actually contributed', () => {
    expect(statusOf(running)).toBe('1 view, 1 shortcut');
    expect(statusOf({ ...running, contributes: { ui: 2, schedules: 1 } })).toBe('2 views, 1 schedule');
  });

  // Loaded and registering nothing usually means a setting it needed is blank,
  // which is worth saying rather than showing an empty row.
  it('says so when it loaded and registered nothing', () => {
    expect(statusOf({ ...running, contributes: {} })).toBe('Running, contributing nothing');
    expect(statusOf({ ...running, contributes: { ui: 0 } })).toBe('Running, contributing nothing');
    expect(statusOf({ ...running, contributes: undefined })).toBe('Running, contributing nothing');
  });

  // Enabled and running are different states; a row that conflated them would
  // have nothing to explain why an enabled extension does nothing at all.
  it('separates disabled from not running, and explains the second', () => {
    expect(statusOf({ ...running, enabled: false })).toBe('Disabled');
    expect(statusOf({ ...running, loaded: false })).toBe('Not running');
    expect(statusOf({ ...running, loaded: false, failures: 1 })).toBe('Not running — failed 1 time');
    expect(statusOf({ ...running, loaded: false, failures: 3 })).toBe('Not running — failed 3 times');
  });

  it('names an unknown registry rather than dropping it', () => {
    expect(statusOf({ ...running, contributes: { widgets: 2 } })).toBe('2 widgetss');
  });
});

describe('installedRow', () => {
  it('describes an extension that was never in any catalogue', () => {
    const row = installedRow({
      name: 'standup',
      displayName: 'Standup',
      description: 'What moved today.',
      license: 'MIT',
      source: 'a folder on this machine',
      enabled: true,
      loaded: true,
      contributes: { ui: 1 },
    });

    expect(row).toMatchObject({ group: 'installed', installed: true, compatible: true, blocked: '' });
    expect(row.owner).toBe('a folder on this machine');
    // No star count, rather than a zero that would sort it below everything.
    expect(row.stars).toBeNull();
  });

  it('holds up for an installation that knows almost nothing about itself', () => {
    const row = installedRow({ name: 'x', enabled: true, loaded: true });
    expect(row.owner).toBe('Installed here');
    expect(row.description).toBe('');
  });
});

describe('useExtensionCatalogue', () => {
  const manifest = (over = {}) => ({
    name: 'standup',
    displayName: 'Standup',
    mcp: { workspace: ['listTasks'], supervisor: [] },
    config: [],
    ...over,
  });

  const found = (over = {}) => ({
    ok: true,
    path: '/tmp/standup',
    manifest: manifest(),
    compatible: true,
    reasons: [],
    shortcutProblems: [],
    ...over,
  });

  const fakeBridge = (over = {}) => ({
    state: vi.fn(async () => ({ index: { entries: [entry()] }, installed: [] })),
    refresh: vi.fn(async () => ({ ok: true, index: { entries: [entry(), entry({ fullName: 'b/x' })] } })),
    chooseFolder: vi.fn(async () => found()),
    installLocal: vi.fn(async () => ({ ok: true, installation: { name: 'standup' } })),
    installFromCatalogue: vi.fn(async () => ({ ok: true, installation: { name: 'thing' } })),
    supervisor: vi.fn(async () => ({ authorized: false })),
    authorize: vi.fn(async () => ({ ok: true })),
    deauthorize: vi.fn(async () => ({ ok: true })),
    uninstall: vi.fn(async () => ({ ok: true })),
    setEnabled: vi.fn(async () => ({ ok: true })),
    configure: vi.fn(async () => ({ ok: true })),
    ...over,
  });

  describe('installing from the catalogue', () => {
    const rowFor = async (catalogue) => {
      await catalogue.load();
      return catalogue.sections.value.flatMap((section) => section.rows).find((row) => !row.installed);
    };

    // Only the repository name crosses the bridge. The main process resolves it
    // against its own index, so the release that gets downloaded is decided by
    // what the app discovered rather than by what this screen is holding.
    it('sends the name, not the entry', async () => {
      const bridge = fakeBridge();
      const catalogue = useExtensionCatalogue({ bridge });
      const row = await rowFor(catalogue);

      await catalogue.beginInstall(row);
      await catalogue.install({ grant: null, config: null });

      expect(bridge.installFromCatalogue).toHaveBeenCalledWith('owner/thing', { grant: null, config: null });
      expect(catalogue.notice.value).toBe('Thing installed.');
    });

    // The one place this differs from the folder install, deliberately. Picking
    // a folder is running code you already have; installing from the catalogue
    // downloads a stranger's and runs it with full access to the machine. The
    // screen is not empty either — it names the author, the repository, the
    // version and the licence — so there is something to read before agreeing.
    it('asks before installing even when it wants no permission at all', async () => {
      const bridge = fakeBridge();
      const catalogue = useExtensionCatalogue({ bridge });
      const row = await rowFor(catalogue);

      expect(asksFor(row.manifest)).toBe(0);

      await catalogue.beginInstall(row);

      expect(catalogue.step.value).toBe('asking');
      expect(bridge.installFromCatalogue).not.toHaveBeenCalled();
    });

    // Cancelling is a decision, and it must install nothing.
    it('installs nothing when the question is declined', async () => {
      const bridge = fakeBridge();
      const catalogue = useExtensionCatalogue({ bridge });

      await catalogue.beginInstall(await rowFor(catalogue));
      catalogue.cancelInstall();

      expect(catalogue.step.value).toBe('idle');
      expect(bridge.installFromCatalogue).not.toHaveBeenCalled();
    });

    // An extension that asks for nothing goes straight to installing.
    // Interposing a permission screen with nothing on it would train people to
    // click past the screen that does have something on it.
    it('stops to ask when the extension asks for something', async () => {
      const asking = entry({
        manifest: {
          name: 'thing',
          displayName: 'Thing',
          license: 'MIT',
          artifact: { ...RELEASE },
          mcp: { workspace: ['createTask'] },
        },
      });
      const bridge = fakeBridge({ state: vi.fn(async () => ({ index: { entries: [asking] }, installed: [] })) });
      const catalogue = useExtensionCatalogue({ bridge });
      const row = await rowFor(catalogue);

      await catalogue.beginInstall(row);

      expect(catalogue.step.value).toBe('asking');
      expect(bridge.installFromCatalogue).not.toHaveBeenCalled();

      await catalogue.install({ grant: { workspace: ['createTask'] }, config: null });

      expect(bridge.installFromCatalogue).toHaveBeenCalledWith('owner/thing', {
        grant: { workspace: ['createTask'] },
        config: null,
      });
    });

    // Settings are entered on that same screen and on no other.
    it('stops to ask for settings alone, with no permission asked for', async () => {
      const configured = entry({
        manifest: {
          name: 'thing',
          displayName: 'Thing',
          license: 'MIT',
          artifact: { ...RELEASE },
          config: [{ key: 'token', type: 'string', label: 'Token' }],
        },
      });
      const bridge = fakeBridge({ state: vi.fn(async () => ({ index: { entries: [configured] }, installed: [] })) });
      const catalogue = useExtensionCatalogue({ bridge });

      await catalogue.beginInstall(await rowFor(catalogue));

      expect(catalogue.step.value).toBe('asking');
    });

    it('carries a refusal through as the sentence it was given', async () => {
      const bridge = fakeBridge({
        installFromCatalogue: vi.fn(async () => ({ ok: false, reason: 'That checksum did not match.' })),
      });
      const catalogue = useExtensionCatalogue({ bridge });

      await catalogue.beginInstall(await rowFor(catalogue));
      await catalogue.install({ grant: null, config: null });

      expect(catalogue.error.value).toBe('That checksum did not match.');
    });

    // Installed but not running is a real outcome and not an error.
    it('says so when it installed but did not start', async () => {
      const bridge = fakeBridge({
        installFromCatalogue: vi.fn(async () => ({ ok: true, loadFailure: 'Cannot find module "x"' })),
      });
      const catalogue = useExtensionCatalogue({ bridge });

      await catalogue.beginInstall(await rowFor(catalogue));
      await catalogue.install({ grant: null, config: null });

      expect(catalogue.notice.value).toBe('Thing installed, but did not start: Cannot find module "x"');
    });

    // The button is not offered on a blocked row. This is the guard for the day
    // somebody offers one anyway.
    it('refuses a row that says why it cannot be used', async () => {
      const bridge = fakeBridge();
      const catalogue = useExtensionCatalogue({ bridge });

      await catalogue.beginInstall({ blocked: 'No release.', manifest: {}, fullName: 'a/b' });

      expect(catalogue.error.value).toBe('No release.');
      expect(bridge.installFromCatalogue).not.toHaveBeenCalled();
    });

    it('has nothing to do without a bridge, a row, or with one already installed', async () => {
      const bridge = fakeBridge();
      const catalogue = useExtensionCatalogue({ bridge });

      await useExtensionCatalogue({ bridge: undefined }).beginInstall({ fullName: 'a/b', manifest: {} });
      await catalogue.beginInstall(undefined);
      await catalogue.beginInstall({ installed: true, fullName: 'a/b', manifest: {} });

      expect(bridge.installFromCatalogue).not.toHaveBeenCalled();
    });
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

  // The line the screen actually shows. `summarise` is tested on its own above,
  // but a lazy `computed` nothing ever reads is a getter that never runs — so
  // this is what proves the screen's own summary is wired to the loaded index
  // rather than to nothing.
  it('summarises what it loaded', async () => {
    const bridge = fakeBridge()
    const catalogue = useExtensionCatalogue({ bridge })

    expect(catalogue.summary.value).toBe('No extensions found yet.')

    await catalogue.load()

    expect(catalogue.summary.value).toBe('1 extension')
  })

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

  describe('installing from a folder', () => {
    it('stops to ask when the extension wants something', async () => {
      const bridge = fakeBridge();
      const catalogue = useExtensionCatalogue({ bridge });

      await catalogue.chooseFolder();

      expect(catalogue.step.value).toBe(INSTALL_STEP.asking);
      expect(catalogue.candidate.value.manifest.name).toBe('standup');
      expect(bridge.installLocal).not.toHaveBeenCalled();
    });

    // Interposing a permission screen with nothing on it trains people to click
    // past the screen that does have something on it.
    it('installs straight away when it asks for nothing', async () => {
      const bridge = fakeBridge({
        chooseFolder: vi.fn(async () => found({ manifest: manifest({ mcp: { workspace: [], supervisor: [] } }) })),
      });
      const catalogue = useExtensionCatalogue({ bridge });

      await catalogue.chooseFolder();

      expect(bridge.installLocal).toHaveBeenCalledWith('/tmp/standup', { grant: null, config: null });
      expect(catalogue.step.value).toBe(INSTALL_STEP.idle);
      expect(catalogue.notice.value).toBe('Standup installed.');
    });

    // The install screen is the only place settings can be entered, so skipping
    // it because no permission was asked for leaves an extension that needs a
    // token running with a blank one and nowhere to fix it.
    it('still asks when the only thing wanted is a setting', async () => {
      const bridge = fakeBridge({
        chooseFolder: vi.fn(async () =>
          found({
            manifest: manifest({
              mcp: { workspace: [], supervisor: [] },
              config: [{ key: 'token', type: 'secret', label: 'API token' }],
            }),
          }),
        ),
      });
      const catalogue = useExtensionCatalogue({ bridge });

      await catalogue.chooseFolder();

      expect(catalogue.step.value).toBe(INSTALL_STEP.asking);
      expect(bridge.installLocal).not.toHaveBeenCalled();
    });

    it('treats a manifest with no mcp block and no settings as asking for nothing', async () => {
      const bridge = fakeBridge({
        chooseFolder: vi.fn(async () => found({ manifest: manifest({ mcp: undefined, config: undefined }) })),
      });
      const catalogue = useExtensionCatalogue({ bridge });

      await catalogue.chooseFolder();

      expect(bridge.installLocal).toHaveBeenCalled();
    });

    // Cancelling a file dialog is not an error and must not read as one.
    it('says nothing at all when the dialog is dismissed', async () => {
      const bridge = fakeBridge({ chooseFolder: vi.fn(async () => ({ ok: false, cancelled: true })) });
      const catalogue = useExtensionCatalogue({ bridge });

      await catalogue.chooseFolder();

      expect(catalogue.error.value).toBe('');
      expect(catalogue.step.value).toBe(INSTALL_STEP.idle);
    });

    it('reports a folder that holds no extension', async () => {
      const bridge = fakeBridge({
        chooseFolder: vi.fn(async () => ({ ok: false, reason: 'There is no agentrq-extension.json in that folder.' })),
      });
      const catalogue = useExtensionCatalogue({ bridge });

      await catalogue.chooseFolder();

      expect(catalogue.error.value).toContain('no agentrq-extension.json');
    });

    it('reports a folder that answered with nothing at all', async () => {
      const catalogue = useExtensionCatalogue({ bridge: fakeBridge({ chooseFolder: vi.fn(async () => undefined) }) });

      await catalogue.chooseFolder();

      expect(catalogue.error.value).toBe('That folder could not be read.');
    });

    it('refuses one that cannot run here, before asking about permissions', async () => {
      const bridge = fakeBridge({
        chooseFolder: vi.fn(async () => found({ compatible: false, reasons: ['Needs AgentRQ >=9; this is 0.5.'] })),
      });
      const catalogue = useExtensionCatalogue({ bridge });

      await catalogue.chooseFolder();

      expect(catalogue.error.value).toContain('Needs AgentRQ >=9');
      expect(catalogue.step.value).toBe(INSTALL_STEP.idle);
    });

    it('refuses one whose shortcut is taken, naming who has it', async () => {
      const bridge = fakeBridge({
        chooseFolder: vi.fn(async () => found({ shortcutProblems: ['"x s" is already used by linear.'] })),
      });
      const catalogue = useExtensionCatalogue({ bridge });

      await catalogue.chooseFolder();

      expect(catalogue.error.value).toContain('linear');
    });

    it('installs with the grant the screen answered, then reloads the list', async () => {
      const bridge = fakeBridge();
      const catalogue = useExtensionCatalogue({ bridge });
      await catalogue.chooseFolder();
      const grant = { scope: 'workspace', workspaces: ['ws1'] };

      await catalogue.install({ grant, config: { since: 8 } });

      expect(bridge.installLocal).toHaveBeenCalledWith('/tmp/standup', { grant, config: { since: 8 } });
      expect(bridge.state).toHaveBeenCalled();
      expect(catalogue.step.value).toBe(INSTALL_STEP.idle);
    });

    // Installed and not running is a real outcome, and it is not an error: the
    // row exists, and this is the sentence explaining why it sits idle.
    it('says an extension installed but did not start', async () => {
      const bridge = fakeBridge({
        installLocal: vi.fn(async () => ({ ok: true, loadFailure: 'Cannot find module "linear-sdk"' })),
      });
      const catalogue = useExtensionCatalogue({ bridge });
      await catalogue.chooseFolder();

      await catalogue.install({});

      expect(catalogue.error.value).toBe('');
      expect(catalogue.notice.value).toContain('did not start');
      expect(catalogue.notice.value).toContain('linear-sdk');
    });

    it('reports an install the main process refused', async () => {
      const bridge = fakeBridge({ installLocal: vi.fn(async () => ({ ok: false, reason: 'disk full' })) });
      const catalogue = useExtensionCatalogue({ bridge });
      await catalogue.chooseFolder();

      await catalogue.install({});

      expect(catalogue.error.value).toBe('disk full');
    });

    it('reports an install that threw', async () => {
      const bridge = fakeBridge({ installLocal: vi.fn(async () => { throw new Error('EPIPE'); }) });
      const catalogue = useExtensionCatalogue({ bridge });
      await catalogue.chooseFolder();

      await catalogue.install({});

      expect(catalogue.error.value).toBe('EPIPE');
      expect(catalogue.step.value).toBe(INSTALL_STEP.idle);
    });

    it('has something to say about an install that threw with no message', async () => {
      const bridge = fakeBridge({ installLocal: vi.fn(async () => { throw new Error(''); }) });
      const catalogue = useExtensionCatalogue({ bridge });
      await catalogue.chooseFolder();

      await catalogue.install({});

      expect(catalogue.error.value).toBe('That extension could not be installed.');
    });

    it('reports an install that answered with nothing', async () => {
      const bridge = fakeBridge({ installLocal: vi.fn(async () => undefined) });
      const catalogue = useExtensionCatalogue({ bridge });
      await catalogue.chooseFolder();

      await catalogue.install({});

      expect(catalogue.error.value).toBe('That extension could not be installed.');
    });

    it('installs nothing when the grant question is abandoned', async () => {
      const bridge = fakeBridge();
      const catalogue = useExtensionCatalogue({ bridge });
      await catalogue.chooseFolder();

      catalogue.cancelInstall();
      await catalogue.install({});

      expect(bridge.installLocal).not.toHaveBeenCalled();
      expect(catalogue.step.value).toBe(INSTALL_STEP.idle);
    });

    it('does nothing at all with no bridge', async () => {
      const catalogue = useExtensionCatalogue({ bridge: undefined });

      await catalogue.chooseFolder();
      await catalogue.install({});
      await catalogue.uninstall('standup');
      await catalogue.setEnabled('standup', false);

      expect(catalogue.error.value).toBe('');
    });
  });

  /**
   * The supervisor reaches every workspace on the account, so acquiring a
   * credential for it is a decision a person makes — not something that
   * follows from installing an extension.
   */
  describe('account-wide authorisation', () => {
    const installedWith = (grant) => ({
      state: vi.fn(async () => ({ index: { entries: [] }, installed: [{ name: 'digest', enabled: true, loaded: true, grant }] })),
    });

    it('asks only when something installed actually wants it', async () => {
      const wanting = useExtensionCatalogue({ bridge: fakeBridge(installedWith({ scope: 'supervisor', workspaces: [] })) });
      await wanting.load();
      expect(wanting.needsAuthorization.value).toBe(true);

      const not = useExtensionCatalogue({ bridge: fakeBridge(installedWith({ scope: 'workspace', workspaces: ['ws1'] })) });
      await not.load();
      expect(not.needsAuthorization.value).toBe(false);
    });

    it('stops asking once it has been given', async () => {
      const bridge = fakeBridge({
        ...installedWith({ scope: 'supervisor', workspaces: [] }),
        supervisor: vi.fn(async () => ({ authorized: true })),
      });
      const catalogue = useExtensionCatalogue({ bridge });

      await catalogue.load();

      expect(catalogue.authorized.value).toBe(true);
      expect(catalogue.needsAuthorization.value).toBe(false);
    });

    it('opens the authorisation when asked, and says what happened', async () => {
      const bridge = fakeBridge();
      const catalogue = useExtensionCatalogue({ bridge });

      await catalogue.authorize();

      expect(bridge.authorize).toHaveBeenCalled();
      expect(catalogue.notice.value).toContain('account-wide');
    });

    it('reports a refusal in the words the flow gave', async () => {
      const bridge = fakeBridge({ authorize: vi.fn(async () => ({ ok: false, reason: 'You declined to authorise AgentRQ.' })) });
      const catalogue = useExtensionCatalogue({ bridge });

      await catalogue.authorize();

      expect(catalogue.error.value).toBe('You declined to authorise AgentRQ.');
    });

    it('gives it back on request', async () => {
      const bridge = fakeBridge();
      const catalogue = useExtensionCatalogue({ bridge });

      await catalogue.deauthorize();

      expect(bridge.deauthorize).toHaveBeenCalled();
    });

    it('copes with a build whose bridge has no supervisor question', async () => {
      // An older desktop build: the catalogue still loads rather than throwing.
      const bridge = fakeBridge();
      delete bridge.supervisor;
      const catalogue = useExtensionCatalogue({ bridge });

      await catalogue.load();

      expect(catalogue.authorized.value).toBe(false);
      expect(catalogue.error.value).toBe('');
    });
  });

  describe('managing what is installed', () => {
    it('removes one and says so', async () => {
      const bridge = fakeBridge();
      const catalogue = useExtensionCatalogue({ bridge });

      await catalogue.uninstall('standup');

      expect(bridge.uninstall).toHaveBeenCalledWith('standup');
      expect(catalogue.notice.value).toBe('standup removed.');
      expect(bridge.state).toHaveBeenCalled();
    });

    // An uninstall that could not take a cron task down has left something
    // running, and the user is the only one who can do anything about it.
    it('says out loud what an uninstall could not take away', async () => {
      const bridge = fakeBridge({
        uninstall: vi.fn(async () => ({ ok: true, leftBehind: ['workspace not found'] })),
      });
      const catalogue = useExtensionCatalogue({ bridge });

      await catalogue.uninstall('standup');

      expect(catalogue.error.value).toContain('workspace not found');
    });

    it('turns one off and back on', async () => {
      const bridge = fakeBridge();
      const catalogue = useExtensionCatalogue({ bridge });

      await catalogue.setEnabled('standup', false);
      expect(catalogue.notice.value).toBe('standup disabled.');

      await catalogue.setEnabled('standup', true);
      expect(catalogue.notice.value).toBe('standup enabled.');
    });

    it('reports a refusal, and a throw, in the same place', async () => {
      const refusing = useExtensionCatalogue({
        bridge: fakeBridge({ uninstall: vi.fn(async () => ({ ok: false, reason: 'EBUSY' })) }),
      });
      await refusing.uninstall('standup');
      expect(refusing.error.value).toBe('EBUSY');

      const throwing = useExtensionCatalogue({
        bridge: fakeBridge({ setEnabled: vi.fn(async () => { throw new Error('gone'); }) }),
      });
      await throwing.setEnabled('standup', true);
      expect(throwing.error.value).toBe('gone');
    });

    it('has something to say about a failure that carried no message', async () => {
      const catalogue = useExtensionCatalogue({
        bridge: fakeBridge({ uninstall: vi.fn(async () => ({ ok: false })) }),
      });

      await catalogue.uninstall('standup');

      expect(catalogue.error.value).toBe('That did not work.');
    });

    it('has something to say about a throw that carried no message', async () => {
      const catalogue = useExtensionCatalogue({
        bridge: fakeBridge({ uninstall: vi.fn(async () => { throw new Error(''); }) }),
      });

      await catalogue.uninstall('standup');

      expect(catalogue.error.value).toBe('That did not work.');
    });
  });
});
