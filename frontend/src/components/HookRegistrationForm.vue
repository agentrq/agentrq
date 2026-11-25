<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/**
 * A registration's name, hook, inputs and routes. The inputs are drawn from
 * the hook's manifest, and each event the hook writes maps to one of the
 * account's events or to none. Editing, the hook is fixed and a secret left
 * blank keeps its value.
 */
import { computed, nextTick, reactive, ref, watch } from 'vue'
import { fetchEventTriggers } from '../api'
import { usesCommandKey } from '../composables/useKeyboardShortcuts'
import { generateSecret, inputText, inputValue, notANumber } from '../utils/hooks'
import { ACTIONS, CANCEL_BUTTON, FORM_CARD, HELP, LABEL, PRIMARY_BUTTON, fieldClass } from '../utils/formStyles'
import { focusFirstInvalid, formKeyAction, submitHint } from '../utils/forms'
import ChoiceCards from './ChoiceCards.vue'
import FormField from './FormField.vue'
import SegmentedControl from './SegmentedControl.vue'

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
// What is wrong, by field: 'name', 'hook', or an input's name.
const errors = reactive({})
// The secrets shown as text rather than dots.
const shown = reactive({})
const copied = ref('')
const formEl = ref(null)
const keys = submitHint(usesCommandKey())

const hook = computed(() => props.hooks.find((h) => h.id === hookId.value) || null)
const hookOptions = computed(() => props.hooks.map((h) => ({ value: h.id, label: h.name, test: 'reg-hook', hook: h })))
const secretSet = (input) => !!props.registration?.secretsSet?.includes(input.name)
const inputId = (input) => `hook-in-${input.name}`

