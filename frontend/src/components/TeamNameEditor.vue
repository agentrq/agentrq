<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- A team's name, renamed in place: click it, type, Enter saves and Escape
     (or leaving the field) puts it back. Any member may rename the team. The
     rename names the version of the team it was made against, so one made
     over someone else's change is refused, and the team is read again. -->
<template>
  <form v-if="editing" class="inline-flex min-w-0 max-w-full" data-test="team-rename-form" @submit.prevent="save">
    <input ref="input" v-model="draft" type="text" maxlength="128" data-test="team-rename-input" aria-label="Team name"
           :disabled="saving" @keydown.esc.prevent.stop="cancel" @blur="onBlur"
           :class="inputClass"
           class="min-w-0 w-full bg-white dark:bg-zinc-900 border border-gray-300 dark:border-zinc-600 rounded px-1 outline-none focus:border-gray-900 dark:focus:border-white text-gray-900 dark:text-white" />
  </form>
  <button v-else type="button" data-test="team-rename" :title="`Rename ${team?.name || 'the team'}`" :aria-label="`Rename ${team?.name || 'the team'}`"
          :disabled="!team" @click.stop="edit"
          class="min-w-0 truncate text-left rounded hover:underline decoration-dotted underline-offset-2 cursor-text disabled:cursor-default">{{ team?.name || fallback }}</button>
</template>

<script setup>
import { nextTick, ref } from 'vue';
import { renameTeam } from '../api';
import { useTeamStore } from '../stores/teamStore';
import { useToasts } from '../composables/useToasts';
import { useTeamMode } from '../composables/useTeamMode';

const props = defineProps({
  // { id, name, version }
  team: { type: Object, default: null },
  fallback: { type: String, default: 'Team' },
  inputClass: { type: String, default: 'text-xs h-6' },
});

const store = useTeamStore();
const { notifySuccess } = useToasts();
const { changeFailed } = useTeamMode();

const editing = ref(false);
const saving = ref(false);
const draft = ref('');
const input = ref(null);

async function edit() {
  draft.value = props.team.name;
  editing.value = true;
  await nextTick();
  input.value?.focus();
  input.value?.select();
}

function cancel() {
  editing.value = false;
}

// Leaving the field puts the name back, unless it is the save itself.
function onBlur() {
  if (!saving.value) cancel();
}

async function save() {
  const name = draft.value.trim();
  if (!name || name === props.team.name) return cancel();
  saving.value = true;
  try {
    store.upsert(await renameTeam(props.team.id, name, props.team.version));
    notifySuccess('Team renamed');
  } catch (err) {
    await changeFailed(err, String(props.team.id));
  } finally {
    saving.value = false;
    editing.value = false;
  }
}
</script>
