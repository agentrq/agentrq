// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { ref, toValue, watch } from 'vue';
import * as api from '../api';
import { useSlashCommands } from './useSlashCommands';

/**
 * Presets: messages kept in a workspace's settings, or in the account's for
 * every workspace, which the composer puts in place of `#name`. The `#` menu is the `/` menu with two differences:
 * it opens on the word being typed at the end of the message, not only at its
 * start, and choosing one puts the preset's body in place of `#name` rather
 * than the name.
 */

/**
 * `#` and a name being typed at the end of the text, after its start or a
 * space: `fix #12` is asking, `issue#12` and a `## heading` are not.
 */
const PRESET_TOKEN = /(^|\s)#([a-z0-9-]*)$/i;

/** The preset name being typed, or null when the text is not asking for one. */
export function presetQuery(text) {
  if (typeof text !== 'string') return null;
  const m = PRESET_TOKEN.exec(text);
  return m ? m[2] : null;
}

/** The text after choosing a preset: the `#name` being typed becomes its body. */
export function applyPreset(preset, text) {
  const current = typeof text === 'string' ? text : '';
  const m = PRESET_TOKEN.exec(current);
  if (!m) return current + preset.body;
  return current.slice(0, m.index + m[1].length) + preset.body;
}

/** The first line of a body, which is what the menu shows beside the name. */
function firstLine(body) {
  return body.split('\n').find((line) => line.trim() !== '')?.trim() ?? '';
}

// Where a preset is kept, which the menu shows as an icon beside it.
export const PRESET_WORKSPACE = 'workspace';
export const PRESET_ACCOUNT = 'account';

/** Presets as menu entries, each with where it is kept. */
export function presetCommands(presets, scope = PRESET_WORKSPACE) {
  if (!Array.isArray(presets)) return [];
  return presets
    .filter((p) => p && typeof p.name === 'string' && p.name !== '' && typeof p.body === 'string')
    .map((p) => ({ name: p.name, body: p.body, description: firstLine(p.body), scope }));
}

/**
 * A workspace's presets, then the account's, as one menu. Where both have a
 * name, the workspace's wins: it is the nearer of the two.
 */
export function mergePresetCommands(workspacePresets, accountPresets) {
  const own = presetCommands(workspacePresets);
  const names = new Set(own.map((c) => c.name));
  return [...own, ...presetCommands(accountPresets, PRESET_ACCOUNT).filter((c) => !names.has(c.name))];
}

/**
 * The workspace's presets and the account's as menu entries, loaded again
 * whenever the workspace changes. Only the newest load may write, so a quick
 * switch cannot leave the previous workspace's presets behind; a list that
 * fails to load is left out.
 */
export function usePresetCommands(workspaceId, {
  load = (id) => api.listWorkspacePresets(id),
  loadAccount = () => api.listAccountPresets(),
} = {}) {
  const commands = ref([]);
  let seq = 0;
  // A load that throws before it returns a promise is a failed one too.
  const presetsOf = (request) => Promise.resolve().then(request).then((res) => res?.presets, () => []);
  watch(
    () => toValue(workspaceId),
    async (id) => {
      const mine = ++seq;
      commands.value = [];
      if (!id) return;
      const [own, account] = await Promise.all([presetsOf(() => load(id)), presetsOf(loadAccount)]);
      if (mine === seq) commands.value = mergePresetCommands(own, account);
    },
    { immediate: true }
  );
  return commands;
}

/**
 * The composer's `#` menu, counting each preset put in.
 *
 * @param {object} options
 * @param {import('vue').Ref<string>} options.text  the composer's contents
 * @param {import('vue').Ref<string>|(() => string)} options.workspaceId  '' for none
 */
export function usePresetMenu({
  text,
  workspaceId,
  load,
  loadAccount,
  record = (action, id) => api.recordTelemetry(action, id),
}) {
  return useSlashCommands({
    text,
    commands: usePresetCommands(workspaceId, { ...(load && { load }), ...(loadAccount && { loadAccount }) }),
    query: presetQuery,
    apply: applyPreset,
    onAccept: () => record(api.TELEMETRY_UI_PRESET_INSERT, toValue(workspaceId)),
  });
}

/**
 * The settings tab's list: load, add, edit and remove, with the server's sentence
 * when it refuses. Each change applies at once; there is no Save.
 *
 * @param {import('vue').Ref<string>|(() => string)} workspaceId  '' for the account's own
 */
export function useWorkspacePresets(workspaceId, {
  list = (id) => (id ? api.listWorkspacePresets(id) : api.listAccountPresets()),
  create = (id, name, body) => (id ? api.createWorkspacePreset(id, name, body) : api.createAccountPreset(name, body)),
  update = (id, name, newName, body) =>
    (id ? api.updateWorkspacePreset(id, name, newName, body) : api.updateAccountPreset(name, newName, body)),
  remove = (id, name) => (id ? api.deleteWorkspacePreset(id, name) : api.deleteAccountPreset(name)),
} = {}) {
  const presets = ref([]);
  const loading = ref(false);
  const error = ref('');
  const saving = ref(false);

  async function load() {
    const id = toValue(workspaceId);
    loading.value = true;
    try {
      const res = await list(id);
      presets.value = res?.presets ?? [];
      error.value = '';
    } catch (e) {
      error.value = e?.message || 'Failed to fetch presets';
    } finally {
      loading.value = false;
    }
  }

  /** Saves a preset; true when it was kept, so the form can clear. */
  function add(name, body) {
    return save(() => create(toValue(workspaceId), name.trim(), body), presets.value);
  }

  /** Gives the preset called oldName a new name and body; true when kept. */
  function edit(oldName, name, body) {
    return save(
      () => update(toValue(workspaceId), oldName, name.trim(), body),
      presets.value.filter((p) => p.name !== oldName)
    );
  }

  async function save(send, others) {
    saving.value = true;
    try {
      const res = await send();
      presets.value = [...others, res.preset]
        // By code point, as the server orders them.
        .sort((a, b) => (a.name < b.name ? -1 : 1));
      error.value = '';
      return true;
    } catch (e) {
      error.value = e?.message || 'Failed to save preset';
      return false;
    } finally {
      saving.value = false;
    }
  }

  async function removePreset(name) {
    try {
      await remove(toValue(workspaceId), name);
      presets.value = presets.value.filter((p) => p.name !== name);
      error.value = '';
    } catch (e) {
      error.value = e?.message || 'Failed to remove preset';
    }
  }

  return { presets, loading, error, saving, load, add, edit, remove: removePreset };
}
