<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
-->

<!--
  Starting an agent for the workspace you are looking at.

  The same launch as the machine page's, from the other end, so it uses the
  same rules (`useWorkspaceAgentLaunch`) and the same visual language as the
  form there — somebody who has used one should recognise the other.

  It offers itself only when there is an online machine to run on. With no
  machines the setup guide is still the honest answer, and this renders
  nothing rather than a button that cannot work.
-->
<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useWorkspaceAgentLaunch } from '../composables/useWorkspaceAgentLaunch'
import AgentKindPicker from './AgentKindPicker.vue'
import { terminalPath } from '../composables/useTerminalView'

const props = defineProps({
  workspace: { type: Object, default: null },
  // 'hero' is the workspace's empty state, where this is one action beside
  // another and stays folded until asked for. 'card' is the setup page, where
  // a form is what the whole page is made of and folding it would just add a
  // click.
  variant: { type: String, default: 'hero' },
})

const emit = defineEmits(['started', 'availability'])

const router = useRouter()
const workspace = computed(() => props.workspace)
const launcher = useWorkspaceAgentLaunch({ workspace })

const {
  available,
  selected,
  error,
  launching,
  machineId,
  kind,
  params,
  offered,
  blockers,
  canLaunch,
  acpAgents,
  acpModels,
} = launcher

const open = ref(props.variant === 'card')
const started = ref(null)

onMounted(launcher.load)

// The page outside needs this: when there is a machine to run on, starting an
// agent is the primary action and the setup guide becomes the secondary one.
// Two primary buttons side by side ask somebody to choose before they know the
// difference.
watch(offered, (value) => emit('availability', value), { immediate: true })

/** The machine that will run it, when there is no choice to make. */
const onlyMachine = computed(() => (available.value.length === 1 ? available.value[0] : null))

const primaryLabel = computed(() => {
  if (launching.value) return 'Starting…'
  return started.value ? 'Starting…' : 'Start an agent'
})

async function start() {
  const session = await launcher.launch()
  if (!session) return
  started.value = session
  emit('started', session)
  // Straight to the terminal: the daemon has been *asked*, and what happens
  // next — the folder missing, the agent booting, the first question it wants
  // answering — is visible there and nowhere else. Leaving somebody on an
  // empty workspace page with a toast would hide the only thing worth
  // watching. The path is `terminalPath`'s to build, shared with the machine
  // page so the two launches cannot drift.
  const to = terminalPath(session)
  if (to) router.push(to)
}
</script>

