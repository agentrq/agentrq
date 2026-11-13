<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- Sharing your workspaces with teams (§4 of the flows). One control, two
     places: Workspace settings › Sharing lists your teams for one workspace
     (`workspace-id`), and Team settings › Your workspaces lists your
     workspaces for one team (`team-id`). Turning one on prefills "show in
     team as" with a team group of the same name; turning one off asks first.
     A fork follows its parent, so it has no switch of its own. -->
<template>
  <div data-test="workspace-sharing">
    <p v-if="rows.length === 0 && !loading" data-test="sharing-empty" class="text-[11px] text-gray-500 dark:text-zinc-400 font-medium">
      <template v-if="workspaceId">You are not in a team yet. <router-link to="/team/new" class="font-semibold underline text-gray-900 dark:text-white">Create one</router-link> or accept an invitation to share this workspace.</template>
      <template v-else>You have no workspaces to share.</template>
    </p>

    <ul v-else class="divide-y divide-gray-100 dark:divide-zinc-800">
      <li v-for="row in rows" :key="row.key" data-test="share-row"
          class="py-3 flex flex-col sm:flex-row sm:items-center gap-2 sm:gap-4" :class="row.fork ? 'opacity-60' : ''">
        <div class="flex-1 min-w-0">
          <p class="text-sm font-bold text-gray-900 dark:text-zinc-100 truncate">{{ row.label }}</p>
          <p v-if="row.fork" data-test="share-fork" class="text-[11px] text-gray-500 dark:text-zinc-400 font-medium">Shared with parent</p>
        </div>
        <template v-if="!row.fork">
          <label v-if="row.on" class="flex items-center gap-2 text-[11px] text-gray-500 dark:text-zinc-400 font-medium min-w-0">
            <span class="shrink-0">show in team as</span>
            <input :value="row.group" :list="`share-groups-${row.key}`" data-test="share-group" maxlength="100"
                   :aria-label="`Show ${row.workspaceName} in ${row.teamName} as`"
                   @change="setGroup(row, $event.target.value)"
                   class="min-w-0 w-36 bg-gray-50 dark:bg-zinc-800/50 border border-gray-200 dark:border-zinc-800 rounded-sm px-2 py-1 text-xs focus:border-gray-900 dark:focus:border-white focus:ring-0 outline-none font-semibold text-gray-900 dark:text-zinc-100" />
            <datalist :id="`share-groups-${row.key}`">
              <option v-for="g in groupsFor(row.teamId)" :key="g" :value="g" />
            </datalist>
          </label>
          <button type="button" role="switch" data-test="share-switch" :aria-checked="row.on"
                  :aria-label="`Share ${row.workspaceName} with ${row.teamName}`" :disabled="pending.has(row.key)"
                  @click="toggle(row)"
                  class="self-end sm:self-auto shrink-0 w-11 h-6 rounded-full border transition-colors relative disabled:opacity-50"
                  :class="row.on ? 'bg-gray-900 dark:bg-white border-gray-900 dark:border-white' : 'bg-gray-200 dark:bg-zinc-700 border-gray-300 dark:border-zinc-600'">
            <span class="absolute top-[3px] w-4 h-4 rounded-full transition-all"
                  :class="row.on ? 'left-[23px] bg-white dark:bg-zinc-900' : 'left-[3px] bg-white dark:bg-zinc-400'"></span>
          </button>
        </template>
      </li>
    </ul>

    <Teleport to="body">
      <DeleteModal :show="!!unsharing" title="Stop sharing" confirm-label="Stop sharing"
                   :message="unsharing ? `Teammates in ${unsharing.teamName} will stop seeing ${unsharing.workspaceName} and its tasks immediately.` : ''"
                   @close="unsharing = null" @confirm="confirmUnshare" />
    </Teleport>
  </div>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue';
import { listWorkspaceShares, shareWorkspace, unshareWorkspace } from '../api';
import { useTeamStore } from '../stores/teamStore';
import { useWorkspaceStore } from '../stores/workspaceStore';
import { useToasts } from '../composables/useToasts';
import { suggestGroup, teamGroups } from '../composables/useTeamMode';
import DeleteModal from './DeleteModal.vue';

