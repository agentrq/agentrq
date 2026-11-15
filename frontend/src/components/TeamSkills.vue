<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- The skills on in one team workspace's shared workspaces
     (/teams/<id>/skills?workspace=), drawn as /skills draws yours, with who has
     each. There is no list across the team: each workspace has its own. One
     click adds it to your own skills as a link, not a copy, on in your
     workspace of that name if you have one: your agents read and improve the
     teammate's skill itself. A skill's page shows its SKILL.md, read-only,
     and lets you choose workspaces to turn it on in as you add it, or swap
     your own copy of it for a link to the teammate's. -->
<template>
  <div data-test="team-skills" class="space-y-6">
    <p v-if="!workspace" data-test="team-skills-pick" class="text-[11px] text-gray-500 dark:text-zinc-400 ml-1">
      Skills belong to a workspace: pick one in the sidebar, then open its skills.
    </p>

    <template v-else-if="name">
      <router-link :to="inWorkspace('/skills')" class="text-[9px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-white">← Skills</router-link>
      <p v-if="error" data-test="team-skill-error" class="text-[11px] font-bold text-red-600 dark:text-red-400">{{ error }}</p>
      <p v-else-if="!skill" class="text-[11px] text-gray-400 dark:text-zinc-500">Loading…</p>
      <template v-else>
        <div class="flex flex-wrap items-center gap-3">
          <h2 class="text-sm font-bold font-mono text-gray-900 dark:text-zinc-100 break-all">{{ skill.name || name }}</h2>
          <span class="flex -space-x-1"><OwnerAvatar v-for="o in skill.owners || []" :key="o.userId ?? o.name" :owner="o" /></span>
        </div>
        <p v-if="skill.description" class="text-[12px] text-gray-500 dark:text-zinc-400">{{ skill.description }}</p>

        <div class="sm:p-4 sm:bg-gray-50 sm:dark:bg-zinc-800/50 sm:rounded-sm sm:border border-gray-100 dark:border-zinc-800 space-y-3">
          <template v-if="isMine(name)">
            <p data-test="team-skill-mine" class="text-[11px] text-gray-600 dark:text-zinc-300">
              In your skills. <router-link :to="ownPage(name)" class="font-semibold underline text-gray-900 dark:text-white">Open yours</router-link>
            </p>
            <!-- Your own copy, not a link: swap it for the teammate's. -->
            <template v-if="ownCopy(name)">
              <p v-if="addError" data-test="team-skill-add-error" role="alert" class="text-[11px] font-bold text-red-600 dark:text-red-400">{{ addError }}</p>
              <button type="button" data-test="team-skill-replace" :disabled="adding.has(name)" @click="replacing = name"
                      class="px-4 py-2 rounded-lg border border-gray-300 dark:border-zinc-600 text-gray-900 dark:text-white text-[11px] font-black uppercase tracking-widest hover:bg-gray-100 dark:hover:bg-zinc-800 disabled:opacity-40">
                {{ adding.has(name) ? 'Switching…' : "Use the team's instead" }}
              </button>
            </template>
          </template>
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

        <DeleteModal :show="replacing === name" title="Use the team's skill" confirm-label="Use theirs"
                     :message="`Your own ${name} and its files are deleted, and your agents use your teammate's instead. Its workspace switches stay as they are.`"
                     @close="replacing = ''" @confirm="swap(name)" />

        <div data-test="team-skill-body" class="md-body text-[13px] text-gray-800 dark:text-zinc-200" v-html="renderMarkdown(skillBody(skill.content))"></div>
      </template>
    </template>

    <template v-else>
      <p v-if="error" data-test="team-skill-error" class="text-[11px] font-bold text-red-600 dark:text-red-400">{{ error }}</p>
      <p v-else-if="skills === null" class="text-[11px] text-gray-400 dark:text-zinc-500 ml-1">Loading skills…</p>
      <div v-else-if="skills.length === 0" data-test="team-skills-empty" class="p-6 bg-gray-50 dark:bg-zinc-800/50 rounded-sm border border-gray-100 dark:border-zinc-800 text-center">
        <p class="text-[11px] font-bold text-gray-700 dark:text-zinc-200">No skills yet.</p>
        <p class="mt-1 text-[11px] text-gray-500 dark:text-zinc-400">A skill shows here once it is on in one of this workspace's shares.</p>
      </div>
      <!-- The cards of /skills; the corner adds one, or says you have it. -->
      <div v-else data-test="team-skill-grid" class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 sm:gap-2 divide-y sm:divide-y-0 divide-gray-100 dark:divide-zinc-800 border-y sm:border-y-0 border-gray-100 dark:border-zinc-800">
        <SkillCard v-for="s in skills" :key="s.name" :to="inWorkspace(`/skills/${encodeURIComponent(s.name)}`)"
                   :name="s.name" :description="s.description" link-test="team-skill-row" action-width="pr-16">
          <template #meta>
            <span class="flex -space-x-1 shrink-0"><OwnerAvatar v-for="o in s.owners || []" :key="o.userId ?? o.name" :owner="o" /></span>
            <span class="min-w-0 truncate">{{ sharedBy(s) }}</span>
          </template>
          <template #action>
            <span v-if="isMine(s.name)" data-test="team-skill-mine" title="In your skills"
                  class="block text-[8px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400 border border-gray-300 dark:border-zinc-600 rounded px-1.5 py-1">Added</span>
            <button v-else type="button" data-test="team-skill-add" :aria-label="`Add ${s.name} to my skills`"
                    :disabled="adding.has(s.name)" @click="add(s.name, ownIds)"
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
import { useRoute } from 'vue-router';
import { addTeamSkill, fetchTeamSkills, getTeamSkill, searchSkills } from '../api';
import DeleteModal from './DeleteModal.vue';
import OwnerAvatar from './OwnerAvatar.vue';
import SkillCard from './SkillCard.vue';
import SkillWorkspaceChoices from './SkillWorkspaceChoices.vue';
import { renderMarkdown } from '../utils/markdown';
import { skillBody, skillWorkspaceChoices } from '../composables/useSkills';
import { useWorkspaceStore } from '../stores/workspaceStore';
import { useToasts } from '../composables/useToasts';
import { lostTeam, ownTeamWorkspace, useTeamMode } from '../composables/useTeamMode';

