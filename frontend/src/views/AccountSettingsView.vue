<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<!-- The account's own settings, which hold in every workspace, laid out like a
     workspace's: the clock times are shown with, the presets every workspace
     offers, the GitHub and GitLab accounts skills are imported from (a tab
     only when the admin set up either), and who the account is, with the
     download of all its data. The tab is the route's ?tab=, so a link opens
     one. Each change applies at once; there is no Save. -->
<template>
  <div class="flex flex-col h-full w-full min-w-0 bg-transparent overflow-y-auto custom-scrollbar" data-test="account-settings">
    <div class="w-full px-4 py-2 mb-6 shrink-0 flex flex-col min-w-0">
      <h1 class="text-lg md:text-2xl font-black text-gray-800 dark:text-zinc-200 leading-tight">Settings</h1>
      <p class="text-xs text-gray-500 dark:text-zinc-400 mt-0.5">Account settings · these hold in every workspace</p>
    </div>

    <div class="flex-1 px-4 pb-20 min-w-0">
      <div class="w-full min-w-0 flex flex-col md:flex-row gap-8">
        <div class="w-full md:w-48 shrink-0">
          <nav class="flex flex-col gap-1 sticky top-0" role="tablist">
            <button v-for="t in TABS" :key="t.id" type="button" role="tab" :aria-selected="tab === t.id" :data-test="`settings-tab-${t.id}`" @click="select(t.id)"
                    :class="tab === t.id ? 'bg-gray-900 text-white dark:bg-white dark:text-zinc-900 shadow-lg shadow-black/5' : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-100 dark:hover:bg-zinc-800'"
                    class="flex items-center gap-3 px-4 py-2.5 rounded-sm text-[10px] font-bold uppercase tracking-widest transition-all text-left">
              <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" :d="t.icon" /></svg>
              {{ t.label }}
            </button>
          </nav>
        </div>

        <div class="flex-1 min-w-0">
          <div class="bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-sm shadow-sm overflow-hidden min-h-[500px] p-4 sm:p-8">
            <div v-if="tab === 'general'" class="space-y-8 animate-in fade-in slide-in-from-bottom-2 duration-300">
              <fieldset data-test="time-format" class="space-y-2">
                <legend class="block text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest ml-1 mb-2">Time Format</legend>
                <SegmentedControl v-model="picked" :options="TIME_FORMAT_OPTIONS" class="ml-1" />
                <p class="text-[9px] text-gray-500 dark:text-zinc-500 font-bold uppercase tracking-wider ml-1 mt-2">
                  Every time in the app is shown this way. It is now <span data-test="time-format-example" class="text-gray-900 dark:text-zinc-100">{{ example }}</span>.
                </p>
              </fieldset>
            </div>

            <WorkspacePresetsPanel v-else-if="tab === 'presets'" />

            <!-- Each connection hides itself when the admin has not set it up. -->
            <div v-else-if="tab === 'connections'" data-test="settings-connections" class="space-y-6 animate-in fade-in slide-in-from-bottom-2 duration-300">
              <p class="text-[9px] text-gray-500 dark:text-zinc-500 font-bold uppercase tracking-wider ml-1">Private repositories skills are imported from, in every workspace.</p>
              <GitHubConnection />
              <GitLabConnection />
            </div>

            <div v-else-if="tab === 'profile'" data-test="settings-profile" class="space-y-8 animate-in fade-in slide-in-from-bottom-2 duration-300">
              <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
                <div class="space-y-2 min-w-0">
                  <label for="profile-name" class="block text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest ml-1">Name</label>
                  <input id="profile-name" data-test="profile-name" :value="user?.name || ''" type="text" readonly :class="READONLY" />
                </div>
                <div class="space-y-2 min-w-0">
                  <label for="profile-email" class="block text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest ml-1">Email</label>
                  <input id="profile-email" data-test="profile-email" :value="user?.email || ''" type="email" readonly :class="READONLY" />
                </div>
              </div>
              <p class="text-[9px] text-gray-500 dark:text-zinc-500 font-bold uppercase tracking-wider ml-1 -mt-4">As your sign-in provider gives them.</p>

              <section class="pt-6 border-t border-gray-100 dark:border-zinc-800/50 space-y-3">
                <h2 class="text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest ml-1">Your Data</h2>
                <p class="text-xs text-gray-500 dark:text-zinc-400 ml-1 max-w-prose">
                  A gzipped JSON file of everything your account holds: settings, presets, workspaces (archived ones too), memories, Pano docs, and every task with its conversation. Attachments and skill files are not included.
                </p>
                <button type="button" data-test="export-data" :disabled="exporting" @click="exportData"
                        class="ml-1 inline-flex items-center gap-2 px-4 py-2 rounded-lg border border-gray-200 dark:border-zinc-700 bg-white dark:bg-zinc-900 text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-100 hover:border-gray-900 dark:hover:border-white transition-colors disabled:opacity-50 disabled:cursor-not-allowed">
                  <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M4 16v2a2 2 0 002 2h12a2 2 0 002-2v-2M7 10l5 5m0 0l5-5m-5 5V4" /></svg>
                  {{ exporting ? 'Preparing…' : 'Export my data' }}
                </button>
              </section>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { exportAccountData, fetchUser, getGitHubConnection, getGitLabConnection } from '../api';
