<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
/**
 * A registration's name, hook, inputs and routes. The inputs are drawn from
 * the hook's manifest, and each event the hook writes maps to one of the
 * account's events or to none. Editing, the hook is fixed and a secret left
 * blank keeps its value.
 */
import { computed, reactive, ref, watch } from 'vue'
import { fetchEventTriggers } from '../api'
import { generateSecret, inputText, inputValue, notANumber } from '../utils/hooks'

const props = defineProps({
  hooks: { type: Array, default: () => [] },
  events: { type: Array, default: () => [] },
  registration: { type: Object, default: null },
  busy: Boolean,
})
const emit = defineEmits(['submit', 'cancel'])

const editing = computed(() => !!props.registration)
const name = ref(props.registration?.name ?? '')
const hookId = ref(props.registration?.hookId ?? '')
const values = reactive({})
const routes = reactive({})
const error = ref('')

const hook = computed(() => props.hooks.find((h) => h.id === hookId.value) || null)
const secretSet = (input) => !!props.registration?.secretsSet?.includes(input.name)

// A new hook picked: its own inputs and events, nothing carried over.
watch(hook, (h) => {
  for (const key of Object.keys(values)) delete values[key]
  for (const key of Object.keys(routes)) delete routes[key]
  for (const input of h?.inputs ?? []) {
    values[input.name] = input.secret ? '' : inputText(input, props.registration?.inputs?.[input.name])
  }
  for (const event of h?.events ?? []) routes[event.name] = props.registration?.routes?.[event.name] ?? ''
}, { immediate: true })

// Events with a trigger that runs every command, looked up once each.
const allowAll = reactive({})
watch(() => Object.values(routes), (ids) => {
  for (const id of ids) {
    if (!id || id in allowAll) continue
    allowAll[id] = false
    fetchEventTriggers(id)
      .then((data) => { allowAll[id] = (data.eventTriggers ?? []).some((t) => t.allowAllCommands) })
      .catch(() => {})
  }
}, { immediate: true })

const risky = computed(() => {
  const ids = new Set(Object.values(routes).filter((id) => allowAll[id]))
  return props.events.filter((e) => ids.has(e.id)).map((e) => e.name)
})

const label = (input) => input.label || input.name

function submit() {
  error.value = ''
  if (!name.value.trim()) error.value = 'A name is required.'
  else if (!hook.value) error.value = 'Pick a hook.'
  if (error.value) return
  const inputs = {}
  for (const input of hook.value.inputs) {
    const v = inputValue(input, values[input.name])
    // A number field holding text gives the page an empty value: badInput says so.
    if (notANumber(v) || document.getElementById(`hook-in-${input.name}`)?.validity?.badInput) {
      error.value = `${label(input)} must be ${input.type === 'array' ? 'numbers, separated by commas' : 'a number'}.`
      return
    }
    if (v !== undefined) inputs[input.name] = v
  }
  const picked = {}
  for (const [event, id] of Object.entries(routes)) if (id) picked[event] = id
  const out = { name: name.value.trim(), inputs, routes: picked }
  if (!editing.value) out.hookId = hookId.value
  emit('submit', out)
}

const field = 'w-full px-3 py-2 text-sm border border-gray-200 dark:border-zinc-700 rounded-lg bg-white dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 placeholder-gray-400 dark:placeholder-zinc-500 focus:outline-none focus:ring-2 focus:ring-black dark:focus:ring-white'
const heading = 'block text-[11px] font-bold text-gray-500 dark:text-zinc-400 uppercase tracking-widest mb-1'
</script>

