<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!--
  The consent the gateway needs to install an agent the ACP registry publishes
  no checksum for on this machine. Asked before the launch, because without it
  the gateway refuses the download and the agent exits at once.
-->
<script setup>
defineProps({
  agent: { type: String, required: true },
  idPrefix: { type: String, required: true },
})

const allowed = defineModel({ type: Boolean, default: false })
</script>

<template>
  <label :for="`${idPrefix}-allow-unverified`" class="flex items-start gap-2 min-w-0 text-[11px] text-gray-600 dark:text-zinc-300">
    <input :id="`${idPrefix}-allow-unverified`" v-model="allowed" type="checkbox" class="mt-0.5 shrink-0 accent-black dark:accent-white" />
    <span class="break-words">
      Install <span class="font-mono">{{ agent.trim() }}</span> without a checksum. The ACP registry publishes none for it on
      this machine, so its download cannot be verified.
    </span>
  </label>
</template>
