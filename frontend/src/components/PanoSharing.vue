<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- Sharing a Pano doc of yours with your teams, one switch per team, as
     WorkspaceSharing does a workspace. Teammates read it, and only read it, on
     the team's Pano. Turning one off asks first. -->
<template>
  <div data-test="pano-sharing">
    <ul class="divide-y divide-gray-100 dark:divide-zinc-800">
      <li v-for="row in rows" :key="row.teamId" data-test="pano-share-row" class="py-2 flex items-center gap-3">
        <p class="flex-1 min-w-0 text-[12px] font-bold text-gray-700 dark:text-zinc-300 truncate">{{ row.teamName }}</p>
        <button type="button" role="switch" data-test="pano-share-switch" :aria-checked="row.on"
                :aria-label="`Share ${title} with ${row.teamName}`" :disabled="loading || pending.has(row.teamId)"
                @click="toggle(row)"
                class="shrink-0 w-9 h-5 rounded-full border transition-colors relative disabled:opacity-50"
                :class="row.on ? 'bg-gray-900 dark:bg-white border-gray-900 dark:border-white' : 'bg-gray-200 dark:bg-zinc-700 border-gray-300 dark:border-zinc-600'">
          <span class="absolute top-[2px] w-3.5 h-3.5 rounded-full transition-all"
                :class="row.on ? 'left-[18px] bg-white dark:bg-zinc-900' : 'left-[2px] bg-white dark:bg-zinc-400'"></span>
        </button>
      </li>
    </ul>

    <Teleport to="body">
      <DeleteModal :show="!!unsharing" title="Stop sharing" confirm-label="Stop sharing"
                   :message="unsharing ? `Teammates in ${unsharing.teamName} will stop seeing ${title} immediately.` : ''"
                   @close="unsharing = null" @confirm="confirmUnshare" />
    </Teleport>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue';
import { listPanoShares, sharePanoDoc, unsharePanoDoc } from '../api';
import { useTeamStore } from '../stores/teamStore';
import { useToasts } from '../composables/useToasts';
import { useTeamMode } from '../composables/useTeamMode';
import DeleteModal from './DeleteModal.vue';

const props = defineProps({
  docId: { type: String, required: true },
  title: { type: String, default: '' },
});

const teamStore = useTeamStore();
const { notifySuccess, notifyError } = useToasts();
const { changeFailed } = useTeamMode();

// The ids of the teams the doc is shared with.
const shared = ref(new Set());
const loading = ref(true);
const pending = ref(new Set());
const unsharing = ref(null);

const rows = computed(() => teamStore.teams.map((t) => ({
  teamId: String(t.id),
  teamName: t.name || '',
  on: shared.value.has(String(t.id)),
})));

async function load() {
  loading.value = true;
  try {
    const res = await listPanoShares(props.docId);
    shared.value = new Set((res?.shares || []).map((s) => String(s.teamId)));
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
  pending.value = new Set(pending.value).add(row.teamId);
  try {
    const res = await work(teamStore.getTeam(row.teamId)?.version);
    teamStore.setVersion(row.teamId, res?.version);
    done();
  } catch (err) {
    if (await changeFailed(err, row.teamId) === 'conflict') await load();
  } finally {
    const next = new Set(pending.value);
    next.delete(row.teamId);
    pending.value = next;
  }
}

function toggle(row) {
  if (row.on) {
    unsharing.value = row;
    return undefined;
  }
  return change(row, (v) => sharePanoDoc(props.docId, row.teamId, v), () => {
    shared.value = new Set(shared.value).add(row.teamId);
    notifySuccess(`Shared ${props.title} with ${row.teamName}`);
  });
}

function confirmUnshare() {
  const row = unsharing.value;
  unsharing.value = null;
  return change(row, (v) => unsharePanoDoc(props.docId, row.teamId, v), () => {
    const next = new Set(shared.value);
    next.delete(row.teamId);
    shared.value = next;
    notifySuccess(`Stopped sharing ${props.title} with ${row.teamName}`);
  });
}

onMounted(load);
</script>
