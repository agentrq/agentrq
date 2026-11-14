<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- The skills on in a team's shared workspaces (/skills?team=), drawn as
     /skills draws yours, with who has each. One click adds a copy to your own
     skills, on in no workspace; a skill's page shows its SKILL.md, read-only,
     and lets you choose workspaces to turn it on in as you add it. Nothing
     here edits a teammate's skill. -->
<template>
  <div data-test="team-skills" class="space-y-6">
    <template v-if="name">
      <router-link :to="link('/skills')" class="text-[9px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-white">← All skills</router-link>
      <p v-if="error" data-test="team-skill-error" class="text-[11px] font-bold text-red-600 dark:text-red-400">{{ error }}</p>
      <p v-else-if="!skill" class="text-[11px] text-gray-400 dark:text-zinc-500">Loading…</p>
      <template v-else>
        <div class="flex flex-wrap items-center gap-3">
          <h2 class="text-sm font-bold font-mono text-gray-900 dark:text-zinc-100 break-all">{{ skill.name || name }}</h2>
          <span class="flex -space-x-1"><OwnerAvatar v-for="o in skill.owners || []" :key="o.userId ?? o.name" :owner="o" /></span>
        </div>
        <p v-if="skill.description" class="text-[12px] text-gray-500 dark:text-zinc-400">{{ skill.description }}</p>

        <div class="sm:p-4 sm:bg-gray-50 sm:dark:bg-zinc-800/50 sm:rounded-sm sm:border border-gray-100 dark:border-zinc-800 space-y-3">
          <p v-if="isMine(name)" data-test="team-skill-mine" class="text-[11px] text-gray-600 dark:text-zinc-300">
            In your skills. <router-link :to="ownPage(name)" class="font-semibold underline text-gray-900 dark:text-white">Open yours</router-link>
          </p>
          <template v-else>
            <SkillWorkspaceChoices v-model="workspaceIds" :workspaces="choices" legend="Turn on in (optional)"
                                   data-test="team-skill-workspaces" item-test="team-skill-workspace" />
            <p v-if="addError" data-test="team-skill-add-error" role="alert" class="text-[11px] font-bold text-red-600 dark:text-red-400">{{ addError }}</p>
            <button type="button" data-test="team-skill-add" :disabled="adding.has(name)" @click="add(name, workspaceIds)"
                    class="px-4 py-2 rounded-lg bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest hover:opacity-90 disabled:opacity-40">
              {{ adding.has(name) ? 'Adding…' : 'Add to my skills' }}
            </button>
          </template>
        </div>

        <div data-test="team-skill-body" class="md-body text-[13px] text-gray-800 dark:text-zinc-200" v-html="renderMarkdown(skillBody(skill.content))"></div>
      </template>
    </template>

    <template v-else>
      <p v-if="error" data-test="team-skill-error" class="text-[11px] font-bold text-red-600 dark:text-red-400">{{ error }}</p>
      <p v-else-if="skills === null" class="text-[11px] text-gray-400 dark:text-zinc-500 ml-1">Loading skills…</p>
      <div v-else-if="skills.length === 0" data-test="team-skills-empty" class="p-6 bg-gray-50 dark:bg-zinc-800/50 rounded-sm border border-gray-100 dark:border-zinc-800 text-center">
        <p class="text-[11px] font-bold text-gray-700 dark:text-zinc-200">No skills yet.</p>
        <p class="mt-1 text-[11px] text-gray-500 dark:text-zinc-400">A skill shows here once it is on in a shared workspace.</p>
      </div>
      <!-- The cards of /skills; the corner adds one, or says you have it. -->
      <div v-else data-test="team-skill-grid" class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 sm:gap-2 divide-y sm:divide-y-0 divide-gray-100 dark:divide-zinc-800 border-y sm:border-y-0 border-gray-100 dark:border-zinc-800">
        <SkillCard v-for="s in skills" :key="s.name" :to="link(`/skills/${encodeURIComponent(s.name)}`)"
                   :name="s.name" :description="s.description" link-test="team-skill-row" action-width="pr-16">
          <template #meta>
            <span class="flex -space-x-1 shrink-0"><OwnerAvatar v-for="o in s.owners || []" :key="o.userId ?? o.name" :owner="o" /></span>
            <span class="min-w-0 truncate">{{ sharedBy(s) }}</span>
          </template>
          <template #action>
            <span v-if="isMine(s.name)" data-test="team-skill-mine" title="In your skills"
                  class="block text-[8px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400 border border-gray-300 dark:border-zinc-600 rounded px-1.5 py-1">Added</span>
            <button v-else type="button" data-test="team-skill-add" :aria-label="`Add ${s.name} to my skills`"
                    :disabled="adding.has(s.name)" @click="add(s.name, [])"
                    class="block px-2.5 py-1 rounded-lg bg-black dark:bg-white text-white dark:text-black text-[9px] font-black uppercase tracking-widest hover:opacity-90 disabled:opacity-40">
              {{ adding.has(s.name) ? 'Adding…' : 'Add' }}
            </button>
          </template>
        </SkillCard>
      </div>
    </template>
  </div>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue';
