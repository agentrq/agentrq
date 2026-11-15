<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- Sharing your workspaces with teams (§4 of the flows). One control, two
     places: Workspace settings › Sharing lists your teams for one workspace
     (`workspace-id`), and the Manage team dialog › Your workspaces lists your
     workspaces for one team (`team-id`). A workspace shows in the team under
     its own name, as one team workspace with any teammate's of the same name.
     Turning one off asks first. A fork follows its parent, so it has no switch
     of its own. In Workspace settings a share also says what the team reads of
     the workspace: its tasks, skills and memory, each a switch of its own, and
     whether the team workspace's skills are turned on in it as they come. -->
<template>
  <div data-test="workspace-sharing">
    <p v-if="rows.length === 0 && !loading" data-test="sharing-empty" class="text-[11px] text-gray-500 dark:text-zinc-400 font-medium">
      <template v-if="workspaceId">You are not in a team yet. <router-link to="/team/new" class="font-semibold underline text-gray-900 dark:text-white">Create one</router-link> or accept an invitation to share this workspace.</template>
      <template v-else>You have no workspaces to share.</template>
    </p>

    <ul v-else class="divide-y divide-gray-100 dark:divide-zinc-800">
      <li v-for="row in rows" :key="row.key" data-test="share-row"
          class="py-3 flex items-center gap-4" :class="row.fork ? 'opacity-60' : ''">
        <div class="flex-1 min-w-0">
          <p class="text-sm font-bold text-gray-900 dark:text-zinc-100 truncate">{{ row.label }}</p>
          <p v-if="row.fork" data-test="share-fork" class="text-[11px] text-gray-500 dark:text-zinc-400 font-medium">Shared with parent</p>
          <div v-if="row.parts" data-test="share-parts" class="mt-2 flex flex-wrap gap-x-4 gap-y-2">
            <span v-for="p in PARTS" :key="p.key" class="flex items-center gap-1.5 text-[11px] font-semibold text-gray-600 dark:text-zinc-300">
              <button type="button" role="switch" :data-test="`share-part-${p.key}`" :aria-checked="row.parts[p.key]"
                      :aria-label="`Share the ${p.key} of ${row.workspaceName} with ${row.teamName}`" :disabled="pending.has(row.key)"
                      @click="togglePart(row, p)"
                      class="shrink-0 w-7 h-4 rounded-full border transition-colors relative disabled:opacity-50"
                      :class="row.parts[p.key] ? 'bg-gray-900 dark:bg-white border-gray-900 dark:border-white' : 'bg-gray-200 dark:bg-zinc-700 border-gray-300 dark:border-zinc-600'">
                <span class="absolute top-[2px] w-2.5 h-2.5 rounded-full transition-all"
                      :class="row.parts[p.key] ? 'left-[14px] bg-white dark:bg-zinc-900' : 'left-[2px] bg-white dark:bg-zinc-400'"></span>
              </button>
              {{ p.label }}
            </span>
          </div>
          <div v-if="row.parts" data-test="share-auto-opt-in" class="mt-2">
            <span class="flex items-center gap-1.5 text-[11px] font-semibold text-gray-600 dark:text-zinc-300">
              <button type="button" role="switch" data-test="share-part-autoOptInSkills" :aria-checked="row.parts.autoOptInSkills"
                      :aria-label="`Turn new team skills on in ${row.workspaceName}`" :disabled="pending.has(row.key)"
                      @click="togglePart(row, AUTO_OPT_IN)"
                      class="shrink-0 w-7 h-4 rounded-full border transition-colors relative disabled:opacity-50"
                      :class="row.parts.autoOptInSkills ? 'bg-gray-900 dark:bg-white border-gray-900 dark:border-white' : 'bg-gray-200 dark:bg-zinc-700 border-gray-300 dark:border-zinc-600'">
                <span class="absolute top-[2px] w-2.5 h-2.5 rounded-full transition-all"
                      :class="row.parts.autoOptInSkills ? 'left-[14px] bg-white dark:bg-zinc-900' : 'left-[2px] bg-white dark:bg-zinc-400'"></span>
              </button>
              {{ AUTO_OPT_IN.label }}
            </span>
            <p class="mt-1 text-[10px] text-gray-500 dark:text-zinc-400">New team skills are turned on in this workspace. Skills you turn off stay off.</p>
          </div>
        </div>
        <button v-if="!row.fork" type="button" role="switch" data-test="share-switch" :aria-checked="row.on"
                :aria-label="`Share ${row.workspaceName} with ${row.teamName}`" :disabled="pending.has(row.key)"
                @click="toggle(row)"
                class="shrink-0 w-11 h-6 rounded-full border transition-colors relative disabled:opacity-50"
                :class="row.on ? 'bg-gray-900 dark:bg-white border-gray-900 dark:border-white' : 'bg-gray-200 dark:bg-zinc-700 border-gray-300 dark:border-zinc-600'">
          <span class="absolute top-[3px] w-4 h-4 rounded-full transition-all"
                :class="row.on ? 'left-[23px] bg-white dark:bg-zinc-900' : 'left-[3px] bg-white dark:bg-zinc-400'"></span>
        </button>
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
import { useTeamMode } from '../composables/useTeamMode';
import DeleteModal from './DeleteModal.vue';

