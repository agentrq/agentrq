<template>
  <div class="flex-1 overflow-y-auto px-4 pb-10 custom-scrollbar">
    <div class="max-w-4xl mx-auto pt-8">

      <div class="flex items-start justify-between gap-4 mb-1">
        <h1 class="text-2xl font-black text-gray-900 dark:text-white">Extensions</h1>
        <div class="flex items-center gap-2 shrink-0">
          <button type="button" @click="chooseFolder()" :disabled="busy"
                  class="px-4 py-2 rounded-sm border border-gray-200 dark:border-zinc-800 text-[10px] font-black uppercase tracking-widest text-gray-700 dark:text-zinc-300 hover:bg-gray-50 dark:hover:bg-zinc-800 disabled:opacity-40 transition-colors">
            Install from folder
          </button>
          <button type="button" @click="refresh()" :disabled="busy"
                  class="px-4 py-2 rounded-sm bg-black dark:bg-white text-white dark:text-black text-[10px] font-black uppercase tracking-widest hover:opacity-80 disabled:opacity-40 transition-all">
            {{ loading ? 'Checking…' : 'Check GitHub' }}
          </button>
        </div>
      </div>

      <p class="text-[11px] text-gray-500 dark:text-zinc-400 mb-6">
        {{ summary }} ·
        <span class="text-gray-400 dark:text-zinc-500">
          Published by anyone on GitHub under the <code class="font-mono">agentrq-extension</code> topic.
        </span>
      </p>

      <div v-if="error"
           class="mb-6 px-3 py-2 rounded-sm border border-amber-200 dark:border-amber-900/60 bg-amber-50 dark:bg-amber-950/30 text-[11px] text-amber-800 dark:text-amber-300">
        {{ error }}
      </div>

      <div v-else-if="notice"
           class="mb-6 px-3 py-2 rounded-sm border border-gray-200 dark:border-zinc-800 bg-gray-50 dark:bg-zinc-900 text-[11px] text-gray-700 dark:text-zinc-300">
        {{ notice }}
      </div>

      <!-- Only when something installed actually wants it. An app asking for a
           credential that reaches every workspace, unprompted and before
           anything needs it, is asking for more than it can justify. -->
      <div v-if="needsAuthorization"
           class="mb-6 px-3 py-2.5 rounded-sm border border-amber-200 dark:border-amber-900/60 bg-amber-50 dark:bg-amber-950/30 flex items-start justify-between gap-4">
        <p class="text-[11px] text-amber-800 dark:text-amber-300 leading-snug">
          An extension here was granted <strong class="font-semibold">all workspaces</strong>, and AgentRQ needs
          your authorisation before it can use account-wide tools on its behalf. A window will open on this
          server's own sign-in page.
        </p>
        <button type="button" @click="authorize()" :disabled="busy"
                class="shrink-0 px-4 py-2 rounded-sm bg-black dark:bg-white text-white dark:text-black text-[10px] font-black uppercase tracking-widest hover:opacity-80 disabled:opacity-40 transition-all">
          Authorise
        </button>
      </div>

      <p v-else-if="authorized" class="mb-6 text-[11px] text-gray-400 dark:text-zinc-500">
        AgentRQ is authorised to use account-wide tools.
        <button type="button" @click="deauthorize()" :disabled="busy"
                class="font-bold uppercase tracking-wider text-gray-500 dark:text-zinc-400 hover:text-black dark:hover:text-white disabled:opacity-40 transition-colors">
          Give it back
        </button>
      </p>

      <div v-if="sections.length === 0 && !loading"
           class="border border-dashed border-gray-200 dark:border-zinc-800 rounded-sm px-6 py-10 text-center">
        <p class="text-[11px] text-gray-500 dark:text-zinc-400">
          Nothing installed, and nothing found yet.
          <strong class="font-semibold text-gray-700 dark:text-zinc-300">Check GitHub</strong> to search,
          or <strong class="font-semibold text-gray-700 dark:text-zinc-300">Install from folder</strong>
          to add one you have on this machine.
        </p>
      </div>

      <section v-for="section in sections" :key="section.group" class="mb-8">
        <h2 class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 mb-3">
          {{ GROUP_LABELS[section.group] }}
        </h2>

        <div class="flex flex-col gap-2">
          <article v-for="row in section.rows" :key="row.fullName"
                   class="border border-gray-200 dark:border-zinc-800 rounded-sm px-4 py-3 bg-white dark:bg-zinc-900">

            <div class="flex items-baseline justify-between gap-3 min-w-0">
              <div class="min-w-0">
                <h3 class="text-sm font-black text-gray-900 dark:text-white truncate">
                  {{ row.manifest?.displayName || row.manifest?.name || row.name }}
                </h3>
                <!-- The owner leads. Installing is a decision about whether to
                     trust a person, not a comparison of feature lists. -->
                <p class="text-[10px] font-bold uppercase tracking-wider text-gray-500 dark:text-zinc-400 mt-0.5">
                  {{ row.owner }}
                </p>
              </div>
              <div class="flex items-center gap-2 shrink-0 text-[10px] text-gray-400 dark:text-zinc-500 font-mono">
                <span v-if="row.manifest?.license">{{ row.manifest.license }}</span>
                <span>★ {{ row.stars ?? 0 }}</span>
              </div>
            </div>

            <p v-if="row.description" class="text-[11px] text-gray-600 dark:text-zinc-400 mt-2 leading-snug">
              {{ row.description }}
            </p>

            <!-- Why it cannot be used, stated rather than implied by its absence. -->
            <p v-if="row.blocked"
               class="text-[11px] text-amber-700 dark:text-amber-400 mt-2 leading-snug">
              {{ row.blocked }}
            </p>

            <!-- Running and enabled are different states, and the row says
                 which: an enabled extension that threw on load does nothing,
                 and "enabled" alone would leave that unexplained. -->
            <p v-if="row.installed" class="text-[10px] font-bold uppercase tracking-wider mt-2"
               :class="row.installation.loaded
                 ? 'text-gray-500 dark:text-zinc-400'
                 : 'text-amber-700 dark:text-amber-400'">
              {{ row.status }}
            </p>

            <div class="flex items-center gap-3 mt-3">
              <template v-if="row.installed">
                <button type="button" @click="setEnabled(row.name, !row.installation.enabled)" :disabled="busy"
                        class="text-[10px] font-bold uppercase tracking-wider text-gray-500 dark:text-zinc-400 hover:text-black dark:hover:text-white disabled:opacity-40 transition-colors">
                  {{ row.installation.enabled ? 'Disable' : 'Enable' }}
                </button>
                <button type="button" @click="uninstall(row.name)" :disabled="busy"
                        class="text-[10px] font-bold uppercase tracking-wider text-red-600 dark:text-red-400 hover:opacity-70 disabled:opacity-40 transition-opacity">
                  Uninstall
                </button>
                <!-- Said out loud: a linked folder can change underneath the
                     app, unlike anything else that gets installed. -->
                <span v-if="row.installation.linked" class="text-[10px] text-gray-400 dark:text-zinc-600 font-mono">
                  linked folder
                </span>
              </template>
              <template v-else>
                <a :href="row.url" target="_blank" rel="noopener noreferrer"
                   class="text-[10px] font-bold uppercase tracking-wider text-gray-500 dark:text-zinc-400 hover:text-black dark:hover:text-white transition-colors">
                  View on GitHub
                </a>
                <span v-if="row.pushedAt" class="text-[10px] text-gray-400 dark:text-zinc-600 font-mono">
                  updated {{ shortDate(row.pushedAt) }}
                </span>
              </template>
            </div>
          </article>
        </div>
      </section>

    </div>

    <ExtensionGrantDialog v-if="candidate"
                          :candidate="candidate"
                          :workspaces="workspaceStore.workspaces ?? []"
                          @cancel="cancelInstall"
                          @confirm="install" />
  </div>
