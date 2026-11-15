<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- A workspace's Skills tab, its "Team skills" group: the skills owned by the
     team workspaces it is shared into, each with this workspace's switch.
     Turning one off opts the workspace out, and it stays out. When one of
     your own skills of the same name is on here it wins (`clash`); "Use the
     team's instead" turns yours off in this workspace. The team's skills are
     changed on the team's Skills page, not here. -->
<template>
  <section data-test="workspace-team-skills" class="space-y-3">
    <div class="space-y-1">
      <h3 class="text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest ml-1">Team skills</h3>
      <p class="text-[11px] text-gray-500 dark:text-zinc-400 font-medium ml-1">
        Skills the team owns. Turn one on for this workspace's agents to see and improve it.
      </p>
    </div>

    <div v-if="skills.length === 0" data-test="team-skills-none" class="p-4 bg-gray-50 dark:bg-zinc-800/50 rounded-sm border border-gray-100 dark:border-zinc-800 text-center">
      <p class="text-[11px] text-gray-500 dark:text-zinc-400">No team skills yet.</p>
    </div>

    <ul v-else class="divide-y divide-gray-100 dark:divide-zinc-800 border-y border-gray-100 dark:border-zinc-800">
      <li v-for="s in skills" :key="s.id" data-test="team-skill-item" class="py-3 flex items-start gap-3">
        <div class="flex-1 min-w-0 space-y-1">
          <p class="flex flex-wrap items-baseline gap-x-2">
            <span data-test="team-skill-name" class="text-xs font-bold font-mono break-all" :class="s.on && !s.clash ? 'text-gray-800 dark:text-zinc-100' : 'text-gray-400 dark:text-zinc-500'">{{ s.name }}</span>
            <span data-test="team-skill-workspace" class="text-[10px] text-gray-400 dark:text-zinc-500">{{ s.teamWorkspaceName }}</span>
          </p>
          <p v-if="s.description" class="text-[11px] leading-snug text-gray-500 dark:text-zinc-400 break-words">{{ s.description }}</p>
          <div v-if="s.clash" data-test="team-skill-clash" class="flex flex-wrap items-center gap-2 text-[11px] text-amber-700 dark:text-amber-300">
            <span>Your own skill <span class="font-mono font-semibold">{{ s.name }}</span> is used instead.</span>
            <button type="button" data-test="team-skill-use-team" :disabled="pending.has(s.id)" @click="useTeams(s)"
                    class="px-2.5 py-1 rounded-lg border border-amber-300 dark:border-amber-500/40 text-[9px] font-black uppercase tracking-widest hover:bg-amber-50 dark:hover:bg-amber-500/10 disabled:opacity-40">
              Use the team's instead
            </button>
          </div>
        </div>
        <button type="button" role="switch" data-test="team-skill-switch" :aria-checked="s.on"
                :aria-label="`On in this workspace: ${s.name}`" :disabled="pending.has(s.id)" @click="toggle(s)"
                class="relative shrink-0 block w-11 h-6 rounded-full border transition-colors disabled:opacity-50"
                :class="s.on ? 'bg-gray-900 dark:bg-white border-gray-900 dark:border-white' : 'bg-gray-200 dark:bg-zinc-700 border-gray-300 dark:border-zinc-600'">
          <span class="absolute top-[3px] w-4 h-4 rounded-full transition-all"
                :class="s.on ? 'left-[23px] bg-white dark:bg-zinc-900' : 'left-[3px] bg-white dark:bg-zinc-400'"></span>
        </button>
      </li>
    </ul>
  </section>
</template>

<script setup>
import { ref } from 'vue';
import { setWorkspaceSkillEnabled, setWorkspaceTeamSkill } from '../api';
import { useToasts } from '../composables/useToasts';

const props = defineProps({
  workspaceId: { type: String, required: true },
  // The team skills the workspace reaches, as listWorkspaceTeamSkills answers.
  skills: { type: Array, default: () => [] },
});
// `update`: one skill as the server now has it. `changed`: both lists are to
// be read again, since your own skill's switch moved too.
const emit = defineEmits(['update', 'changed']);

const { notifySuccess, notifyError } = useToasts();
// Skills whose answer is still out, by id.
const pending = ref(new Set());

async function busy(id, work) {
  pending.value = new Set(pending.value).add(id);
  try {
    await work();
  } catch (err) {
    notifyError(err.message);
  } finally {
    const next = new Set(pending.value);
    next.delete(id);
    pending.value = next;
  }
}

function toggle(s) {
  const on = !s.on;
  return busy(s.id, async () => {
    const res = await setWorkspaceTeamSkill(props.workspaceId, s.id, on);
    emit('update', { ...s, ...(res?.skill || {}), on: res?.skill?.on ?? on });
    notifySuccess(on ? `Turned ${s.name} on in this workspace` : `Turned ${s.name} off in this workspace`);
  });
}

// Your own skill of the name, off in this workspace: the team's then wins.
function useTeams(s) {
  return busy(s.id, async () => {
    await setWorkspaceSkillEnabled(props.workspaceId, s.name, false);
    notifySuccess(`This workspace now uses the team's ${s.name}`);
    emit('changed');
  });
}
</script>
