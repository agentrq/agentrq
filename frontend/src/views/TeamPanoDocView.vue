<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<!-- A Pano doc a teammate shared with the team, read-only: its markdown, files
     and attachments, and whose it is. No editing, comments or tasks, which
     stay the owner's. -->
<template>
  <div class="@container flex flex-col h-full w-full overflow-y-auto custom-scrollbar">
    <div class="w-full px-4 py-2 mb-4 shrink-0">
      <router-link :to="link('/pano')" data-test="team-pano-back" class="text-[10px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400 hover:text-gray-800 dark:hover:text-zinc-100">← Pano</router-link>
    </div>

    <div class="px-4 pb-10 min-w-0">
      <p v-if="error && !doc" data-test="team-pano-error" class="text-[11px] font-bold text-red-600 dark:text-red-400">{{ error }}</p>
      <p v-else-if="!doc" class="text-[11px] text-gray-400 dark:text-zinc-500">Loading…</p>

      <div v-else class="grid gap-8 @min-[64rem]:grid-cols-[minmax(0,1fr)_320px] items-start">
        <article :class="[paper, color.card, color.body]" :style="{ '--pano-ink': color.inkHex, '--pano-tag': color.tagHex, '--pano-tag-ink': color.tagInkHex }" class="mt-3 px-6 @min-[40rem]:px-10 pt-10 pb-8 min-w-0">
          <span aria-hidden="true" :class="color.tape" class="absolute -top-3 left-1/2 -translate-x-1/2 -rotate-2 w-24 h-6 rounded-[2px]"></span>
          <h1 data-test="team-pano-title" :class="color.title" class="text-2xl @min-[48rem]:text-3xl font-black leading-tight break-words">{{ doc.title }}</h1>
          <p data-test="team-pano-meta" :class="color.muted" class="mt-1 flex items-center gap-1.5 text-[11px] min-w-0">
            <OwnerAvatar :owner="owner" size="md" />
            <span class="truncate"><span class="font-bold">{{ owner.name }}</span> · updated {{ panoDate(doc.updatedAt) }}</span>
          </p>
          <PanoProgress :doc="doc" class="mt-4 max-w-sm" />
          <div class="my-5 border-t border-black/10"></div>
          <div ref="bodyEl" data-test="team-pano-body" class="md-body text-[14px] break-words [&_img]:max-w-full [&_img]:rounded" v-html="body"></div>
        </article>

        <aside class="space-y-6 min-w-0">
          <dl class="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1.5 text-[12px]">
            <dt class="text-gray-400 dark:text-zinc-500">Id</dt><dd class="font-mono text-gray-700 dark:text-zinc-300 truncate">{{ doc.id }}</dd>
            <dt class="text-gray-400 dark:text-zinc-500">Expires</dt><dd class="text-gray-700 dark:text-zinc-300">{{ expiresLabel(doc.expiresAt) }}</dd>
          </dl>

          <section v-for="list in fileLists" :key="list.title" :data-test="list.test" class="space-y-2">
            <h3 class="text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest">{{ list.title }}</h3>
            <ul class="space-y-1">
              <li v-for="f in list.files" :key="f.id" class="flex items-center gap-2 text-[12px] min-w-0">
                <a :href="f.url" target="_blank" rel="noopener" class="min-w-0 truncate font-mono text-gray-700 dark:text-zinc-300 underline decoration-dotted underline-offset-2">{{ f.filename }}</a>
                <span class="shrink-0 text-[10px] text-gray-400 dark:text-zinc-500">{{ formatFileSize(f.size) }}</span>
              </li>
            </ul>
          </section>
        </aside>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { getTeamPanoDoc, teamFileUrl } from '../api';
import { expiresLabel, formatFileSize, linkPanoFiles, panoAssignees, panoColor, panoDate } from '../composables/usePano';
import { renderMarkdown } from '../utils/markdown';
import { usePanoA2ui } from '../composables/usePanoA2ui';
import { lostTeam, useTeamMode } from '../composables/useTeamMode';
import { useToasts } from '../composables/useToasts';
import { onWebMCPChange } from '../composables/useWebMCPChanges';
import OwnerAvatar from '../components/OwnerAvatar.vue';
import PanoProgress from '../components/PanoProgress.vue';

const route = useRoute();
const router = useRouter();
const { teamId, link } = useTeamMode();
const { notifyInfo } = useToasts();

const docId = String(route.params.docId);
const doc = ref(null);
const error = ref('');

// A file's link is its team route, as the server gave it, under the app's base path.
const withTeamUrls = (files = []) => files.map((f) => ({ ...f, url: teamFileUrl(f) }));
const files = computed(() => withTeamUrls(doc.value.files));
const attachments = computed(() => withTeamUrls(doc.value.attachments));
const fileLists = computed(() => [
  { title: 'Linked files', test: 'team-pano-files', files: files.value },
  { title: 'Attachments', test: 'team-pano-attachments', files: attachments.value },
].filter((l) => l.files.length));

// An assigned item's task is the owner's: its status shows, with no link to it.
const body = computed(() => panoAssignees(linkPanoFiles(renderMarkdown(doc.value.markdown), files.value), { link: false }));
const color = computed(() => panoColor(doc.value.color));
const bodyEl = ref(null);
usePanoA2ui(() => bodyEl.value, () => ({ html: doc.value ? body.value : '', files: doc.value ? files.value : [], color: doc.value ? color.value : panoColor() }));
const owner = computed(() => ({ userId: doc.value.ownerId, name: doc.value.ownerName || 'A teammate', picture: doc.value.ownerPicture }));

async function load() {
  try {
    doc.value = (await getTeamPanoDoc(teamId.value, docId)).doc;
    error.value = '';
  } catch (err) {
    // No longer shared, or you are no longer in the team: the team's Pano
    // says which, by listing what is left or going back to Me.
    if (lostTeam(err)) {
      notifyInfo('That doc is no longer shared with the team');
      router.replace(link('/pano'));
    } else {
      error.value = err.message;
    }
  }
}

onMounted(load);
onWebMCPChange(load);

const paper = 'pano-paper relative rounded-md border border-black/[0.06] shadow-[0_1px_2px_rgba(0,0,0,0.06),0_16px_32px_-16px_rgba(0,0,0,0.3)] dark:shadow-[0_2px_4px_rgba(0,0,0,0.3),0_20px_40px_-16px_rgba(0,0,0,0.45)]';
</script>
