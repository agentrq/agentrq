<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
/** Registers a hook, then opens the registration with its URL and token. */
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { createHookRegistration, fetchAvailableHooks, fetchEvents } from '../api'
import { useToasts } from '../composables/useToasts'
import HookRegistrationForm from '../components/HookRegistrationForm.vue'
import LoadingState from '../components/LoadingState.vue'
import { rememberTokenUrl } from '../utils/hooks'

const router = useRouter()
const { notifyError, notifySuccess } = useToasts()
const hooks = ref([])
const events = ref([])
const loading = ref(true)
const busy = ref(false)

onMounted(async () => {
  try {
    const [available, list] = await Promise.all([fetchAvailableHooks(), fetchEvents()])
    hooks.value = (available.hooks ?? []).filter((h) => h.enabled)
    events.value = list.events ?? []
  } catch (e) {
    notifyError(e.message)
  } finally {
    loading.value = false
  }
})

async function create(fields) {
  busy.value = true
  try {
    const reg = await createHookRegistration(fields)
    rememberTokenUrl(reg.id, reg.urlWithToken)
    notifySuccess(`Registered ${reg.name}.`)
    router.push(`/events/hooks/${reg.id}`)
  } catch (e) {
    notifyError(e.message)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="flex flex-col h-full w-full overflow-y-auto custom-scrollbar">
    <div class="w-full px-4 py-2 mb-4 shrink-0">
      <p class="text-[11px] text-gray-400 dark:text-zinc-500">
        <RouterLink to="/events" class="hover:underline">Events</RouterLink> /
        <RouterLink to="/events/hooks" class="hover:underline">Hooks</RouterLink>
      </p>
      <h1 class="text-lg md:text-2xl font-black text-gray-800 dark:text-zinc-200 leading-tight">New registration</h1>
      <p class="text-xs text-gray-500 dark:text-zinc-400 mt-0.5">Pick a hook, fill in its inputs, and map its events to yours.</p>
    </div>
    <div class="px-4 pb-10 max-w-3xl">
      <LoadingState v-if="loading" label="Loading hooks…" />
      <HookRegistrationForm v-else :hooks="hooks" :events="events" :busy="busy"
        @submit="create" @cancel="router.push('/events/hooks')" />
    </div>
  </div>
</template>
