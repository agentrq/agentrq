<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<!-- Team mode's two filters, on the task list and the board: whose tasks
     (a member, with their avatar) and which team workspace (with its owners'
     avatars). Both live in the URL (?member=, ?workspace=), so a link keeps
     them. On a narrow page each folds to an icon. -->
<template>
  <div class="flex items-center gap-2 min-w-0" data-test="team-task-filters">
    <ItemPicker :model-value="member" @update:model-value="set('member', $event)" :items="memberChoices(owners)"
                data-test="team-filter-member" aria-label="Member" :icon="memberIcon" :align="align" class="max-w-[7.5rem] @min-[48rem]:max-w-[11rem]" />
    <ItemPicker :model-value="workspace" @update:model-value="set('workspace', $event)" :items="workspaceChoices(groups)"
                data-test="team-filter-workspace" aria-label="Workspace" :icon="workspaceIcon" :align="align" class="max-w-[7.5rem] @min-[48rem]:max-w-[11rem]" />
  </div>
</template>

<script setup>
import { computed } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import ItemPicker from './ItemPicker.vue';
import { useTeamStore } from '../stores/teamStore';
import { memberChoices, teamOwners, teamWorkspaceList, useTeamMode, workspaceChoices } from '../composables/useTeamMode';

defineProps({ align: { type: String, default: 'right' } });

// What each picker folds to on a narrow page: a person, and a folder.
const memberIcon = 'M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z';
const workspaceIcon = 'M3 7a2 2 0 012-2h4l2 2h8a2 2 0 012 2v8a2 2 0 01-2 2H5a2 2 0 01-2-2V7z';

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