// A new hook picked: its own inputs and events, nothing carried over.
watch(hook, (h) => {
  for (const key of Object.keys(values)) delete values[key]
  for (const key of Object.keys(routes)) delete routes[key]
  for (const key of Object.keys(shown)) delete shown[key]
  for (const key of Object.keys(errors)) if (key !== 'name') delete errors[key]
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

function describe(input) {
  if (input.secret) {
    return editing.value && secretSet(input)
      ? 'Set. Leave it blank to keep it, or enter a new one.'
      : "Give the provider the same value. It is stored encrypted, so you won't see it again after saving."
  }
  if (input.type === 'array') return input.items === 'number' ? 'Numbers, separated by commas.' : 'Separate values with commas.'
  if (input.type === 'number') return 'A number.'
  return ''
}

/** What is wrong with an input's value, or ''. */
function check(input, v) {
  // A number field holding text gives the page an empty value: badInput says so.
  if (notANumber(v) || document.getElementById(inputId(input))?.validity?.badInput) {
    return `${label(input)} must be ${input.type === 'array' ? 'numbers, separated by commas' : 'a number'}.`
  }
  const empty = v === undefined || v === '' || (Array.isArray(v) && !v.length)
  const kept = input.secret && secretSet(input)
  if (input.required && input.type !== 'boolean' && empty && !kept) return `${label(input)} is required.`
  return ''
}

function clear(key) {
  delete errors[key]
}

async function submit() {
  if (props.busy) return
  for (const key of Object.keys(errors)) delete errors[key]
  if (!name.value.trim()) errors.name = 'Give the registration a name.'
  if (!hook.value) errors.hook = 'Pick a hook.'
  const inputs = {}
  for (const input of hook.value?.inputs ?? []) {
    const v = inputValue(input, values[input.name])
    const problem = check(input, v)
    if (problem) errors[input.name] = problem
    else if (v !== undefined) inputs[input.name] = v
  }
  if (Object.keys(errors).length) {
    await nextTick()
    focusFirstInvalid(formEl.value)
    return
  }
  const picked = {}
  for (const [event, id] of Object.entries(routes)) if (id) picked[event] = id
  const out = { name: name.value.trim(), inputs, routes: picked }
  if (!editing.value) out.hookId = hookId.value
  emit('submit', out)
}

function generate(input) {
  values[input.name] = generateSecret()
  shown[input.name] = true
  clear(input.name)
}

async function copySecret(input) {
  try {
    await navigator.clipboard.writeText(values[input.name])
    copied.value = input.name
    setTimeout(() => { if (copied.value === input.name) copied.value = '' }, 2000)
  } catch {
    copied.value = ''
  }
}

function onKeydown(event) {
  const action = formKeyAction(event)
  if (!action) return
  event.preventDefault()
  if (action === 'submit') submit()
  else emit('cancel')
}

const SECTION = 'pt-6 border-t border-gray-100 dark:border-zinc-800 space-y-5'
const SECTION_TITLE = 'text-sm font-bold text-gray-900 dark:text-zinc-50'
const INLINE_BUTTON = 'inline-flex items-center justify-center gap-1.5 px-3 text-[11px] font-black uppercase tracking-widest rounded-lg border border-gray-300 dark:border-zinc-700 bg-white dark:bg-zinc-900 text-gray-700 dark:text-zinc-300 hover:bg-gray-50 dark:hover:bg-zinc-800 transition-colors shrink-0'
</script>

<template>
  <form ref="formEl" novalidate :class="FORM_CARD" @submit.prevent="submit" @keydown="onKeydown">
    <div class="p-4 sm:p-6 space-y-6">
      <FormField v-slot="f" label="Name" required :error="errors.name"
        description="Yours to tell registrations apart. The provider never sees it.">
        <input :id="f.id" v-model="name" data-test="reg-name" type="text" maxlength="64" required autocomplete="off"
          placeholder="agentrq/agentrq CI" :aria-describedby="f.describedBy" :aria-invalid="f.invalid"
          :class="fieldClass(f.invalid)" @input="clear('name')" />
      </FormField>

      <ChoiceCards v-if="!editing" v-model="hookId" legend="Hook" required :options="hookOptions" :error="errors.hook"
        columns="sm:grid-cols-2 xl:grid-cols-3"
        @update:model-value="clear('hook')">
        <template #empty>
          <p v-if="!hooks.length" data-test="reg-no-hooks" :class="HELP" class="mb-2">
            No hook is available yet. Your server's operator adds the built-in ones.
          </p>
        </template>
        <template #option="{ option }">
          <span class="flex flex-wrap items-baseline gap-x-2 text-sm font-bold font-mono text-gray-900 dark:text-zinc-50">
            {{ option.hook.name }}
            <span class="font-sans text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">{{ option.hook.builtIn ? 'built-in' : 'yours' }}</span>
          </span>
          <span class="block text-xs text-gray-500 dark:text-zinc-400 mt-0.5">{{ option.hook.version }}<template v-if="option.hook.description"> · {{ option.hook.description }}</template></span>
        </template>
      </ChoiceCards>

      <div v-if="hook" class="grid gap-x-10 gap-y-6 lg:grid-cols-2 items-start">
        <section v-if="hook.inputs.length" :class="SECTION" aria-labelledby="reg-inputs-title">
          <div>
            <h3 id="reg-inputs-title" :class="SECTION_TITLE">Settings</h3>
            <p :class="HELP" class="mt-0.5">What the <span class="font-mono">{{ hook.name }}</span> hook needs to check and sort the requests it gets.</p>
          </div>
          <div v-for="input in hook.inputs" :key="input.name" data-test="reg-input">
            <fieldset v-if="input.type === 'boolean'" :id="inputId(input)" :data-test="`reg-input-${input.name}`">
              <legend :class="LABEL" class="mb-1.5">{{ label(input) }}</legend>
              <SegmentedControl v-model="values[input.name]" uppercase
                :options="[{ value: true, label: 'On', test: `reg-input-${input.name}-on` }, { value: false, label: 'Off', test: `reg-input-${input.name}-off` }]" />
            </fieldset>
            <FormField v-else v-slot="f" :id="inputId(input)" :label="label(input)" :required="input.required"
              :error="errors[input.name]" :description="describe(input)">
              <div class="flex flex-wrap gap-2">
                <div class="relative flex-1 basis-full sm:basis-[22rem] min-w-0">
                  <input :id="f.id" v-model="values[input.name]" :data-test="`reg-input-${input.name}`"
                    :type="input.secret ? (shown[input.name] ? 'text' : 'password') : input.type === 'number' ? 'number' : 'text'"
                    autocomplete="off" spellcheck="false" :required="input.required && !(input.secret && secretSet(input))"
                    :placeholder="input.secret && secretSet(input) ? '•••••••• set' : input.placeholder || ''"
                    :aria-describedby="f.describedBy" :aria-invalid="f.invalid"
                    :class="[fieldClass(f.invalid), input.secret && 'font-mono pr-10']" @input="clear(input.name)" />
                  <button v-if="input.secret" type="button" :data-test="`reg-reveal-${input.name}`"
                    :aria-label="shown[input.name] ? `Hide ${label(input)}` : `Show ${label(input)}`" :aria-pressed="!!shown[input.name]"
                    class="absolute inset-y-0 right-0 flex items-center px-3 text-gray-400 dark:text-zinc-500 hover:text-gray-700 dark:hover:text-zinc-300"
                    @click="shown[input.name] = !shown[input.name]">
                    <svg v-if="shown[input.name]" class="w-4 h-4" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"><path fill-rule="evenodd" d="M3.28 2.22a.75.75 0 0 0-1.06 1.06l14.5 14.5a.75.75 0 1 0 1.06-1.06l-1.745-1.745a10.029 10.029 0 0 0 3.3-4.38 1.651 1.651 0 0 0 0-1.185A10.004 10.004 0 0 0 9.999 3a9.956 9.956 0 0 0-4.744 1.194L3.28 2.22ZM7.752 6.69l1.092 1.092a2.5 2.5 0 0 1 3.374 3.373l1.091 1.092a4 4 0 0 0-5.557-5.557Z" clip-rule="evenodd" /><path d="m10.748 13.93 2.523 2.523a9.987 9.987 0 0 1-3.27.547c-4.258 0-7.894-2.66-9.337-6.41a1.651 1.651 0 0 1 0-1.186A10.007 10.007 0 0 1 2.839 6.02L6.07 9.252a4 4 0 0 0 4.678 4.678Z" /></svg>
                    <svg v-else class="w-4 h-4" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"><path d="M10 12.5a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5Z" /><path fill-rule="evenodd" d="M.664 10.59a1.651 1.651 0 0 1 0-1.186A10.004 10.004 0 0 1 10 3c4.257 0 7.893 2.66 9.336 6.41.147.381.146.804 0 1.186A10.004 10.004 0 0 1 10 17c-4.257 0-7.893-2.66-9.336-6.41ZM14 10a4 4 0 1 1-8 0 4 4 0 0 1 8 0Z" clip-rule="evenodd" /></svg>
                  </button>
                </div>
                <div v-if="input.secret" class="flex gap-2 basis-full sm:basis-auto">
                  <button type="button" :data-test="`reg-generate-${input.name}`" :class="INLINE_BUTTON" class="flex-1 sm:flex-none py-2.5"
                    @click="generate(input)">Generate</button>
                  <button v-if="values[input.name]" type="button" :data-test="`reg-copy-${input.name}`" :class="INLINE_BUTTON" class="flex-1 sm:flex-none py-2.5 min-w-[5.5rem]"
                    @click="copySecret(input)">
                    <svg v-if="copied === input.name" class="w-3.5 h-3.5 text-emerald-600 dark:text-emerald-400" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"><path fill-rule="evenodd" d="M16.704 4.153a.75.75 0 0 1 .143 1.052l-8 10.5a.75.75 0 0 1-1.127.075l-4.5-4.5a.75.75 0 0 1 1.06-1.06l3.894 3.893 7.48-9.817a.75.75 0 0 1 1.05-.143Z" clip-rule="evenodd" /></svg>
                    <span aria-live="polite">{{ copied === input.name ? 'Copied' : 'Copy' }}</span>
                  </button>
                </div>
              </div>
            </FormField>
          </div>
        </section>

        <section v-if="hook.events.length" :class="SECTION" aria-labelledby="reg-routes-title">
          <div>
            <h3 id="reg-routes-title" :class="SECTION_TITLE">Routes</h3>
            <p :class="HELP" class="mt-0.5">Pick which of your events each request fires. One left unrouted is logged and fires nothing.</p>
          </div>
          <p v-if="!events.length" class="px-3 py-2 rounded-lg bg-gray-50 dark:bg-zinc-800/60 text-xs text-gray-600 dark:text-zinc-300">
            You have no events yet. <RouterLink to="/events" class="font-bold underline underline-offset-2">Create one</RouterLink> to route to.
          </p>
          <div class="divide-y divide-gray-100 dark:divide-zinc-800">
            <div v-for="event in hook.events" :key="event.name" data-test="reg-route"
              class="grid sm:grid-cols-[minmax(0,1fr)_auto_minmax(0,16rem)] items-center gap-x-3 gap-y-1.5 py-3 first:pt-0 last:pb-0">
              <div class="min-w-0">
                <label :for="`reg-route-${event.name}`" class="block text-sm font-bold font-mono text-gray-900 dark:text-zinc-100 truncate">{{ event.name }}</label>
                <p v-if="event.description" :id="`reg-route-${event.name}-description`" :class="HELP" class="mt-0.5">{{ event.description }}</p>
              </div>
              <svg class="hidden sm:block w-4 h-4 text-gray-300 dark:text-zinc-600" fill="none" viewBox="0 0 24 24" stroke="currentColor" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13.5 4.5 21 12m0 0-7.5 7.5M21 12H3" /></svg>
              <select :id="`reg-route-${event.name}`" v-model="routes[event.name]" :data-test="`reg-route-${event.name}`"
                :aria-describedby="event.description ? `reg-route-${event.name}-description` : undefined" :class="fieldClass()">
                <option value="">Not routed</option>
                <option v-for="e in events" :key="e.id" :value="e.id">{{ e.name }}</option>
              </select>
            </div>
          </div>
        </section>

        <div v-if="risky.length" data-test="reg-warning" role="alert"
          class="lg:col-span-2 flex items-start gap-2 p-3 rounded-lg bg-amber-50 dark:bg-amber-500/10 border border-amber-200 dark:border-amber-500/30 text-xs text-amber-800 dark:text-amber-200">
          <svg class="w-4 h-4 shrink-0" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"><path fill-rule="evenodd" d="M8.485 2.495c.673-1.167 2.357-1.167 3.03 0l6.28 10.875c.673 1.167-.17 2.625-1.516 2.625H3.72c-1.347 0-2.189-1.458-1.515-2.625L8.485 2.495ZM10 5a.75.75 0 0 1 .75.75v3.5a.75.75 0 0 1-1.5 0v-3.5A.75.75 0 0 1 10 5Zm0 9a1 1 0 1 0 0-2 1 1 0 0 0 0 2Z" clip-rule="evenodd" /></svg>
          <span><span class="font-mono font-bold">{{ risky.join(', ') }}</span> {{ risky.length === 1 ? 'has' : 'have' }} a trigger in YOLO mode, which runs every
          command without asking. Its tasks will contain text written by whoever can call this hook's URL. You can still save.</span>
        </div>
      </div>
    </div>

    <div :class="ACTIONS">
      <span class="hidden sm:inline text-[10px] font-medium text-gray-400 dark:text-zinc-500 mr-auto">{{ keys }} to {{ editing ? 'save' : 'create' }} · Esc to cancel</span>
      <button type="button" data-test="reg-cancel" :class="CANCEL_BUTTON" @click="emit('cancel')">Cancel</button>
      <button type="submit" data-test="reg-submit" :disabled="busy" :class="PRIMARY_BUTTON">
        <svg v-if="busy" class="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 12a8 8 0 018-8v8H4z" /></svg>
        {{ editing ? 'Save changes' : 'Create registration' }}
      </button>
    </div>
  </form>
</template>