const props = defineProps({ name: { type: String, default: '' } });

const workspaceStore = useWorkspaceStore();
const { notifySuccess, notifyError } = useToasts();
const { teamId, link, leaveTeam } = useTeamMode();
const route = useRoute();
// The team workspace on screen, by its shared name.
const workspace = computed(() => (typeof route.query.workspace === 'string' ? route.query.workspace : ''));
const inWorkspace = (path) => link({ path, query: { workspace: workspace.value } });

const skills = ref(null);
const skill = ref(null);
const error = ref('');
// Your own skills, by name, with where each came from: one you have is not
// offered again, and one of your own (not a link) can be swapped for the
// team's.
const mine = ref(new Map());
// The skill whose swap is being confirmed.
const replacing = ref('');
// Skills being added, by name, while their answer is out.
const adding = ref(new Set());
const addError = ref('');
// Your workspace of the team workspace's name, ticked to start with: a skill
// added from a workspace is for that workspace.
const workspaceIds = ref([]);

const choices = computed(() => skillWorkspaceChoices(workspaceStore.workspaces));
const ownId = computed(() => ownTeamWorkspace(workspaceStore.workspaces, workspace.value));
const ownIds = computed(() => (ownId.value ? [ownId.value] : []));
watch(ownIds, (ids) => { workspaceIds.value = [...ids]; }, { immediate: true });
const isMine = (n) => mine.value.has(n);
const ownCopy = (n) => mine.value.has(n) && mine.value.get(n) !== 'team';
const ownPage = (n) => `/skills/${encodeURIComponent(n)}`;
const sharedBy = (s) => {
  const n = (s.owners || []).length;
  return n === 1 ? `· ${s.owners[0].name || 'a teammate'}` : `· ${n} teammates`;
};

async function loadMine() {
  // Without your list every skill is offered; the add then says if you have it.
  try {
    mine.value = new Map(((await searchSkills())?.skills || []).map((s) => [s.name, s.sourceType]));
  } catch {
    mine.value = new Map();
  }
}

async function load() {
  error.value = '';
  addError.value = '';
  skills.value = null;
  skill.value = null;
  workspaceIds.value = [...ownIds.value];
  if (!workspace.value) return undefined;
  try {
    if (props.name) {
      const res = await getTeamSkill(teamId.value, props.name, workspace.value);
      skill.value = res?.skill ?? res;
    } else {
      skills.value = (await fetchTeamSkills(teamId.value, workspace.value))?.skills ?? [];
    }
  } catch (err) {
    if (lostTeam(err) && !props.name) return leaveTeam();
    error.value = err.message;
  }
  return undefined;
}

async function add(skillName, ids, replace = false) {
  adding.value = new Set(adding.value).add(skillName);
  addError.value = '';
  try {
    await addTeamSkill(teamId.value, skillName, [...ids], workspace.value, replace);
    mine.value = new Map(mine.value).set(skillName, 'team');
    const done = replace ? `Your ${skillName} is now your teammate's` : `Added ${skillName} to your skills`;
    notifySuccess(`${done}: your agents improve the teammate's skill, not a copy`, null, { path: ownPage(skillName), label: 'Open' });
  } catch (err) {
    if (props.name) addError.value = err.message;
    else notifyError(err.message);
  } finally {
    const next = new Set(adding.value);
    next.delete(skillName);
    adding.value = next;
  }
}

function swap(skillName) {
  replacing.value = '';
  return add(skillName, [], true);
}

onMounted(() => {
  loadMine();
  // The store reports its own failures; the page then offers no workspace.
  if (workspaceStore.workspaces.length === 0) workspaceStore.fetchWorkspaces();
});
watch(() => [props.name, teamId.value, workspace.value], load, { immediate: true });
</script>