</template>

<script setup>
/**
 * Browsing what exists.
 *
 * Desktop only, and it lives here in `frontend/src/desktop/` rather than under
 * `desktop/` because Tailwind scans from the build root: a class used only in a
 * file outside this tree is silently dropped from the stylesheet, and the page
 * renders unstyled with no error to explain it.
 *
 * What this has to get right is that every row explains itself. A broken
 * manifest shows its parse failure; an incompatible one shows the version it
 * wants against the version running; an installed one says whether it is
 * actually running, which is not the same as being enabled.
 */
import { computed, onMounted } from 'vue';

import { useExtensionCatalogue } from '../composables/useExtensionCatalogue';
import { useWorkspaceStore } from '../stores/workspaceStore';
import ExtensionGrantDialog from './ExtensionGrantDialog.vue';

const GROUP_LABELS = {
  installed: 'Installed',
  available: 'Available',
  unavailable: 'Unavailable',
};

const workspaceStore = useWorkspaceStore();

const {
  authorized,
  needsAuthorization,
  authorize,
  deauthorize,
  sections,
  summary,
  loading,
  error,
  notice,
  candidate,
  step,
  load,
  refresh,
  chooseFolder,
  install,
  cancelInstall,
  uninstall,
  setEnabled,
} = useExtensionCatalogue();

// One flag for every control, so nothing can be pressed twice while a folder is
// being read or an install is being written.
const busy = computed(() => loading.value || step.value !== 'idle');

// Reads the cache. Deliberately not a search: GitHub answers ten of those a
// minute, and spending that on somebody opening a screen would leave nothing
// for the person who actually pressed the button.
onMounted(load);

/** `2026-09-10T…` → `10 Sep`, which is all a row has room for. */
function shortDate(iso) {
  const date = new Date(iso);
  return Number.isNaN(date.getTime())
    ? ''
    : date.toLocaleDateString(undefined, { day: 'numeric', month: 'short' });
}
</script>
