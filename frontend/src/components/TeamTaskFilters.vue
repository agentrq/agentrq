<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- Team mode's two filters, on the task list and the board: whose tasks
     (a member, with their avatar) and which team workspace (with its owners'
     avatars). Both live in the URL (?member=, ?workspace=), so a link keeps
     them. -->
<template>
  <div class="flex items-center gap-2 min-w-0" data-test="team-task-filters">
    <ItemPicker :model-value="member" @update:model-value="set('member', $event)" :items="memberChoices(owners)"
                data-test="team-filter-member" aria-label="Member" :align="align" class="max-w-[7.5rem] @min-[48rem]:max-w-[11rem]" />
    <ItemPicker :model-value="workspace" @update:model-value="set('workspace', $event)" :items="workspaceChoices(groups)"
                data-test="team-filter-workspace" aria-label="Workspace" :align="align" class="max-w-[7.5rem] @min-[48rem]:max-w-[11rem]" />
  </div>
</template>

<script setup>
import { computed } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import ItemPicker from './ItemPicker.vue';
import { useTeamStore } from '../stores/teamStore';
import { memberChoices, teamOwners, teamWorkspaceList, useTeamMode, workspaceChoices } from '../composables/useTeamMode';

defineProps({ align: { type: String, default: 'right' } });

const route = useRoute();
const router = useRouter();
const store = useTeamStore();
const { teamId } = useTeamMode();

const queryOf = (key) => (typeof route.query[key] === 'string' ? route.query[key] : '');
const member = computed(() => queryOf('member'));
const workspace = computed(() => queryOf('workspace'));
const workspaces = computed(() => store.workspacesOf(teamId.value));
const owners = computed(() => teamOwners(workspaces.value));
const groups = computed(() => teamWorkspaceList(workspaces.value));

function set(key, value) {
  const query = { ...route.query, [key]: value };
  if (!value) delete query[key];
  return router.push({ path: route.path, query });
}
</script>
