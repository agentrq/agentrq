<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
-->

<script setup>
/**
 * A full-page terminal for one agent session.
 *
 * The machines and sessions list links here; this page is only the frame
 * around the terminal, which is a component so it can also sit beside a
 * workspace later without becoming a second copy.
 */
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import SessionTerminal from '../components/SessionTerminal.vue'

const route = useRoute()
const router = useRouter()

const sessionId = computed(() => String(route.params.id ?? ''))
</script>

<template>
  <div class="px-4 py-6 md:px-16 md:pt-8 md:pb-8">
    <div class="mb-4 flex items-center justify-between">
      <h1 class="text-lg font-black tracking-tight text-black">Terminal</h1>
      <button
        class="rounded-lg border border-gray-200 bg-white px-3 py-1.5 text-[11px] font-black uppercase tracking-widest text-black hover:border-gray-300"
        @click="router.back()"
      >
        Back
      </button>
    </div>
    <SessionTerminal :session-id="sessionId" />
  </div>
</template>
