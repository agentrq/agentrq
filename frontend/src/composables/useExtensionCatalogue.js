// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { computed, ref } from 'vue';

/**
 * What the Extensions screen shows, and in what order.
 *
 * The catalogue comes from the desktop bridge, already searched, validated *and
 * judged compatible* in the main process — which is where the manifest rules,
 * the running version and the live tool list all are. This is only concerned
 * with turning that into rows a person can act on, so it is a plain composable
 * that never learns what SPDX or a semver range is.
 *
 * ## Nothing usable is hidden
 *
 * A repository carrying the topic whose manifest does not parse is **listed as
 * broken with its reason**, and one that needs a newer AgentRQ is **listed but
 * not installable**, with the version stated. Both are deliberate. A silently
 * missing extension is undebuggable for the author who published it, and to
 * anyone following a link to it the catalogue simply looks broken — which is a
 * worse failure than an honest row explaining itself.
 */

/**
 * The order rows appear in.
 *
 * Installed first, because those are the ones a person came to manage. Then what
 * they could install, most-starred first — stars being the only signal an
 * uncurated catalogue has. Then what they cannot use, which is reference rather
 * than choice, so it sits at the bottom rather than interrupting the list.
 */
export const GROUPS = ['installed', 'available', 'unavailable'];

/** The grant that reaches the whole account, which is the one needing OAuth. */
const SCOPE_SUPERVISOR = 'supervisor';

/**
 * Which group a row belongs in.
 *
 * Installed wins over everything: an extension that stopped being compatible
 * after an app downgrade is still installed, and hiding it among the
 * unavailable is how somebody fails to find the thing they need to remove.
 */
export function groupFor(entry, { installed = false } = {}) {
  if (installed) return 'installed';
  if (!entry.ok || !entry.compatible) return 'unavailable';
  // Available means installable. An entry with no release to fetch belongs with
  // the rest of what cannot be used, next to the sentence saying why.
  return releaseProblem(entry) ? 'unavailable' : 'available';
}

/**
 * Why a row cannot be used, or '' when it can.
 *
 * A broken manifest reports the parse failure, which is the author's problem to
 * fix; an incompatible one reports every reason rather than the first, because
 * somebody fixing a manifest wants the whole list.
 */
export function blockedReason(entry) {
  if (!entry.ok) return entry.reason ?? 'This manifest could not be read.';
  if (!entry.compatible) return (entry.reasons ?? []).join(' ');
  return releaseProblem(entry);
}

/** A digest of nothing: what a manifest carries before there is a release. */
const PLACEHOLDER_DIGEST = /^0+$/;
const SHA256 = /^[0-9a-f]{64}$/i;

/**
 * Why this entry has nothing to install, or '' when it has.
 *
 * An extension is installed from the release asset its manifest names, pinned
 * by digest — the tag is mutable, so without the digest "install v1.2.0" is a
 * promise nobody is keeping. An entry that names no asset, or names one with a
 * placeholder digest, therefore cannot be installed at all.
 *
 * Said rather than discovered by pressing the button. The catalogue is
 * uncurated, and an entry whose author has not cut a release yet looks exactly
 * like one who has until you try — which leaves somebody hunting for a control
 * that was never going to work.
 */
export function releaseProblem(entry) {
  const artifact = entry?.manifest?.artifact;
  if (!artifact?.release || !artifact?.asset) {
    return 'This extension has not published a release yet, so there is nothing to install.';
  }

  const digest = String(artifact.sha256 ?? '');
  if (!SHA256.test(digest) || PLACEHOLDER_DIGEST.test(digest)) {
    return 'This extension names a release but no checksum for it, so it cannot be verified.';
  }
  return '';
}

/**
 * One installed extension, as a row.
 *
 * Built from the installation rather than from the catalogue, because most
 * installed extensions are not in the catalogue at all: anything installed from
 * a local folder was never published, and anything published can be taken down
 * afterwards. A list driven by the catalogue would be missing exactly the row
 * somebody needs in order to remove something.
 */