import GitHubConnection from '../components/GitHubConnection.vue';
import GitLabConnection from '../components/GitLabConnection.vue';
import SegmentedControl from '../components/SegmentedControl.vue';
import WorkspacePresetsPanel from '../components/WorkspacePresetsPanel.vue';
import { useToasts } from '../composables/useToasts';
import { onWebMCPChange } from '../composables/useWebMCPChanges';
import {
  TIME_FORMAT_12H, TIME_FORMAT_24H, TIME_FORMAT_BROWSER, loadTimeFormat, saveTimeFormat, timeFormat, withClock,
} from '../composables/useTimeFormat';

const ALL_TABS = [
  { id: 'general', label: 'General', icon: 'M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z' },
  { id: 'presets', label: 'Presets', icon: 'M7 8h10M7 12h4m1 8l-4-4H5a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v8a2 2 0 01-2 2h-3l-4 4z' },
  { id: 'connections', label: 'Connections', icon: 'M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101m-.758-4.899a4 4 0 005.656 0l4-4a4 4 0 00-5.656-5.656l-1.1 1.1' },
  { id: 'profile', label: 'Profile', icon: 'M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z' },
];

const READONLY = 'w-full bg-gray-100 dark:bg-zinc-800 border border-gray-200 dark:border-zinc-800 rounded-sm px-4 py-3 text-sm outline-none text-gray-700 dark:text-zinc-300 shadow-sm cursor-default';

// How long the saved file's address outlives the click: long enough for the
// browser to start reading it.
const REVOKE_DELAY_MS = 40_000;

const TIME_FORMAT_OPTIONS = [
  { value: TIME_FORMAT_BROWSER, label: 'Browser', test: 'time-format-browser' },
  { value: TIME_FORMAT_12H, label: '12h', test: 'time-format-12h' },
  { value: TIME_FORMAT_24H, label: '24h', test: 'time-format-24h' },
];

const route = useRoute();
const router = useRouter();
const { notifyError, notifySuccess } = useToasts();

// null until both are read. A connection that fails to read counts as not
// offered, as the components themselves take it.
const connectionsOffered = ref(null);
const TABS = computed(() => ALL_TABS.filter((t) => t.id !== 'connections' || connectionsOffered.value));

// ?tab=connections, as GitHub's and GitLab's returns come back, is kept while
// the connections are still being read, so the page does not flash General.
const tab = computed(() => {
  const asked = route.query.tab;
  if (TABS.value.some((t) => t.id === asked)) return asked;
  if (asked === 'connections' && connectionsOffered.value === null) return asked;
  return 'general';
});
const select = (id) => router.replace({ path: '/settings', query: { tab: id } });

// A pick is shown at once and saved; a refusal puts the last one back.
const picked = computed({
  get: () => timeFormat.value,
  set: (format) => saveTimeFormat(format).catch((e) => notifyError(e?.message || 'Failed to save account settings')),
});

// Read when drawn, so it follows the pick.
const example = computed(() => new Date().toLocaleTimeString('en-US', withClock({ hour: '2-digit', minute: '2-digit' })));

const user = ref(null);
const exporting = ref(false);

onMounted(async () => {
  const offered = (read) => read().then((c) => Boolean(c?.enabled), () => false);
  const [github, gitlab] = await Promise.all([offered(getGitHubConnection), offered(getGitLabConnection)]);
  connectionsOffered.value = github || gitlab;
});
onMounted(async () => {
  user.value = await fetchUser().catch(() => null);
});

// When the next export is allowed, on the account's clock: a weekday too,
// since it is often tomorrow.
const retryTime = (at) => at.toLocaleString('en-US', withClock({ weekday: 'short', hour: '2-digit', minute: '2-digit' }));

async function exportData() {
  exporting.value = true;
  try {
    const { blob, filename } = await exportAccountData();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(url), REVOKE_DELAY_MS);
    notifySuccess('Your data is downloading');
  } catch (e) {
    const message = e?.message || 'Failed to export your data';
    notifyError(e?.retryAt ? `${message}. Try again after ${retryTime(e.retryAt)}.` : message);
  } finally {
    exporting.value = false;
  }
}

// The clock is read once, at sign-in, and kept by every save; only a browser
// agent's change needs it read again.
onWebMCPChange(() => loadTimeFormat());
</script>