import { addTeamSkill, fetchTeamSkills, getTeamSkill, searchSkills } from '../api';
import OwnerAvatar from './OwnerAvatar.vue';
import SkillCard from './SkillCard.vue';
import SkillWorkspaceChoices from './SkillWorkspaceChoices.vue';
import { renderMarkdown } from '../utils/markdown';
import { skillBody, skillWorkspaceChoices } from '../composables/useSkills';
import { useWorkspaceStore } from '../stores/workspaceStore';
import { useToasts } from '../composables/useToasts';
import { lostTeam, useTeamMode } from '../composables/useTeamMode';

const props = defineProps({ name: { type: String, default: '' } });

const workspaceStore = useWorkspaceStore();
const { notifySuccess, notifyError } = useToasts();
const { teamId, link, leaveTeam } = useTeamMode();

const skills = ref(null);
const skill = ref(null);
const error = ref('');
// The names of your own skills, so one you have is not offered again.
const mine = ref(new Set());
// Skills being added, by name, while their answer is out.
const adding = ref(new Set());
const addError = ref('');
// None ticked, as on /skills: turning one on is a choice.
const workspaceIds = ref([]);

const choices = computed(() => skillWorkspaceChoices(workspaceStore.workspaces));
const isMine = (n) => mine.value.has(n);
const ownPage = (n) => `/skills/${encodeURIComponent(n)}`;
const sharedBy = (s) => {
  const n = (s.owners || []).length;
  return n === 1 ? `· ${s.owners[0].name || 'a teammate'}` : `· ${n} teammates`;
};

async function loadMine() {
  // Without your list every skill is offered; the add then says if you have it.
  try {
    mine.value = new Set(((await searchSkills())?.skills || []).map((s) => s.name));
  } catch {
    mine.value = new Set();
  }
}

async function load() {
  error.value = '';
  addError.value = '';
  skills.value = null;
  skill.value = null;
  workspaceIds.value = [];
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
  return undefined;
}

async function add(skillName, ids) {
  adding.value = new Set(adding.value).add(skillName);
  addError.value = '';
  try {
    await addTeamSkill(teamId.value, skillName, [...ids]);
    mine.value = new Set(mine.value).add(skillName);
    notifySuccess(`Added ${skillName} to your skills`, null, { path: ownPage(skillName), label: 'Open' });
  } catch (err) {
    if (props.name) addError.value = err.message;
    else notifyError(err.message);
  } finally {
    const next = new Set(adding.value);
    next.delete(skillName);
    adding.value = next;
  }
}

onMounted(() => {
  loadMine();
  // The store reports its own failures; the page then offers no workspace.
  if (workspaceStore.workspaces.length === 0) workspaceStore.fetchWorkspaces();
});
watch(() => [props.name, teamId.value], load, { immediate: true });
</script>
