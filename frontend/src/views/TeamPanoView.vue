<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- A team's Pano: the docs its members shared with it, each with its owner,
     read-only. The papers are the board's, without its write controls. -->
<template>
  <div class="@container flex-1 min-w-0 flex flex-col h-full overflow-y-auto custom-scrollbar">
    <div class="w-full px-4 py-2 mb-4 shrink-0 flex flex-col min-w-0">
      <h1 class="text-lg @min-[48rem]:text-2xl font-black text-gray-800 dark:text-zinc-200 truncate leading-tight">Pano</h1>
      <p class="text-xs text-gray-500 dark:text-zinc-400 mt-0.5">Docs your teammates shared with the team, to read.</p>
    </div>

    <div class="px-4 pb-10">
      <p v-if="loading" class="text-[11px] text-gray-400 dark:text-zinc-500">Loading the Pano…</p>
      <p v-else-if="!docs.length" data-test="team-pano-empty" class="text-[11px] text-gray-500 dark:text-zinc-400">
        Nothing shared yet. A member shares one of their docs from its page on their Pano.
      </p>

      <div v-else class="grid gap-x-5 gap-y-7 pt-2 grid-cols-1 @min-[33rem]:grid-cols-2 @min-[57rem]:grid-cols-3 @min-[73rem]:grid-cols-4 @min-[89rem]:grid-cols-5">
        <router-link v-for="doc in docs" :key="doc.id" :to="link({ path: `/team/pano/${doc.id}` })" data-test="team-pano-card"
          :style="{ '--pano-ink': panoColor(doc.color).inkHex, '--pano-tag': panoColor(doc.color).tagHex, '--pano-tag-ink': panoColor(doc.color).tagInkHex, '--pano-tilt': `${panoTilt(doc.id)}deg` }"
          :class="[panoColor(doc.color).card, panoColor(doc.color).body]"
          class="pano-paper relative flex flex-col min-w-0 h-72 rounded-md border border-black/[0.06] shadow-[0_1px_2px_rgba(0,0,0,0.06),0_10px_20px_-12px_rgba(0,0,0,0.25)] dark:shadow-[0_2px_4px_rgba(0,0,0,0.3),0_16px_32px_-12px_rgba(0,0,0,0.45)] rotate-[var(--pano-tilt)] hover:rotate-0 focus:rotate-0 hover:z-10 transition-[rotate] duration-200 focus:outline-none">
          <span aria-hidden="true" :class="panoColor(doc.color).tape" class="absolute -top-2.5 left-1/2 -translate-x-1/2 -rotate-2 w-20 h-5 rounded-[2px]"></span>
          <div class="flex flex-col flex-1 min-h-0 px-5 pt-6 pb-2">
            <h2 :class="panoColor(doc.color).title" class="text-lg font-black leading-snug line-clamp-2 break-words">{{ doc.title }}</h2>
            <div class="relative mt-2 flex-1 min-h-0 overflow-hidden">
              <div data-test="team-pano-excerpt" class="md-body text-[13px] leading-relaxed [&_img]:hidden"
                v-html="panoAssignees(renderMarkdown(doc.excerpt), { link: false })"></div>
            </div>
            <PanoProgress :doc="doc" class="mt-3 shrink-0" />
          </div>
          <footer :class="panoColor(doc.color).muted" class="flex items-center gap-2 px-5 py-3 border-t border-black/10 text-[11px] min-w-0">
            <OwnerAvatar :owner="ownerOf(doc)" size="md" />
            <span class="min-w-0 flex-1 truncate"><span class="font-bold">{{ ownerOf(doc).name }}</span> · {{ timeAgo(doc.updatedAt) }}</span>
          </footer>
        </router-link>
      </div>
    </div>
  </div>
</template>

<script setup>
import { onMounted, ref, watch } from 'vue';
import { listTeamPanoDocs } from '../api';
import { panoAssignees, panoColor, panoTilt, timeAgo } from '../composables/usePano';
import { renderMarkdown } from '../utils/markdown';
import { lostTeam, useTeamMode } from '../composables/useTeamMode';
import { useToasts } from '../composables/useToasts';
import { onWebMCPChange } from '../composables/useWebMCPChanges';
import OwnerAvatar from '../components/OwnerAvatar.vue';
import PanoProgress from '../components/PanoProgress.vue';

const { teamId, link, leaveTeam } = useTeamMode();
const { notifyError } = useToasts();

const docs = ref([]);
const loading = ref(true);

/** The doc's owner, as OwnerAvatar takes one. */
const ownerOf = (doc) => ({ userId: doc.ownerId, name: doc.ownerName || 'A teammate', picture: doc.ownerPicture });

// Quietly, without the loading state, once the page is up.
async function load({ quiet = false } = {}) {
  if (!quiet) loading.value = true;
  try {
    docs.value = (await listTeamPanoDocs(teamId.value))?.docs || [];
  } catch (err) {
    if (lostTeam(err)) leaveTeam();
    else notifyError(err.message);
  } finally {
    loading.value = false;
  }
}

onMounted(load);
// Picking another team in the switch changes only the query.
watch(teamId, (id) => { if (id) load(); });
onWebMCPChange(() => load({ quiet: true }));
</script>
