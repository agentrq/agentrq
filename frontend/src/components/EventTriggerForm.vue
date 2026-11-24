<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/**
 * A trigger: the task this event creates in a workspace. Its instructions can
 * carry what the event was sent with, through {{EVENT_PAYLOAD}} and
 * {{EVENT_FAQ}}; the title is always the text written here.
 */
import { computed, nextTick, onMounted, reactive, ref, useId } from 'vue'
import { insertAtCursor } from '../utils/insertAtCursor'
import { ACTIONS, CANCEL_BUTTON, FORM_CARD, HELP, LABEL, PRIMARY_BUTTON, fieldClass } from '../utils/formStyles'
import { focusFirstInvalid, formKeyAction, submitHint } from '../utils/forms'
import { usesCommandKey } from '../composables/useKeyboardShortcuts'
import FormField from './FormField.vue'
import SegmentedControl from './SegmentedControl.vue'

const props = defineProps({
  workspaces: { type: Array, default: () => [] },
  // The events the task may fire once completed: every other event.
  events: { type: Array, default: () => [] },
  trigger: { type: Object, default: null },
  busy: Boolean,
})
const emit = defineEmits(['submit', 'cancel'])

const PAYLOAD = '{{EVENT_PAYLOAD}}'
const FAQ = '{{EVENT_FAQ}}'
const VARIABLES = [
  { token: PAYLOAD, label: 'Event payload', test: 'insert-payload', hint: 'What the publisher sent with the event' },
  { token: FAQ, label: 'Event FAQ', test: 'insert-faq', hint: 'The questions and answers sent with the event' },
]
const ASSIGNEES = [
  { value: 'agent', label: 'Agent', description: "The workspace's agent picks the task up and works on it.", test: 'trigger-assignee-agent' },
  { value: 'human', label: 'Human', description: 'The task waits for you. No agent works on it.', test: 'trigger-assignee-human' },
]
const YOLO = [
  { value: true, label: 'On', test: 'trigger-yolo-on' },
  { value: false, label: 'Off', test: 'trigger-yolo-off' },
]

const editing = computed(() => !!props.trigger)
const form = reactive({
  workspaceId: props.trigger?.workspaceId ?? '',
  title: props.trigger?.title ?? '',
  body: props.trigger?.body ?? '',
  assignee: props.trigger?.assignee ?? 'agent',
  allowAllCommands: !!props.trigger?.allowAllCommands,
  emitEventId: props.trigger?.emitEventId ?? '',
})
const errors = reactive({ workspaceId: '', title: '' })
const formEl = ref(null)
const bodyEl = ref(null)
const headingId = `trigger-form-${useId()}`
const yoloId = `${headingId}-yolo`

const keys = submitHint(usesCommandKey())
const assigneeHelp = computed(() => ASSIGNEES.find((a) => a.value === form.assignee).description)
const usesPayload = computed(() => form.body.includes(PAYLOAD))

async function insert(token) {
  const { value, caret } = insertAtCursor(form.body, token, bodyEl.value)
  form.body = value
  await nextTick()
  bodyEl.value.focus()
  bodyEl.value.setSelectionRange(caret, caret)
}

function validate() {
  errors.workspaceId = form.workspaceId ? '' : 'Pick the workspace that gets the task.'
  errors.title = form.title.trim() ? '' : 'Give the task a title.'
  return !errors.workspaceId && !errors.title
}

async function submit() {
  if (props.busy) return
  if (!validate()) {
    await nextTick()
    focusFirstInvalid(formEl.value)
    return
  }
  emit('submit', {
    workspaceId: form.workspaceId,
    title: form.title.trim(),
    body: form.body,
    assignee: form.assignee,
    // Only an agent runs commands.
    allowAllCommands: form.assignee === 'agent' && form.allowAllCommands,
    // Kept as it was: this form does not schedule.
    cronSchedule: props.trigger?.cronSchedule ?? '',
    emitEventId: form.emitEventId,
  })
}

function onKeydown(event) {
  const action = formKeyAction(event)
  if (!action) return
  event.preventDefault()
  if (action === 'submit') submit()
  else emit('cancel')
}

onMounted(() => formEl.value.querySelector(editing.value ? 'input' : 'select').focus())
</script>