const props = defineProps({
  // One of the two: which workspace to share, or which team to share with.
  workspaceId: { type: String, default: '' },
  teamId: { type: String, default: '' },
});

const teamStore = useTeamStore();
const workspaceStore = useWorkspaceStore();
const { notifySuccess, notifyError } = useToasts();

// What is shared, keyed `${workspaceId}:${teamId}` → its group.
const shared = ref(new Map());
const loading = ref(true);
const pending = ref(new Set());
const unsharing = ref(null);

const keyOf = (workspaceId, teamId) => `${workspaceId}:${teamId}`;

const workspace = computed(() => workspaceStore.getWorkspace(props.workspaceId));
const team = computed(() => teamStore.getTeam(props.teamId));

const rows = computed(() => {
  if (props.workspaceId) {
    const ws = workspace.value;
    return teamStore.teams.map((t) => row(ws, t));
  }
  return workspaceStore.workspaces.filter((w) => !w.archivedAt).map((w) => row(w, team.value));
});

function row(ws, t) {
  const key = keyOf(ws?.id ?? props.workspaceId, t?.id ?? props.teamId);
  return {
    key,
    workspaceId: String(ws?.id ?? props.workspaceId),
    teamId: String(t?.id ?? props.teamId),
    workspaceName: ws?.name || '',
    teamName: t?.name || '',
    label: props.workspaceId ? t?.name : ws?.name,
    fork: !!ws?.forkOfId,
    on: shared.value.has(key),
    group: shared.value.get(key) || '',
  };
}

// The team's groups, offered in the "show in team as" box.
const groupsFor = (teamId) => teamGroups(teamStore.workspacesOf(teamId)).map((g) => g.name);

async function load() {
  loading.value = true;
  const next = new Map();
  try {
    if (props.workspaceId) {
      const res = await listWorkspaceShares(props.workspaceId);
      for (const s of res?.shares || []) next.set(keyOf(props.workspaceId, s.teamId), s.group || '');
    } else {
      if (workspaceStore.workspaces.length === 0) await workspaceStore.fetchWorkspaces();
      // Yours among the team's shared workspaces are the ones shared.
      const mine = new Set(workspaceStore.workspaces.map((w) => String(w.id)));
      for (const w of await teamStore.loadWorkspaces(props.teamId)) {
        if (mine.has(String(w.id))) next.set(keyOf(w.id, props.teamId), w.group || '');
      }
    }
    shared.value = next;
  } catch (err) {
    notifyError(err.message);
  } finally {
    loading.value = false;
  }
}

async function withPending(row, work) {
  pending.value = new Set(pending.value).add(row.key);
  try {
    await work();
  } catch (err) {
    notifyError(err.message);
  } finally {
    const next = new Set(pending.value);
    next.delete(row.key);
    pending.value = next;
  }
}

function put(row, group) {
  return withPending(row, async () => {
    await shareWorkspace(row.workspaceId, row.teamId, group);
    shared.value = new Map(shared.value).set(row.key, group);
  });
}

async function toggle(row) {
  if (row.on) {
    unsharing.value = row;
    return;
  }
  // The team's groups, read now, so the prefill matches what is there.
  const groups = await teamStore.loadWorkspaces(row.teamId).then(
    (list) => teamGroups(list).map((g) => g.name),
    () => [],
  );
  await put(row, suggestGroup(row.workspaceName, groups));
  if (shared.value.has(row.key)) notifySuccess(`Shared ${row.workspaceName} with ${row.teamName}`);
}

function setGroup(row, value) {
  const group = value.trim() || row.workspaceName;
  if (group === row.group) return;
  return put(row, group);
}

async function confirmUnshare() {
  const row = unsharing.value;
  unsharing.value = null;
  await withPending(row, async () => {
    await unshareWorkspace(row.workspaceId, row.teamId);
    const next = new Map(shared.value);
    next.delete(row.key);
    shared.value = next;
    notifySuccess(`Stopped sharing ${row.workspaceName} with ${row.teamName}`);
  });
}

onMounted(load);
watch(() => [props.workspaceId, props.teamId], load);
</script>
