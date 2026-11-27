<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<!-- Importing skills from GitHub or GitLab, for your account and for a team
     workspace alike: Discover lists a link's skills, Import takes those left
     on. `run(url, overwrite, skills, list)` asks the server; what the import
     asks besides overwriting, such as the workspaces to turn the skills on in,
     comes in the options slot. -->
<template>
  <!-- Boxed from sm up; on a phone a box in a box leaves too little room. -->
  <form novalidate class="sm:p-4 sm:bg-gray-50 sm:dark:bg-zinc-800/50 sm:rounded-sm sm:border border-gray-100 dark:border-zinc-800 space-y-3"
        @submit.prevent="discover">
    <FormField :id="`${testPrefix}-import-url`" label="Import from GitHub or GitLab" :error="urlError"
               description="A repository, or one folder of it. Its skills are listed for you to choose from.">
      <template #default="f">
        <div class="flex gap-2">
          <input :id="f.id" ref="urlInput" v-model="url" :data-test="`${testPrefix}-import-url`" type="url" autocomplete="off" spellcheck="false"
                 placeholder="https://github.com/obra/superpowers" :aria-describedby="f.describedBy" :aria-invalid="f.invalid || undefined"
                 :class="fieldClass(f.invalid, { size: 'compact' })" class="min-w-0 flex-1 font-mono" />
          <button type="submit" :data-test="`${testPrefix}-import`" :disabled="importing" :class="COMPACT_PRIMARY_BUTTON" class="shrink-0">
            {{ importing && !choice ? 'Discovering…' : 'Discover' }}
          </button>
        </div>
      </template>
    </FormField>
    <!-- A private repository is read through the importing person's own connections. -->
    <GitHubConnection />
    <GitLabConnection />
    <!-- The repository's skills, to switch off those not to import. -->
    <SkillImportChoice v-if="choice" v-model="chosen" :choice="choice" :importing="importing" :test-prefix="testPrefix"
                       @import="importChosen" @cancel="choice = null">
      <template #options>
        <slot name="options" />
        <label class="flex items-center gap-2 text-[11px] text-gray-600 dark:text-zinc-300">
          <input v-model="overwrite" :data-test="`${testPrefix}-import-overwrite`" type="checkbox" class="rounded-sm border-gray-300 dark:border-zinc-600" />
          {{ overwriteLabel }}
        </label>
      </template>
    </SkillImportChoice>
    <p v-if="importError" role="alert" :data-test="`${testPrefix}-import-error`" :class="ERROR" class="break-words">{{ importError }}</p>
    <div v-if="report" role="status" :data-test="`${testPrefix}-import-report`" class="space-y-2 text-[11px] text-gray-700 dark:text-zinc-300">
      <p class="font-bold">
        Imported {{ report.imported.length }} {{ report.imported.length === 1 ? 'skill' : 'skills' }}
        from {{ report.sourceRepo }}<span v-if="report.sourceCommit">@{{ report.sourceCommit.slice(0, 7) }}</span>.
      </p>
      <div v-if="report.skipped.length">
        <p class="text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Skipped ({{ report.skipped.length }})</p>
        <ul class="mt-1 space-y-1">
          <li v-for="(s, i) in report.skipped" :key="i" class="break-words">
            <span class="font-mono font-bold">{{ s.path || s.name }}</span> — {{ s.reason }}
          </li>
        </ul>
      </div>
    </div>
  </form>
</template>

<script setup>
import { nextTick, ref, watch } from 'vue';

import { choosablePaths, chosenImportPaths, importUrlError, orderCandidates } from '../composables/useSkills';
import { useGitLabHost } from '../composables/useGitLabHost';
import { COMPACT_PRIMARY_BUTTON, ERROR, fieldClass } from '../utils/formStyles';
import FormField from './FormField.vue';
import GitHubConnection from './GitHubConnection.vue';
import GitLabConnection from './GitLabConnection.vue';
import SkillImportChoice from './SkillImportChoice.vue';

const props = defineProps({
  run: { type: Function, required: true },
  overwriteLabel: { type: String, default: 'Overwrite skills you already have' },
  testPrefix: { type: String, default: 'skill' },
});
const emit = defineEmits(['imported']);

const url = ref('');
const urlInput = ref(null);
const overwrite = ref(false);
const importing = ref(false);
// What is wrong with the link, under its field; what went wrong importing.
const urlError = ref('');
const importError = ref('');
const report = ref(null);
// The listed skills: the link they came from, and those switched on — all
// that can be, unless the repository is too large to import whole.
const choice = ref(null);
const chosen = ref([]);
// The GitLab a link may name besides GitHub.
const gitlabHost = useGitLabHost();

// A list belongs to the link it came from: a new link puts it away.
watch(url, () => {
  urlError.value = '';
  choice.value = null;
});

async function request(link, skills, list) {
  importing.value = true;
  importError.value = '';
  report.value = null;
  try {
    const res = await props.run(link, overwrite.value, skills, list);
    if (res?.candidates?.length) {
      const candidates = orderCandidates(res.candidates);
      choice.value = { url: link, repo: res.sourceRepo, candidates, tooLarge: !!res.tooLarge };
      chosen.value = res.tooLarge ? [] : choosablePaths(candidates);
      return;
    }
    choice.value = null;
    report.value = { imported: res?.imported || [], skipped: res?.skipped || [], sourceRepo: res?.sourceRepo || '', sourceCommit: res?.sourceCommit || '' };
    emit('imported');
  } catch (err) {
    // A link refused is said under its field; an import that failed, below.
    if (list) {
      urlError.value = err.message;
      await focusUrl();
    } else {
      importError.value = err.message;
    }
  } finally {
    importing.value = false;
  }
}

async function focusUrl() {
  await nextTick();
  urlInput.value.focus();
}

function discover() {
  const link = url.value.trim();
  urlError.value = importUrlError(link, gitlabHost.value);
  if (urlError.value) return focusUrl();
  choice.value = null;
  return request(link, undefined, true);
}

function importChosen() {
  return request(choice.value.url, chosenImportPaths(choice.value, chosen.value), false);
}
</script>
