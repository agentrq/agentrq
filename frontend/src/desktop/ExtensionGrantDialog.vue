<template>
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 px-4" @click.self="$emit('cancel')">
    <div class="w-full max-w-lg bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl shadow-sm max-h-[85vh] overflow-y-auto custom-scrollbar">

      <div class="px-5 pt-5 pb-4 border-b border-gray-100 dark:border-zinc-800">
        <h2 class="text-lg font-black text-gray-900 dark:text-white">
          Install {{ candidate.manifest.displayName || candidate.manifest.name }}?
        </h2>
        <!-- Where it is about to come from. A folder shows its path; a
             catalogue entry shows the repository, because "who published this"
             is the question being answered by pressing Install. -->
        <p class="text-[11px] text-gray-500 dark:text-zinc-400 mt-1 font-mono truncate">
          {{ candidate.path || candidate.fullName }}
        </p>

        <!-- The sentence that is true on every install, whatever else is on the
             screen. It carries the whole weight when the permission list below
             is empty, and an absent list reads as safety. -->
        <p class="mt-3 text-[11px] leading-relaxed text-gray-700 dark:text-zinc-300 border border-amber-200 dark:border-amber-900/60 bg-amber-50 dark:bg-amber-950/30 rounded-sm px-3 py-2">
          {{ ask.machineAccess }}
          <span v-if="ask.networkClaim" class="text-gray-500 dark:text-zinc-400">{{ ask.networkClaim }}</span>
        </p>

        <dl class="mt-3 flex flex-wrap gap-x-5 gap-y-1 text-[10px] font-bold uppercase tracking-wider text-gray-500 dark:text-zinc-400">
          <div><dt class="inline">Version</dt> <dd class="inline font-mono normal-case text-gray-700 dark:text-zinc-300">{{ candidate.manifest.version }}</dd></div>
          <div><dt class="inline">Licence</dt> <dd class="inline font-mono normal-case text-gray-700 dark:text-zinc-300">{{ candidate.manifest.license }}</dd></div>
        </dl>
      </div>

      <div class="px-5 py-4 flex flex-col gap-4">

        <!-- Shown verbatim. A summary of what an extension may do is a
             paraphrase of a permission, and this is not the place for one. -->
        <section v-if="ask.workspace.length > 0">
          <h3 class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 mb-1.5">
            Workspace tools it may call
          </h3>
          <p class="text-[11px] font-mono text-gray-700 dark:text-zinc-300 leading-relaxed">{{ ask.workspace.join(', ') }}</p>
        </section>

        <section v-if="ask.supervisor.length > 0">
          <h3 class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 mb-1.5">
            Account tools it may call
          </h3>
          <p class="text-[11px] font-mono text-gray-700 dark:text-zinc-300 leading-relaxed">{{ ask.supervisor.join(', ') }}</p>
        </section>

        <section v-if="scopes.length > 0">
          <h3 class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 mb-2">
            What it may reach
          </h3>
          <div class="flex flex-col gap-1.5">
            <label v-for="option in scopes" :key="option"
                   class="flex items-start gap-2.5 px-3 py-2 rounded-sm border cursor-pointer transition-colors"
                   :class="scope === option
                     ? 'border-gray-900 dark:border-white bg-gray-50 dark:bg-zinc-800'
                     : 'border-gray-200 dark:border-zinc-800 hover:border-gray-300 dark:hover:border-zinc-700'">
              <input type="radio" :value="option" v-model="scope" class="mt-0.5 accent-black dark:accent-white" />
              <span class="text-[11px] text-gray-800 dark:text-zinc-200 leading-snug">{{ scopeLabel(option) }}</span>
            </label>
          </div>

          <div v-if="scope === SCOPE.selected" class="mt-2 flex flex-col gap-1 max-h-40 overflow-y-auto custom-scrollbar">
            <label v-for="workspace in workspaces" :key="workspace.id"
                   class="flex items-center gap-2 text-[11px] text-gray-700 dark:text-zinc-300 px-1">
              <input type="checkbox" :value="workspace.id" v-model="selected" class="accent-black dark:accent-white" />
              {{ workspace.name }}
            </label>
          </div>
        </section>

        <section v-if="fields.length > 0">
          <h3 class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 mb-2">
            Settings
          </h3>
          <div class="flex flex-col gap-2">
            <label v-for="field in fields" :key="field.key" class="flex flex-col gap-1">
              <span class="text-[10px] font-bold uppercase tracking-wider text-gray-500 dark:text-zinc-400">
                {{ field.label }}
                <span v-if="field.type === 'secret'" class="normal-case font-medium text-gray-400 dark:text-zinc-500">
                  — stored in your keychain, never shown again
                </span>
              </span>
              <input v-model="values[field.key]"
                     :type="field.type === 'secret' ? 'password' : field.type === 'number' ? 'number' : 'text'"
                     class="px-2.5 py-1.5 text-[12px] rounded-sm border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-950 text-gray-900 dark:text-white focus:outline-none focus:border-gray-400 dark:focus:border-zinc-600" />
            </label>
          </div>
        </section>
      </div>

      <div class="px-5 py-4 border-t border-gray-100 dark:border-zinc-800 flex items-center justify-between gap-3">
        <p class="text-[11px] text-amber-700 dark:text-amber-400">{{ problem }}</p>
        <div class="flex items-center gap-2 shrink-0">
          <button type="button" @click="$emit('cancel')"
                  class="px-4 py-2 rounded-lg border border-gray-200 dark:border-zinc-800 text-[11px] font-black uppercase tracking-widest text-gray-700 dark:text-zinc-300 hover:bg-gray-50 dark:hover:bg-zinc-800 transition-colors">
            Cancel
          </button>
          <button type="button" @click="confirm" :disabled="!valid"
                  class="px-4 py-2 rounded-lg bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest hover:opacity-80 disabled:opacity-40 transition-all">
            Install
          </button>
        </div>
      </div>

    </div>
  </div>