export function installedRow(installation) {
  return {
    fullName: `installed:${installation.name}`,
    name: installation.name,
    owner: installation.source || 'Installed here',
    stars: null,
    url: '',
    description: installation.description ?? '',
    manifest: { name: installation.name, displayName: installation.displayName, license: installation.license },
    ok: true,
    compatible: true,
    reasons: [],
    installed: true,
    installation,
    group: 'installed',
    blocked: '',
    // Enabled and running are different states, and the row has to be able to
    // say which: an enabled extension that threw on load does nothing, and
    // "enabled" alone would leave nothing to explain why.
    status: statusOf(installation),
  };
}

/**
 * What a row says about itself in one phrase.
 *
 * Ordered by what a person needs to know first. "Not running" beats the failure
 * count, because the count is the explanation and the state is the fact.
 */
export function statusOf(installation) {
  if (installation.enabled === false) return 'Disabled';
  if (!installation.loaded) {
    return installation.failures > 0
      ? `Not running — failed ${installation.failures} ${installation.failures === 1 ? 'time' : 'times'}`
      : 'Not running';
  }
  const contributes = Object.entries(installation.contributes ?? {})
    .filter(([, count]) => count > 0)
    .map(([registry, count]) => `${count} ${LABELS[registry] ?? registry}${count === 1 ? '' : 's'}`);

  // An extension that loaded and registered nothing is a real state and worth
  // saying out loud — it usually means a setting it needed was blank.
  return contributes.length > 0 ? contributes.join(', ') : 'Running, contributing nothing';
}

const LABELS = { ui: 'view', shortcuts: 'shortcut', schedules: 'schedule', renderers: 'renderer' };

/**
 * The rows for the screen, grouped and ordered.
 *
 * Installed extensions lead, and they come from what is actually installed
 * rather than from the catalogue — see `installedRow`. A catalogue entry for
 * something already installed is folded into that row rather than repeated.
 */
export function toRows(index, { installed = [] } = {}) {
  const byName = new Map(installed.map((installation) => [installation.name, installation]));

  const catalogue = (index?.entries ?? [])
    .filter((entry) => !byName.has(entry.manifest?.name ?? ''))
    .map((entry) => {
      // `compatible` and `reasons` arrive already decided from the main process;
      // an entry that never parsed has no manifest to judge, so it is simply not.
      const row = {
        ...entry,
        compatible: entry.ok === true && entry.compatible !== false,
        reasons: entry.reasons ?? [],
        installed: false,
      };
      return { ...row, group: groupFor(row), blocked: blockedReason(row) };
    });

  const rows = [...installed.map(installedRow), ...catalogue];

  return GROUPS.map((group) => ({
    group,
    rows: rows.filter((row) => row.group === group).sort(byStarsThenName),
  })).filter((section) => section.rows.length > 0);
}

/**
 * Most-starred first, then alphabetical.
 *
 * Stars are the only signal an uncurated catalogue carries, and the name breaks
 * the tie so the order is stable rather than dependent on whatever order the
 * search happened to return.
 */
function byStarsThenName(a, b) {
  return (b.stars ?? 0) - (a.stars ?? 0) || String(a.fullName).localeCompare(String(b.fullName));
}

/**
 * How much this extension has to ask a person before it can be installed.
 *
 * Settings count alongside permissions. They are entered on that same screen and
 * on no other, so an extension declaring a config field but no permission would
 * otherwise install straight past the only place its values could ever be given
 * — and then run with every setting blank.
 */
export function asksFor(manifest) {
  return (
    (manifest?.mcp?.workspace ?? []).length +
    (manifest?.mcp?.supervisor ?? []).length +
    (manifest?.config ?? []).length
  );
}

/** The install flow's states. A grant is a question, so there is a step for it. */
export const INSTALL_STEP = { idle: 'idle', asking: 'asking', working: 'working' };

/** A one-line summary of the catalogue's state, for the screen's header. */
export function summarise(index, sections) {
  const total = sections.reduce((n, section) => n + section.rows.length, 0);
  if (total === 0) return 'No extensions found yet.';

  const unavailable = sections.find((s) => s.group === 'unavailable')?.rows.length ?? 0;
  const parts = [`${total} extension${total === 1 ? '' : 's'}`];
  if (unavailable > 0) parts.push(`${unavailable} unavailable`);
  // Said out loud, because a truncated catalogue is indistinguishable from a
  // complete one unless it says so.
  if (index?.truncated) parts.push('list is incomplete');
  return parts.join(' · ');
}

