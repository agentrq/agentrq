<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- The skills on in a team's shared workspaces (/skills?team=), with who has
     each; one opened shows its SKILL.md, read-only, and offers to add a copy
     to your own skills. Nothing here edits a teammate's skill. -->
<template>
  <div data-test="team-skills" class="space-y-4">
    <template v-if="name">
      <router-link :to="link('/skills')" class="text-[9px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-white">← All skills</router-link>
      <p v-if="error" data-test="team-skill-error" class="text-[11px] font-bold text-red-600 dark:text-red-400">{{ error }}</p>
      <p v-else-if="!skill" class="text-[11px] text-gray-400 dark:text-zinc-500">Loading…</p>
      <template v-else>
        <div class="flex flex-wrap items-center gap-3">
          <h2 class="text-sm font-bold font-mono text-gray-900 dark:text-zinc-100">{{ skill.name || name }}</h2>
          <span class="flex -space-x-1"><OwnerAvatar v-for="o in skill.owners || []" :key="o.userId ?? o.name" :owner="o" /></span>
          <button type="button" data-test="team-skill-add" @click="adding = !adding"
                  class="ml-auto px-4 py-2 rounded-lg bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest hover:opacity-90">Add to my skills</button>
        </div>
        <form v-if="adding" data-test="team-skill-add-form" @submit.prevent="add"
              class="p-4 bg-gray-50 dark:bg-zinc-800/50 border border-gray-100 dark:border-zinc-800 rounded-sm space-y-3">
          <fieldset v-if="mine.length" class="min-w-0">
            <legend class="mb-1.5 text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Turn on in</legend>
            <div class="flex flex-wrap gap-1.5">
              <label v-for="w in mine" :key="w.id" class="inline-flex items-center gap-1.5 px-2 py-1 border border-gray-200 dark:border-zinc-700 rounded-sm text-[11px] text-gray-700 dark:text-zinc-200 cursor-pointer">
                <input v-model="workspaceIds" type="checkbox" :value="String(w.id)" data-test="team-skill-workspace" class="rounded-sm border-gray-300 dark:border-zinc-600" />
                <span class="truncate">{{ w.name }}</span>
              </label>
            </div>
          </fieldset>
          <p v-if="addError" data-test="team-skill-add-error" role="alert" class="text-[11px] font-bold text-red-600 dark:text-red-400">{{ addError }}</p>
          <button type="submit" data-test="team-skill-add-confirm" :disabled="busy"
                  class="px-4 py-2 rounded-lg bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest disabled:opacity-40">{{ busy ? 'Adding…' : 'Add' }}</button>
        </form>
        <div data-test="team-skill-body" class="md-body text-[13px] text-gray-800 dark:text-zinc-200" v-html="renderMarkdown(skillBody(skill.content))"></div>
      </template>
    </template>

    <template v-else>
      <p v-if="error" data-test="team-skill-error" class="text-[11px] font-bold text-red-600 dark:text-red-400">{{ error }}</p>
      <p v-else-if="skills === null" class="text-[11px] text-gray-400 dark:text-zinc-500">Loading skills…</p>
      <p v-else-if="skills.length === 0" data-test="team-skills-empty" class="text-[11px] text-gray-500 dark:text-zinc-400">No skill is on in a shared workspace yet.</p>
      <ul v-else class="divide-y divide-gray-100 dark:divide-zinc-800">
        <li v-for="s in skills" :key="s.name" data-test="team-skill-row">
          <router-link :to="link(`/skills/${encodeURIComponent(s.name)}`)" class="flex items-center gap-3 py-3 group">
            <div class="flex-1 min-w-0">
              <p class="text-sm font-bold font-mono text-gray-900 dark:text-zinc-100 truncate group-hover:underline">{{ s.name }}</p>
              <p class="text-[11px] text-gray-500 dark:text-zinc-400 truncate">{{ s.description }}</p>
            </div>
            <span class="flex -space-x-1 shrink-0"><OwnerAvatar v-for="o in s.owners || []" :key="o.userId ?? o.name" :owner="o" /></span>
          </router-link>
        </li>
      </ul>
    </template>
  </div>
</template>

<script setup>
import { computed, ref, watch } from 'vue';
import { addTeamSkill, fetchTeamSkills, getTeamSkill } from '../api';
import OwnerAvatar from './OwnerAvatar.vue';
import { renderMarkdown } from '../utils/markdown';
import { skillBody } from '../composables/useSkills';
import { useWorkspaceStore } from '../stores/workspaceStore';
import { useToasts } from '../composables/useToasts';
import { lostTeam, useTeamMode } from '../composables/useTeamMode';

const props = defineProps({ name: { type: String, default: '' } });

const workspaceStore = useWorkspaceStore();
const { notifySuccess } = useToasts();
const { teamId, link, leaveTeam } = useTeamMode();

const skills = ref(null);
const skill = ref(null);
const error = ref('');
const adding = ref(false);
const workspaceIds = ref([]);
const busy = ref(false);
const addError = ref('');

const mine = computed(() => workspaceStore.workspaces.filter((w) => !w.archivedAt));

async function load() {
  error.value = '';
  skills.value = null;
  skill.value = null;
  adding.value = false;
  try {
    if (props.name) {
      const res = await getTeamSkill(teamId.value, props.name);
      skill.value = res?.skill ?? res;
    } else {
      skills.value = (await fetchTeamSkills(teamId.value))?.skills ?? [];
    }
  } catch (err) {
    if (lostTeam(err) && !props.name) return leaveTeam();
    error.value = err.message;
  }
}

async function add() {
  busy.value = true;
  addError.value = '';
  try {
    await addTeamSkill(teamId.value, props.name, workspaceIds.value);
    adding.value = false;
    notifySuccess(`Added ${props.name} to your skills`, null, { path: `/skills/${encodeURIComponent(props.name)}`, label: 'Open' });
  } catch (err) {
    addError.value = err.message;
  } finally {
    busy.value = false;
  }
}

watch(adding, (open) => {
  // The store reports its own failures; the form then offers no workspace.
  if (open && workspaceStore.workspaces.length === 0) workspaceStore.fetchWorkspaces();
});
watch(() => [props.name, teamId.value], load, { immediate: true });
</script>