</template>

<script setup>
/**
 * The install question, and the two conversations on it.
 *
 * **The author conversation is constant.** Every extension runs with full access
 * to the computer — there is no sandbox, by design — so that sentence is on this
 * screen every time, above everything else, and it does not move or shrink.
 *
 * **The permission conversation scales with the ask.** An extension wanting
 * workspace tools shows them and the workspace they apply to; one wanting the
 * account shows that it reaches every workspace, including ones created later.
 *
 * The dialog is only shown at all when there *is* a permission to discuss — an
 * extension asking for nothing installs without it, because a permission screen
 * with nothing on it is how people learn to click past the one that matters.
 *
 * ## Why the network line is not in the permission list
 *
 * `net` is the author saying what their extension contacts. Nothing enforces it
 * and nothing can. Putting it under a permission heading would make a
 * description read as a boundary somebody is holding, so it sits with the
 * machine-access sentence, in the author's voice.
 */
import { reactive } from 'vue';

import { SCOPE, useExtensionGrant, scopeLabel as labelFor } from '../composables/useExtensionGrant';

const props = defineProps({
  candidate: { type: Object, required: true },
  workspaces: { type: Array, default: () => [] },
  workspaceId: { type: String, default: '' },
});

const emit = defineEmits(['cancel', 'confirm']);

const { ask, scopes, scope, selected, valid, problem, grant } = useExtensionGrant({
  manifest: props.candidate.manifest,
  workspaceId: props.workspaceId,
  workspaces: props.workspaces,
});

const fields = props.candidate.manifest.config ?? [];
// Started empty rather than pre-filled: a default typed into a secret field is
// a credential somebody did not choose.
const values = reactive(Object.fromEntries(fields.map((field) => [field.key, ''])));

function scopeLabel(option) {
  return labelFor(option, { workspaceName: 'This workspace' });
}

function confirm() {
  if (!valid.value) return;
  // Only the fields somebody actually filled in. Sending an empty secret would
  // clear one that is already stored, which is not what leaving a box alone
  // means.
  const config = Object.fromEntries(Object.entries(values).filter(([, value]) => String(value).trim() !== ''));
  emit('confirm', {
    grant: ask.value.level === 'none' ? null : grant.value,
    config: Object.keys(config).length > 0 ? config : null,
  });
}
</script>