<template>
  <div v-if="offered" :class="variant === 'card' ? 'w-full' : 'w-full max-w-[420px]'">
    <!-- Folded: one action, and what it will do. -->
    <button
      v-if="!open"
      type="button"
      @click="open = true"
      class="w-full px-8 py-3 bg-black dark:bg-white text-white dark:text-zinc-900 rounded-sm text-[10px] font-black uppercase tracking-widest shadow-lg hover:shadow-xl active:scale-95 transition-all"
    >
      Start an agent
    </button>
    <p
      v-if="!open && onlyMachine"
      class="mt-2 text-[11px] text-gray-500 dark:text-zinc-400 font-medium text-center"
    >
      on {{ onlyMachine.name }}
    </p>

    <div
      v-if="open"
      class="@container border border-gray-100 dark:border-zinc-800 rounded-xl p-5 bg-white dark:bg-zinc-900 space-y-4 text-left animate-in fade-in slide-in-from-bottom-2 duration-300"
    >
      <div>
        <h2 class="text-sm font-bold text-gray-800 dark:text-zinc-200">Start an agent</h2>
        <!-- Says where it will run and on what. With one machine there is
             nothing to pick, so naming it here is the whole answer; with
             several, the picker below is. `break-all` because a path has no
             spaces to wrap at, so a long one otherwise overflows the card. -->
        <p class="text-[11px] text-gray-500 dark:text-zinc-400 mt-0.5">
          <template v-if="workspace?.workingDirectory">
            Runs in
            <code class="bg-gray-100 dark:bg-zinc-800 px-1 py-0.5 rounded text-gray-900 dark:text-white break-all">{{
              workspace.workingDirectory
            }}</code>
          </template>
          <template v-else>Runs in the workspace's folder</template>
          <template v-if="onlyMachine"> on {{ onlyMachine.name }}, your only machine that is online.</template>
          <template v-else> on the machine you pick.</template>
        </p>
      </div>

      <!-- One machine is not a choice, so it is stated in the line above rather
           than asked for here. -->
      <div v-if="available.length > 1">
        <p
          id="start-agent-machine-label"
          class="block text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 mb-1"
        >
          Machine
        </p>
        <!-- The machine page's workspace picker in miniature, so the two launch
             forms still look alike. `min-w-0` on the card and not only on the
             name: a grid item's minimum width is its content, so without it a
             long machine name widens the card instead of truncating. -->
        <div
          role="radiogroup"
          aria-labelledby="start-agent-machine-label"
          class="grid gap-2 @sm:grid-cols-2 max-h-40 overflow-y-auto custom-scrollbar"
        >
          <label
            v-for="m in available"
            :key="m.id"
            class="flex items-center gap-2 px-3 py-2 rounded-lg border cursor-pointer transition-all min-w-0"
            :class="
              machineId === m.id
                ? 'border-gray-900 dark:border-white bg-gray-50 dark:bg-zinc-800'
                : 'border-gray-200 dark:border-zinc-700 hover:border-gray-300 dark:hover:border-zinc-600'
            "
          >
            <input
              :id="`start-agent-machine-${m.id}`"
              type="radio"
              name="start-agent-machine"
              class="sr-only"
              :value="m.id"
              :checked="machineId === m.id"
              @change="machineId = m.id"
            />
            <span
              class="w-3 h-3 rounded-full border flex items-center justify-center shrink-0"
              :class="
                machineId === m.id
                  ? 'border-gray-900 dark:border-white'
                  : 'border-gray-300 dark:border-zinc-600'
              "
            >
              <span
                v-if="machineId === m.id"
                class="w-1.5 h-1.5 rounded-full bg-gray-900 dark:bg-white"
              />
            </span>
            <span class="block text-xs font-bold text-gray-900 dark:text-zinc-100 truncate min-w-0">
              {{ m.name }}
            </span>
          </label>
        </div>
      </div>

      <!-- Its own row: sharing one with the machine picker meant two columns,
           and in a 420px card neither was wide enough to read. -->
      <AgentKindPicker id-prefix="start-agent-kind" v-model="kind" />

      <!-- Only the gateway needs these, and only the agent is required. Agent
           first: the model list is per-agent, so there is nothing to suggest
           for the second field until the first is answered. -->
      <div v-if="kind === 'acp-gateway'" class="grid gap-3 @md:grid-cols-2">
        <div>
          <label
            for="start-agent-agent"
            class="block text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 mb-1"
            >Agent</label
          >
          <input
            id="start-agent-agent"
            v-model="params.agent"
            type="text"
            list="start-agent-agent-options"
            spellcheck="false"
            autocapitalize="off"
            autocorrect="off"
            class="w-full px-3 py-2 text-sm font-mono border border-gray-200 dark:border-zinc-700 rounded-lg bg-white dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 focus:outline-none focus:ring-2 focus:ring-black dark:focus:ring-white"
          />
          <!-- A datalist only ever suggests: typing anything else, including
               while the list is empty or never arrives, is still accepted. -->
          <datalist id="start-agent-agent-options">
            <option v-for="a in acpAgents" :key="a.id" :value="a.id">{{ a.name }}</option>
          </datalist>
        </div>
        <div>
          <label
            for="start-agent-model"
            class="block text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 mb-1"
            >Model <span class="normal-case font-normal tracking-normal">(optional)</span></label
          >
          <input
            id="start-agent-model"
            v-model="params.model"
            type="text"
            list="start-agent-model-options"
            placeholder="Gateway default"
            spellcheck="false"
            autocapitalize="off"
            autocorrect="off"
            class="w-full px-3 py-2 text-sm font-mono border border-gray-200 dark:border-zinc-700 rounded-lg bg-white dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 focus:outline-none focus:ring-2 focus:ring-black dark:focus:ring-white"
          />
          <datalist id="start-agent-model-options">
            <option v-for="m in acpModels" :key="m.id" :value="m.id">{{ m.name }}</option>
          </datalist>
        </div>
      </div>

      <!-- Said before the button rather than after it is pressed. -->
      <ul v-if="blockers.length" class="space-y-1">
        <li
          v-for="b in blockers"
          :key="b.reason"
          class="text-[11px] text-gray-500 dark:text-zinc-400"
        >
          {{ b.reason }}
          <router-link v-if="b.fix" :to="b.fix.to" class="underline hover:text-gray-700 dark:hover:text-zinc-200">{{
            b.fix.label
          }}</router-link>
        </li>
      </ul>

      <p v-if="error" class="text-[11px] text-red-600 dark:text-red-400 font-medium">{{ error }}</p>

      <div class="flex items-center gap-2">
        <button
          type="button"
          :disabled="!canLaunch"
          @click="start"
          class="px-4 py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95 disabled:opacity-40 disabled:cursor-not-allowed"
        >
          {{ primaryLabel }}
        </button>
        <button
          v-if="variant === 'hero'"
          type="button"
          @click="open = false"
          class="px-4 py-2 text-[11px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400 hover:text-gray-800 dark:hover:text-zinc-200 transition-all"
        >
          Cancel
        </button>
      </div>
    </div>
  </div>
</template>