/**
 * The screen's state, over the desktop bridge.
 *
 * With no bridge — the web build, or the frontend's own test run — everything
 * stays inert and empty rather than throwing, which is what lets the section be
 * *absent* in the browser rather than broken in it.
 */
export function useExtensionCatalogue({ bridge = globalThis.window?.agentrq?.extensions } = {}) {
  const index = ref({ entries: [], fetchedAt: null, truncated: false });
  const installed = ref([]);
  const loading = ref(false);
  const error = ref('');
  /** Whether account-wide tools can be used at all yet. */
  const authorized = ref(false);

  /** Whether any installed extension actually wants them. */
  const needsAuthorization = computed(() =>
    !authorized.value && installed.value.some((entry) => entry.grant?.scope === SCOPE_SUPERVISOR),
  );

  /** What `chooseFolder` found, while the grant question is on screen. */
  const candidate = ref(null);
  const step = ref(INSTALL_STEP.idle);
  const notice = ref('');

  const sections = computed(() => toRows(index.value, { installed: installed.value }));

  const summary = computed(() => summarise(index.value, sections.value));

  /** Read what is already known. Never searches — see the refresh below. */
  async function load() {
    if (!bridge) return;
    loading.value = true;
    error.value = '';
    try {
      const state = await bridge.state();
      index.value = state?.index ?? index.value;
      installed.value = state?.installed ?? [];
      authorized.value = (await bridge.supervisor?.())?.authorized ?? false;
    } catch (err) {
      error.value = err?.message || 'Could not read the extension catalogue.';
    } finally {
      loading.value = false;
    }
  }

  /**
   * Go and look again — a deliberate act, never something a page load triggers.
   *
   * GitHub answers ten searches a minute unauthenticated, and a catalogue that
   * refreshed itself on every visit would spend that budget on people who opened
   * the screen and closed it again.
   */
  async function refresh() {
    if (!bridge) return;
    loading.value = true;
    error.value = '';
    try {
      const result = await bridge.refresh();
      if (result?.index) index.value = result.index;
      if (result && result.ok === false) error.value = result.reason ?? 'Could not refresh.';
    } catch (err) {
      error.value = err?.message || 'Could not refresh the extension catalogue.';
    } finally {
      loading.value = false;
    }
  }

  /**
   * Ask for a folder, and stop to ask about permissions if there are any.
   *
   * An extension that asks for nothing goes straight to installing. Interposing
   * a permission screen with nothing on it would train people to click past the
   * screen that does have something on it.
   */
  async function chooseFolder() {
    if (!bridge) return;
    error.value = '';
    notice.value = '';

    const found = await bridge.chooseFolder();
    // Cancelling a file dialog is not an error and must not read as one.
    if (found?.cancelled) return;
    if (!found?.ok) {
      error.value = found?.reason || 'That folder could not be read.';
      return;
    }
    if (!found.compatible) {
      error.value = found.reasons.join(' ');
      return;
    }
    if (found.shortcutProblems?.length > 0) {
      error.value = found.shortcutProblems.join(' ');
      return;
    }

    candidate.value = { ...found, kind: 'folder' };
    if (asksFor(found.manifest) === 0) {
      await install({ grant: null, config: null });
      return;
    }
    step.value = INSTALL_STEP.asking;
  }

  /**
   * Install a catalogue row, asking about permissions if it asks for any.
   *
   * Only the repository name is sent. The main process resolves it against its
   * own index, so the release that gets downloaded and the digest it is checked
   * against are decided by what this app discovered, not by what this screen is
   * holding.
   */
  async function beginInstall(row) {
    if (!bridge || !row || row.installed) return;
    error.value = '';
    notice.value = '';

    // Should not be reachable — a blocked row offers no button — but a row that
    // says why it cannot be used must not install anyway if one is ever added.
    if (row.blocked) {
      error.value = row.blocked;
      return;
    }

    // Always asked, even when the extension wants no permission at all — which
    // is the one place this differs from the folder install, deliberately.
    //
    // The rule there is that a permission screen with nothing on it teaches
    // people to click past the screen that does have something on it, and that
    // rule is right: you picked that folder, so you already have the code.
    // Installing from the catalogue is the other thing. It downloads a
    // stranger's code and runs it with full access to this machine, and the
    // screen is not empty — it names the author, the repository, the version
    // and the licence, and carries the sentence about machine access. That is
    // the whole of the decision, and it is not one to make by single click.
    candidate.value = { ...row, kind: 'catalogue' };
    step.value = INSTALL_STEP.asking;
  }

  /** Install what was found, with whatever the grant screen answered. */
  async function install({ grant = null, config = null } = {}) {
    if (!bridge || !candidate.value) return;
    step.value = INSTALL_STEP.working;
    error.value = '';

    try {
      const result =
        candidate.value.kind === 'catalogue'
          ? await bridge.installFromCatalogue(candidate.value.fullName, { grant, config })
          : await bridge.installLocal(candidate.value.path, { grant, config });
      if (!result?.ok) {
        error.value = result?.reason || 'That extension could not be installed.';
        return;
      }
      // Installed but not running is a real outcome, and it is not an error —
      // the row exists, and this is the sentence that explains why it is idle.
      notice.value = result.loadFailure
        ? `${candidate.value.manifest.displayName} installed, but did not start: ${result.loadFailure}`
        : `${candidate.value.manifest.displayName} installed.`;
      await load();
    } catch (err) {
      error.value = err?.message || 'That extension could not be installed.';
    } finally {
      step.value = INSTALL_STEP.idle;
      candidate.value = null;
    }
  }

  /** Abandon the grant question without installing. */
  function cancelInstall() {
    candidate.value = null;
    step.value = INSTALL_STEP.idle;
  }

  /**
   * Do something to an installed extension, then say what happened.
   *
   * The list is reloaded **before** the message is set, not after. `load` clears
   * the error as its first act — which is right when a person presses refresh,
   * and wrong here: a reload afterwards would wipe the sentence explaining what
   * had just gone wrong, leaving a failed uninstall looking like a successful
   * one.
   */
  async function act(promise, describe) {
    if (!bridge) return;
    error.value = '';
    notice.value = '';

    let outcome;
    try {
      const result = await promise;
      if (result?.ok === false) outcome = { error: result.reason || 'That did not work.' };
      // Said rather than left silent: an uninstall that could not take a cron
      // task down has left something running, and the user is the only one who
      // can do anything about it.
      else if (result?.leftBehind?.length > 0) {
        outcome = { error: `Removed, but this was left behind: ${result.leftBehind.join(' ')}` };
      } else outcome = { notice: describe };
      await load();
    } catch (err) {
      outcome = { error: err?.message || 'That did not work.' };
    }

    if (outcome.error) error.value = outcome.error;
    else notice.value = outcome.notice;
  }

  const uninstall = (name) => act(bridge?.uninstall(name), `${name} removed.`);
  const setEnabled = (name, enabled) =>
    act(bridge?.setEnabled(name, enabled), `${name} ${enabled ? 'enabled' : 'disabled'}.`);

  /**
   * Ask the user to authorise account-wide access.
   *
   * A window opens on the server's own authorisation page — this is a question
   * put to a person, and nothing about it happens quietly.
   */
  const authorize = () =>
    act(bridge?.authorize(), 'AgentRQ can now use account-wide tools.');

  const deauthorize = () =>
    act(bridge?.deauthorize(), 'Account-wide access given back.');

  return {
    index,
    installed,
    authorized,
    needsAuthorization,
    authorize,
    deauthorize,
    sections,
    summary,
    loading,
    error,
    notice,
    candidate,
    step,
    load,
    refresh,
    chooseFolder,
    beginInstall,
    install,
    cancelInstall,
    uninstall,
    setEnabled,
    available: Boolean(bridge),
  };
}