<template>
  <form ref="formEl" novalidate data-test="trigger-form" :aria-labelledby="headingId" :class="FORM_CARD"
    @submit.prevent="submit" @keydown="onKeydown">
    <div class="p-4 sm:p-5 space-y-6">
      <div>
        <h3 :id="headingId" class="text-sm font-bold text-gray-900 dark:text-zinc-50">{{ editing ? 'Edit trigger' : 'New trigger' }}</h3>
        <p :class="HELP" class="mt-0.5">Each time this event fires, a task is created in the workspace you pick.</p>
      </div>

      <div class="grid gap-6 lg:grid-cols-2">
        <FormField v-slot="f" label="Workspace" required :error="errors.workspaceId"
          description="Where the task is created.">
          <select :id="f.id" v-model="form.workspaceId" data-test="trigger-workspace" required
            :aria-describedby="f.describedBy" :aria-invalid="f.invalid" :class="fieldClass(f.invalid)"
            @change="errors.workspaceId = ''">
            <option value="" disabled>Select a workspace…</option>
            <option v-for="ws in workspaces" :key="ws.id" :value="ws.id">{{ ws.name }}</option>
          </select>
        </FormField>

        <FormField v-slot="f" label="Task title" required :error="errors.title"
          description="Used word for word. Put event details in the instructions.">
          <input :id="f.id" v-model="form.title" data-test="trigger-title" type="text" required maxlength="255"
            autocomplete="off" placeholder="Review the latest deploy"
            :aria-describedby="f.describedBy" :aria-invalid="f.invalid" :class="fieldClass(f.invalid)"
            @input="errors.title = ''" />
        </FormField>
      </div>

      <FormField v-slot="f" label="Instructions" optional>
        <div class="rounded-lg border border-gray-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 transition-shadow focus-within:border-gray-900 dark:focus-within:border-zinc-300 focus-within:ring-2 focus-within:ring-gray-900/10 dark:focus-within:ring-white/10">
          <div role="toolbar" :aria-controls="f.id" aria-label="Insert a variable"
            class="flex flex-wrap items-center gap-1.5 px-2 py-1.5 border-b border-gray-200 dark:border-zinc-800">
            <span class="px-1 text-[11px] font-bold text-gray-500 dark:text-zinc-400">Insert</span>
            <button v-for="v in VARIABLES" :key="v.token" type="button" :data-test="v.test" :title="`${v.hint}: inserts ${v.token}`"
              class="inline-flex items-center gap-1 h-7 px-2 rounded-md border border-gray-200 dark:border-zinc-700 bg-gray-50 dark:bg-zinc-900 text-xs font-semibold text-gray-700 dark:text-zinc-200 hover:bg-gray-100 dark:hover:bg-zinc-800 hover:border-gray-300 dark:hover:border-zinc-600 transition-colors"
              @click="insert(v.token)">
              <svg class="w-3.5 h-3.5 text-gray-400 dark:text-zinc-500" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"><path d="M10.75 4.75a.75.75 0 0 0-1.5 0v4.5h-4.5a.75.75 0 0 0 0 1.5h4.5v4.5a.75.75 0 0 0 1.5 0v-4.5h4.5a.75.75 0 0 0 0-1.5h-4.5v-4.5Z" /></svg>
              {{ v.label }}
            </button>
          </div>
          <textarea :id="f.id" ref="bodyEl" v-model="form.body" data-test="trigger-body" rows="6"
            :aria-describedby="`${f.id}-variables`"
            placeholder="Look into the failed deploy below and open a fix.&#10;&#10;{{EVENT_PAYLOAD}}"
            class="block w-full px-3 py-2.5 bg-transparent text-sm leading-relaxed text-gray-900 dark:text-zinc-100 placeholder:text-gray-400 dark:placeholder:text-zinc-600 outline-none resize-y min-h-[8rem] rounded-b-lg custom-scrollbar"></textarea>
        </div>
        <p :id="`${f.id}-variables`" :class="HELP" class="mt-1.5">
          <code class="font-mono text-gray-700 dark:text-zinc-300">{{ PAYLOAD }}</code> becomes what the publisher sent with the event,
          <code class="font-mono text-gray-700 dark:text-zinc-300">{{ FAQ }}</code> the questions and answers that came with it.
        </p>
        <p v-if="!usesPayload" data-test="trigger-payload-hint" role="status"
          class="mt-2 flex flex-wrap items-center gap-x-2 gap-y-1 px-3 py-2 rounded-lg bg-sky-50 dark:bg-sky-500/10 border border-sky-200 dark:border-sky-500/30 text-xs text-sky-800 dark:text-sky-200">
          <svg class="w-4 h-4 shrink-0" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"><path fill-rule="evenodd" d="M18 10a8 8 0 1 1-16 0 8 8 0 0 1 16 0Zm-7-4a1 1 0 1 1-2 0 1 1 0 0 1 2 0ZM9 9a.75.75 0 0 0 0 1.5h.253a.25.25 0 0 1 .244.304l-.459 2.066A1.75 1.75 0 0 0 10.747 15H11a.75.75 0 0 0 0-1.5h-.253a.25.25 0 0 1-.244-.304l.459-2.066A1.75 1.75 0 0 0 9.253 9H9Z" clip-rule="evenodd" /></svg>
          <span class="flex-1 min-w-[12rem]">This task won't include what the event carried.</span>
          <button type="button" data-test="trigger-payload-insert" class="font-bold underline underline-offset-2 hover:opacity-80" @click="insert(PAYLOAD)">
            Insert the payload
          </button>
        </p>
      </FormField>

      <div class="space-y-3">
        <div class="flex flex-wrap gap-x-10 gap-y-6">
          <fieldset :aria-describedby="`${headingId}-assignee`">
            <legend :class="LABEL" class="mb-1.5">Who works on it</legend>
            <SegmentedControl v-model="form.assignee" :options="ASSIGNEES" />
            <p :id="`${headingId}-assignee`" :class="HELP" class="mt-1.5 max-w-xs">{{ assigneeHelp }}</p>
          </fieldset>
          <fieldset v-if="form.assignee === 'agent'" :aria-describedby="`${yoloId}-description`">
            <legend :class="LABEL" class="mb-1.5">YOLO mode</legend>
            <SegmentedControl v-model="form.allowAllCommands" uppercase :options="YOLO" />
            <p :id="`${yoloId}-description`" :class="HELP" class="mt-1.5 max-w-xs">The agent runs every command without asking you first.</p>
          </fieldset>
          <FormField v-if="events.length" v-slot="f" class="flex-1 min-w-[16rem]" label="When the task is completed, fire"
            description="Its agent publishes this event before completing the task, so another workspace can pick up from there.">
            <select :id="f.id" v-model="form.emitEventId" data-test="trigger-emit" :aria-describedby="f.describedBy" :class="fieldClass()">
              <option value="">No event</option>
              <option v-for="ev in events" :key="ev.id" :value="ev.id">{{ ev.name }}</option>
            </select>
          </FormField>
        </div>
        <p v-if="form.assignee === 'agent' && form.allowAllCommands" data-test="trigger-yolo-warning"
          class="flex items-start gap-2 px-3 py-2 rounded-lg bg-amber-50 dark:bg-amber-500/10 border border-amber-200 dark:border-amber-500/30 text-xs text-amber-800 dark:text-amber-200">
          <svg class="w-4 h-4 shrink-0" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"><path fill-rule="evenodd" d="M8.485 2.495c.673-1.167 2.357-1.167 3.03 0l6.28 10.875c.673 1.167-.17 2.625-1.516 2.625H3.72c-1.347 0-2.189-1.458-1.515-2.625L8.485 2.495ZM10 5a.75.75 0 0 1 .75.75v3.5a.75.75 0 0 1-1.5 0v-3.5A.75.75 0 0 1 10 5Zm0 9a1 1 0 1 0 0-2 1 1 0 0 0 0 2Z" clip-rule="evenodd" /></svg>
          <span>These tasks can carry text from whoever fired the event, an outside service through a hook included. Turn this on only if you trust them all.</span>
        </p>
      </div>

    </div>

    <div :class="ACTIONS">
      <span class="hidden sm:inline text-[10px] font-medium text-gray-400 dark:text-zinc-500 mr-auto">{{ keys }} to save · Esc to cancel</span>
      <button type="button" data-test="trigger-cancel" :class="CANCEL_BUTTON" @click="emit('cancel')">Cancel</button>
      <button type="submit" data-test="trigger-submit" :disabled="busy" :class="PRIMARY_BUTTON">
        <svg v-if="busy" class="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 12a8 8 0 018-8v8H4z" /></svg>
        {{ editing ? (busy ? 'Saving…' : 'Save trigger') : (busy ? 'Creating…' : 'Create trigger') }}
      </button>
    </div>
  </form>
</template>