<template>
  <form class="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl p-5 space-y-5" @submit.prevent="submit">
    <div>
      <label :class="heading" for="hook-reg-name">Name <span class="text-red-500">*</span></label>
      <input id="hook-reg-name" v-model="name" data-test="reg-name" type="text" maxlength="64"
        placeholder="agentrq/agentrq CI" :class="field" />
    </div>

    <div v-if="!editing">
      <p :class="heading">Hook <span class="text-red-500">*</span></p>
      <div v-if="!hooks.length" data-test="reg-no-hooks" class="text-xs text-gray-500 dark:text-zinc-400">
        No hook is available yet. Your server's operator adds the built-in ones.
      </div>
      <div class="grid gap-2 sm:grid-cols-2">
        <label v-for="h in hooks" :key="h.id" data-test="reg-hook"
          class="flex gap-3 p-3 rounded-lg border cursor-pointer transition-colors"
          :class="h.id === hookId ? 'border-black dark:border-white' : 'border-gray-200 dark:border-zinc-700 hover:border-gray-300 dark:hover:border-zinc-600'">
          <input v-model="hookId" type="radio" name="hook" :value="h.id" class="mt-1 accent-black dark:accent-white" />
          <span class="min-w-0">
            <span class="block text-sm font-bold font-mono text-gray-900 dark:text-zinc-50">
              {{ h.name }}
              <span class="ml-1 text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">{{ h.builtIn ? 'built-in' : 'yours' }}</span>
            </span>
            <span class="block text-xs text-gray-500 dark:text-zinc-400 mt-0.5">{{ h.version }}<template v-if="h.description"> · {{ h.description }}</template></span>
          </span>
        </label>
      </div>
    </div>

    <template v-if="hook">
      <div v-if="hook.inputs.length" class="space-y-3">
        <p :class="heading">Inputs · from the {{ hook.name }} hook</p>
        <div v-for="input in hook.inputs" :key="input.name" data-test="reg-input">
          <label :for="`hook-in-${input.name}`" class="block text-xs font-semibold text-gray-700 dark:text-zinc-300 mb-1">
            {{ label(input) }}<span v-if="input.required" class="text-red-500"> *</span>
            <span v-if="input.secret" class="ml-1 text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">secret</span>
          </label>
          <label v-if="input.type === 'boolean'" class="inline-flex items-center gap-2 text-xs text-gray-600 dark:text-zinc-300">
            <input :id="`hook-in-${input.name}`" v-model="values[input.name]" type="checkbox" class="accent-black dark:accent-white" />
            On
          </label>
          <div v-else class="flex gap-2">
            <input :id="`hook-in-${input.name}`" v-model="values[input.name]" :data-test="`reg-input-${input.name}`"
              :type="input.secret ? 'password' : input.type === 'number' ? 'number' : 'text'" autocomplete="off"
              :placeholder="input.secret && secretSet(input) ? '•••••••• set · leave blank to keep' : input.type === 'array' ? 'Comma-separated' : ''"
              :class="[field, input.secret && 'font-mono']" />
            <button v-if="input.secret" type="button" :data-test="`reg-generate-${input.name}`"
              class="px-3 py-2 text-[11px] font-black uppercase tracking-widest rounded-lg border border-gray-200 dark:border-zinc-700 text-gray-700 dark:text-zinc-300 hover:bg-gray-50 dark:hover:bg-zinc-800 shrink-0"
              @click="values[input.name] = generateSecret()">
              Generate
            </button>
          </div>
          <p v-if="input.secret" class="text-[11px] text-gray-400 dark:text-zinc-500 mt-1">
            Give the provider the same value. Stored encrypted; you will not see it again.
            <span v-if="values[input.name]" class="font-mono break-all text-gray-600 dark:text-zinc-300" data-test="reg-secret-shown">{{ values[input.name] }}</span>
          </p>
        </div>
      </div>

      <div v-if="hook.events.length" class="space-y-2">
        <p :class="heading">Routes · hook event → your event</p>
        <div v-for="event in hook.events" :key="event.name" data-test="reg-route"
          class="grid sm:grid-cols-[1fr_auto_minmax(0,14rem)] items-center gap-x-3 gap-y-1">
          <div class="min-w-0">
            <p class="text-sm font-mono text-gray-900 dark:text-zinc-100 truncate">{{ event.name }}</p>
            <p v-if="event.description" class="text-[11px] text-gray-500 dark:text-zinc-400 truncate">{{ event.description }}</p>
          </div>
          <span class="hidden sm:block text-gray-300 dark:text-zinc-600" aria-hidden="true">→</span>
          <select v-model="routes[event.name]" :data-test="`reg-route-${event.name}`" :aria-label="`Event fired by ${event.name}`" :class="field">
            <option value="">Not routed</option>
            <option v-for="e in events" :key="e.id" :value="e.id">{{ e.name }}</option>
          </select>
        </div>
        <p v-if="!events.length" class="text-[11px] text-gray-500 dark:text-zinc-400">
          You have no events yet. <RouterLink to="/events" class="underline">Create one</RouterLink> to route to.
        </p>
      </div>

      <div v-if="risky.length" data-test="reg-warning" role="alert"
        class="p-3 rounded-lg bg-amber-50 dark:bg-amber-500/10 border border-amber-200 dark:border-amber-500/30 text-xs text-amber-800 dark:text-amber-200">
        ⚠ <span class="font-mono">{{ risky.join(', ') }}</span> {{ risky.length === 1 ? 'has' : 'have' }} a trigger that runs every
        command without asking. Its tasks will contain text written by whoever can call this hook's URL. You can still save.
      </div>
    </template>

    <p v-if="error" data-test="reg-error" class="text-xs text-red-600 dark:text-red-400">{{ error }}</p>

    <div class="flex items-center gap-2 pt-2 border-t border-gray-100 dark:border-zinc-800">
      <button type="submit" data-test="reg-submit" :disabled="busy"
        class="px-4 py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95 disabled:opacity-50">
        {{ editing ? 'Save' : 'Create' }}
      </button>
      <button type="button" data-test="reg-cancel" @click="emit('cancel')"
        class="px-4 py-2 bg-white dark:bg-zinc-800 text-gray-700 dark:text-zinc-300 border border-gray-200 dark:border-zinc-700 text-[11px] font-black uppercase tracking-widest rounded-lg hover:bg-gray-50 dark:hover:bg-zinc-700 transition-all active:scale-95">
        Cancel
      </button>
    </div>
  </form>
</template>