const props = defineProps({
  // One of the two: which workspace to share, or which team to share with.
  workspaceId: { type: String, default: '' },
  teamId: { type: String, default: '' },
});

// After a share or an unshare, which changes who the teammates are.
const emit = defineEmits(['changed']);

const teamStore = useTeamStore();
const workspaceStore = useWorkspaceStore();
const { notifySuccess, notifyError } = useToasts();
const { changeFailed } = useTeamMode();

// What is shared, keyed `${workspaceId}:${teamId}`.
const shared = ref(new Set());
// What each share lets the team read, by the same key; Workspace settings only.
const parts = ref(new Map());

// The parts of a workspace a share can hold back from the team.
const PARTS = [
  { key: 'tasks', label: 'Tasks' },
  { key: 'skills', label: 'Skills' },
  { key: 'memory', label: 'Memory' },
];

// Not what the team reads but what the workspace takes from it: each skill of
// the team workspace it has made no choice about is turned on in it.
const AUTO_OPT_IN = { key: 'autoOptInSkills', label: 'Auto opt-in to team skills' };

// What a share's answer says the team reads; a part it does not name is shared.
const partsOf = (s) => ({
  tasks: s?.tasks !== false,
  skills: s?.skills !== false,
  memory: s?.memory !== false,
  autoOptInSkills: s?.autoOptInSkills !== false,
});

function partMessage(row, key, on) {
  if (key === AUTO_OPT_IN.key) {
    return on ? `New team skills are turned on in ${row.workspaceName}` : `New team skills stay off in ${row.workspaceName}`;
  }
  return on ? `${row.teamName} now reads the ${key} of ${row.workspaceName}` : `${row.teamName} no longer reads the ${key} of ${row.workspaceName}`;
}
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
  const on = shared.value.has(key);
  return {
    key,
    workspaceId: String(ws?.id ?? props.workspaceId),
    teamId: String(t?.id ?? props.teamId),
    workspaceName: ws?.name || '',
    teamName: t?.name || '',
    label: props.workspaceId ? t?.name : ws?.name,
    fork: !!ws?.forkOfId,
    on,
    parts: on && props.workspaceId && !ws?.forkOfId ? parts.value.get(key) : null,
  };
}

async function load() {
  loading.value = true;
  const next = new Set();
  const nextParts = new Map();
  try {
    if (props.workspaceId) {
      const res = await listWorkspaceShares(props.workspaceId);
      for (const s of res?.shares || []) {
        next.add(keyOf(props.workspaceId, s.teamId));
        nextParts.set(keyOf(props.workspaceId, s.teamId), partsOf(s));
      }
    } else {
      if (workspaceStore.workspaces.length === 0) await workspaceStore.fetchWorkspaces();
      // Yours among the team's shared workspaces are the ones shared.
      const mine = new Set(workspaceStore.workspaces.map((w) => String(w.id)));
      for (const w of await teamStore.loadWorkspaces(props.teamId)) {
        if (mine.has(String(w.id))) next.add(keyOf(w.id, props.teamId));
      }
    }
    shared.value = next;
    parts.value = nextParts;
  } catch (err) {
    notifyError(err.message);
  } finally {
    loading.value = false;
  }
}

/**
 * A share or an unshare, against the version of the team the switch knows;
 * its answer moves the version on. One made over someone else's change reads
 * the team and the shares again.
 */
async function change(row, work, done) {
  pending.value = new Set(pending.value).add(row.key);
  try {
    const res = await work(teamStore.getTeam(row.teamId)?.version);
    teamStore.setVersion(row.teamId, res?.version);
    done(res);
    emit('changed');
  } catch (err) {
    if (await changeFailed(err, row.teamId) === 'conflict') await load();
  } finally {
    const next = new Set(pending.value);
    next.delete(row.key);
    pending.value = next;
  }
}

function toggle(row) {
  if (row.on) {
    unsharing.value = row;
    return undefined;
  }
  return change(row, (v) => shareWorkspace(row.workspaceId, row.teamId, v), (res) => {
    shared.value = new Set(shared.value).add(row.key);
    parts.value = new Map(parts.value).set(row.key, partsOf(res));
    notifySuccess(`Shared ${row.workspaceName} with ${row.teamName}`);
  });
}

// Shares one part of a shared workspace with the team, or holds it back.
function togglePart(row, part) {
  const next = { ...row.parts, [part.key]: !row.parts[part.key] };
  return change(row, (v) => shareWorkspace(row.workspaceId, row.teamId, v, next), () => {
    parts.value = new Map(parts.value).set(row.key, next);
    notifySuccess(partMessage(row, part.key, next[part.key]));
  });
}

function confirmUnshare() {
  const row = unsharing.value;
  unsharing.value = null;
  return change(row, (v) => unshareWorkspace(row.workspaceId, row.teamId, v), () => {
    const next = new Set(shared.value);
    next.delete(row.key);
    shared.value = next;
    notifySuccess(`Stopped sharing ${row.workspaceName} with ${row.teamName}`);
  });
}

onMounted(load);
watch(() => [props.workspaceId, props.teamId], load);
</script>
