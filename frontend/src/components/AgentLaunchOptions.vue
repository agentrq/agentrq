<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<!--
  What to run, and its options: the part of a launch both launch forms share.
  The machine page and the workspace's own form each render this one, so the
  two cannot drift apart again (#1425); they differ only in the picker above it.
-->
<script setup>
import AcpAgentList from './AcpAgentList.vue'
import UnverifiedAgentConsent from './UnverifiedAgentConsent.vue'
import AgentKindPicker from './AgentKindPicker.vue'
import ClaudeOptionsPicker from './ClaudeOptionsPicker.vue'
import AcpEffortPicker from './AcpEffortPicker.vue'

defineProps({
  idPrefix: { type: String, required: true },
  claudeModels: { type: Array, default: undefined },
  acpModels: { type: Array, default: () => [] },
  acpAgents: { type: Array, default: () => [] },
  unverifiedAgent: { type: Boolean, default: false },
})

const kind = defineModel('kind', { type: String, required: true })
const params = defineModel('params', { type: Object, required: true })

defineEmits(['pick'])

const INPUT =
  'w-full px-3 py-2 text-sm font-mono border border-gray-200 dark:border-zinc-700 rounded-lg bg-white dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 focus:outline-none focus:ring-2 focus:ring-black dark:focus:ring-white'
const LABEL = 'block text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 mb-1'
</script>

<template>
  <div class="@container space-y-4">
    <AgentKindPicker :id-prefix="`${idPrefix}-kind`" v-model="kind" />

    <ClaudeOptionsPicker v-if="kind === 'claude-code'" :id-prefix="idPrefix" v-model="params" :models="claudeModels" />

    <!-- Agent first: the model list is per-agent. The list sits right under
         the field it fills, and effort after both, since it is the agent's. -->
    <div v-if="kind === 'acp-gateway'" class="grid gap-3 @md:grid-cols-2">
      <div>
        <label :for="`${idPrefix}-agent`" :class="LABEL">Agent</label>
        <input
          :id="`${idPrefix}-agent`"
          v-model="params.agent"
          type="text"
          spellcheck="false"
          autocapitalize="off"
          autocorrect="off"
          :class="INPUT"
        />
      </div>
      <div>
        <label :for="`${idPrefix}-model`" :class="LABEL"
          >Model <span class="normal-case font-normal tracking-normal">(optional)</span></label
        >
        <input
          :id="`${idPrefix}-model`"
          v-model="params.model"
          type="text"
          :list="`${idPrefix}-model-options`"
          placeholder="Gateway default"
          spellcheck="false"
          autocapitalize="off"
          autocorrect="off"
          :class="INPUT"
        />
        <datalist :id="`${idPrefix}-model-options`">
          <option v-for="m in acpModels" :key="m.id" :value="m.id">{{ m.name }}</option>
        </datalist>
      </div>
      <!-- The list only ever suggests: any other id typed in the field is
           still sent, so an agent the list does not know can still run. -->
      <AcpAgentList class="@md:col-span-2" :id-prefix="idPrefix" :agents="acpAgents" :query="params.agent" @pick="$emit('pick', $event)" />
      <AcpEffortPicker class="@md:col-span-2" :id-prefix="idPrefix" v-model="params.effort" />
      <UnverifiedAgentConsent v-if="unverifiedAgent" v-model="params.allowUnverified" class="@md:col-span-2" :id-prefix="idPrefix" :agent="params.agent" />
    </div>
  </div>
</template>
