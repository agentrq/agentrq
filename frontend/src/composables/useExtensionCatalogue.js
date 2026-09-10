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
  return 'available';
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
  return '';
}

/**
 * The rows for the screen, grouped and ordered.
 *
 * `installedNames` comes from what is actually installed rather than from the
 * catalogue, so an extension that has been removed from GitHub still appears
 * while it is on this machine — the row someone needs in order to uninstall it
 * is exactly the one a catalogue-driven list would drop.
 */
export function toRows(index, { installedNames = [] } = {}) {
  const installed = new Set(installedNames);
  const rows = (index?.entries ?? []).map((entry) => {
    // `compatible` and `reasons` arrive already decided from the main process;
    // an entry that never parsed has no manifest to judge, so it is simply not.
    const row = {
      ...entry,
      compatible: entry.ok === true && entry.compatible !== false,
      reasons: entry.reasons ?? [],
      installed: installed.has(entry.manifest?.name ?? ''),
    };
    return { ...row, group: groupFor(row, { installed: row.installed }), blocked: blockedReason(row) };
  });

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
  const installedNames = ref([]);
  const loading = ref(false);
  const error = ref('');

  const sections = computed(() => toRows(index.value, { installedNames: installedNames.value }));

  const summary = computed(() => summarise(index.value, sections.value));

  /** Read what is already known. Never searches — see the refresh below. */
  async function load() {
    if (!bridge) return;
    loading.value = true;
    error.value = '';
    try {
      const state = await bridge.state();
      index.value = state?.index ?? index.value;
      installedNames.value = state?.installed ?? [];
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

  return { index, sections, summary, loading, error, load, refresh, available: Boolean(bridge) };
}
