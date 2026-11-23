<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<!-- The desktop shell's other profiles, and the way to add one. Shared by the
     sidebar's account menu and the macOS title bar's profile card, so the two
     places a profile is switched from cannot drift apart. -->
<template>
  <div>
    <p class="text-[10px] font-black text-gray-500 dark:text-zinc-500 mb-2">Other Profiles</p>
    <div v-if="others.length" class="space-y-1 mb-1">
      <div v-for="p in others" :key="p.id" class="group relative flex items-center">
        <button type="button"
                @click="$emit('switch', p.id)" :disabled="disabled"
                class="min-w-0 flex-1 flex items-center gap-2.5 px-2 py-1.5 rounded-sm text-left hover:bg-gray-50 dark:hover:bg-zinc-800 transition-colors disabled:opacity-50 disabled:cursor-not-allowed">
          <span class="w-6 h-6 shrink-0 rounded-full bg-gray-100 dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700 flex items-center justify-center text-[10px] font-black text-gray-600 dark:text-zinc-300 overflow-hidden">
            <img v-if="p.identity?.picture || p.account?.picture" :src="p.identity?.picture || p.account?.picture" class="w-full h-full object-cover" alt="" />
            <template v-else>{{ profileDisplay(p).initial }}</template>
          </span>
          <span class="min-w-0 flex-1">
            <span class="block text-xs font-bold text-gray-700 dark:text-zinc-200 truncate">{{ profileDisplay(p).title }}</span>
            <span class="block text-[10px] font-medium text-gray-400 dark:text-zinc-500 truncate" :title="profileDisplay(p).subtitle">{{ profileDisplay(p).subtitle }}</span>
            <!-- Said where the profiles are listed, beside the one
                 thing that fixes it. -->
            <span v-if="duplicateNotice(p, profiles)" class="block text-[10px] font-bold text-amber-600 dark:text-amber-500 truncate" :title="duplicateNotice(p, profiles)">{{ duplicateNotice(p, profiles) }}</span>
          </span>
        </button>
        <button type="button" @click.stop="$emit('remove', p.id)" :disabled="disabled"
                title="Remove this profile"
                class="shrink-0 ml-1 p-1.5 rounded-sm text-gray-400 dark:text-zinc-500 opacity-0 group-hover:opacity-100 focus:opacity-100 hover:text-red-600 dark:hover:text-red-400 hover:bg-gray-50 dark:hover:bg-zinc-800 transition-all disabled:opacity-50 disabled:cursor-not-allowed">
          <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
        </button>
      </div>
    </div>
    <button type="button" @click="$emit('add')" :disabled="disabled"
            class="w-full flex items-center gap-2.5 px-2 py-1.5 rounded-sm text-xs font-bold text-gray-600 dark:text-zinc-300 hover:bg-gray-50 dark:hover:bg-zinc-800 transition-colors disabled:opacity-50 disabled:cursor-not-allowed">
      <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M12 4v16m8-8H4" /></svg>
      Add profile
    </button>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { duplicateNotice, profileDisplay } from '../composables/useProfileDisplay'

const props = defineProps({
  /** Every profile, the active one included: a duplicate names its original. */
  profiles: { type: Array, default: () => [] },
  disabled: { type: Boolean, default: false },
})
defineEmits(['switch', 'remove', 'add'])

const others = computed(() => props.profiles.filter((p) => !p.active))
</script>
