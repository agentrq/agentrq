// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { ref, toValue, watch } from 'vue';
import * as api from '../api';
import { useSlashCommands } from './useSlashCommands';

/**
 * A workspace's presets: messages kept in its settings, which the composer
 * puts in place of `#name`. The `#` menu is the `/` menu with two differences:
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

/** A workspace's presets as menu entries. */
export function presetCommands(presets) {
  if (!Array.isArray(presets)) return [];
  return presets
    .filter((p) => p && typeof p.name === 'string' && p.name !== '' && typeof p.body === 'string')
    .map((p) => ({ name: p.name, body: p.body, description: firstLine(p.body), hint: 'preset' }));
}

/**
 * The workspace's presets as menu entries, loaded again whenever the workspace
 * changes. Only the newest load may write, so a quick switch cannot leave the
 * previous workspace's presets behind; a failed load leaves the menu empty.
 */
export function usePresetCommands(workspaceId, { load = (id) => api.listWorkspacePresets(id) } = {}) {
  const commands = ref([]);
  let seq = 0;
  watch(
    () => toValue(workspaceId),
    async (id) => {
      const mine = ++seq;
      commands.value = [];
      if (!id) return;
      try {
        const res = await load(id);
        if (mine === seq) commands.value = presetCommands(res?.presets);
      } catch {
        // Already empty.
      }
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
  record = (action, id) => api.recordTelemetry(action, id),
}) {
  return useSlashCommands({
    text,
    commands: usePresetCommands(workspaceId, load ? { load } : undefined),
    query: presetQuery,
    apply: applyPreset,
    onAccept: () => record(api.TELEMETRY_UI_PRESET_INSERT, toValue(workspaceId)),
  });
}

/**
 * The settings tab's list: load, add and remove, with the server's sentence
 * when it refuses. Each change applies at once; there is no Save.
 *
 * @param {import('vue').Ref<string>|(() => string)} workspaceId
 */
export function useWorkspacePresets(workspaceId, {
  list = (id) => api.listWorkspacePresets(id),
  create = (id, name, body) => api.createWorkspacePreset(id, name, body),
  remove = (id, name) => api.deleteWorkspacePreset(id, name),
} = {}) {
  const presets = ref([]);
  const loading = ref(false);
  const error = ref('');
  const saving = ref(false);

  async function load() {
    const id = toValue(workspaceId);
    if (!id) return;
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
  async function add(name, body) {
    saving.value = true;
    try {
      const res = await create(toValue(workspaceId), name.trim(), body);
      presets.value = [...presets.value, res.preset]
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

  return { presets, loading, error, saving, load, add, remove: removePreset };
}
