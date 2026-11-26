<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/**
 * One attachment, previewed over the page, with Download, Open in browser and
 * Copy link. Draws nothing while `attachment` is null; Escape or the backdrop closes it.
 */
import { ref } from 'vue'
import { useRoute } from 'vue-router'
import { downloadAttachment, isInlineImage } from '../composables/useAttachmentDownload'
import { useEscapeKey } from '../composables/useEscapeKey'
import { useToasts } from '../composables/useToasts'
import { recordUiAction } from '../composables/useUiTelemetry'
import { writeClipboard } from '../composables/useMarkdownLinks'
import { TELEMETRY_UI_COPY_LINK } from '../api'

const props = defineProps({
  attachment: { type: Object, default: null },
  // The signed-in route to the file's bytes.
  src: { type: String, default: '' },
  // The file's public link, '' when it has none.
  publicLink: { type: String, default: '' },
})
const emit = defineEmits(['close'])

const route = useRoute()
const { notifyError } = useToasts()
const copied = ref('')

useEscapeKey(() => {
  if (!props.attachment) return false
  emit('close')
})

function download() {
  return downloadAttachment(props.attachment, { source: props.src, fallback: props.publicLink || props.src })
}

async function copyLink() {
  const id = props.attachment.id
  try {
    await writeClipboard(props.publicLink, { bridge: window.agentrq?.clipboard, clipboard: navigator.clipboard })
  } catch {
    notifyError('Could not copy to the clipboard.')
    return
  }
  recordUiAction(TELEMETRY_UI_COPY_LINK, route)
  copied.value = id
  setTimeout(() => { if (copied.value === id) copied.value = '' }, 1500)
}
</script>

<template>
  <div v-if="attachment" class="fixed inset-0 z-[110] flex items-center justify-center">
    <div class="absolute inset-0 bg-black/80 backdrop-blur-sm" @click="emit('close')"></div>
    <button @click="emit('close')" class="absolute top-6 right-6 text-white/50 hover:text-white z-20 p-2 rounded-full bg-white/10 hover:bg-white/20 transition-all">
      <svg class="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path d="M6 18L18 6M6 6l12 12"></path></svg>
    </button>
    <div class="relative max-w-[90vw] max-h-[85vh] flex flex-col items-center gap-4 z-10">
      <div class="rounded-sm overflow-hidden flex items-center justify-center min-w-[min(300px,90vw)] max-w-full bg-black shadow-2xl border border-white/10">
        <img v-if="isInlineImage(attachment)" :src="src" class="max-w-full max-h-[70vh] object-scale-down" />
        <video v-else-if="attachment.mimeType?.startsWith('video/')" controls autoplay :src="src" class="max-w-full max-h-[70vh]" />
        <div v-else-if="attachment.mimeType?.startsWith('audio/')" class="p-16 flex flex-col items-center gap-6">
          <div class="w-20 h-20 rounded-full bg-gray-900 dark:bg-white flex items-center justify-center shadow-lg">
            <svg class="w-10 h-10 text-white" fill="currentColor" viewBox="0 0 24 24"><path d="M8 5v14l11-7z"/></svg>
          </div>
          <audio controls autoplay :src="src" class="w-[min(400px,70vw)]" />
        </div>
        <iframe v-else-if="attachment.mimeType?.includes('pdf')" :src="src" class="w-[80vw] h-[75vh]" frameborder="0"></iframe>
        <div v-else class="p-8 @min-[40rem]:p-20 flex flex-col items-center gap-4 min-w-0">
          <svg class="w-24 h-24 text-white/20" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1"><path d="M7 21h10a2 2 0 002-2V9.414a1 1 0 00-.293-.707l-5.414-5.414A1 1 0 0012.586 3H7a2 2 0 00-2 2v14a2 2 0 002 2z" /></svg>
          <p class="text-white font-bold text-sm text-center break-all">{{ attachment.filename }}</p>
        </div>
      </div>
      <div class="flex items-center gap-3 @min-[40rem]:gap-4 max-w-full px-4 @min-[40rem]:px-6 py-3 bg-zinc-900 border border-zinc-800 rounded-sm shadow-xl">
        <div class="flex flex-col min-w-0">
          <p class="text-xs font-semibold text-white truncate @min-[40rem]:max-w-[250px]" :title="attachment.filename">{{ attachment.filename }}</p>
          <p class="text-[9px] font-semibold text-zinc-400 truncate">{{ attachment.mimeType }}</p>
        </div>
        <div class="w-px h-8 bg-zinc-700 shrink-0"></div>
        <!-- The public link when there is one; previews above stay on the signed-in
             route, which the desktop app's CSP allows and the attachment cache serves.
             A click saves from that route instead, so the file keeps its name and type. -->
        <a :href="publicLink || src" :download="attachment.filename"
           :target="publicLink ? '_blank' : undefined" :rel="publicLink ? 'noopener noreferrer' : undefined" @click.prevent="download"
           title="Download" aria-label="Download"
           class="shrink-0 flex items-center gap-2 px-3 @min-[40rem]:px-4 py-2 rounded-sm bg-white text-black text-[10px] font-semibold hover:bg-gray-100 transition-all">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path d="M4 16v1a2 2 0 002 2h12a2 2 0 002-2v-1m-4-4l-4 4m0 0l-4-4m4 4V4"></path></svg>
          <span class="hidden @min-[40rem]:inline">Download</span>
        </a>
        <!-- The public link, never the signed-in route: in the desktop app that route is
             on app:// and would open in-app, not in the browser. -->
        <a v-if="publicLink" :href="publicLink" target="_blank" rel="noopener noreferrer"
           title="Open in browser" aria-label="Open in browser"
           class="shrink-0 flex items-center justify-center gap-2 px-3 @min-[40rem]:px-4 py-2 rounded-sm border border-zinc-700 text-white text-[10px] font-semibold hover:bg-zinc-800 transition-all">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14"/></svg>
          <span class="hidden @min-[40rem]:inline">Open in browser</span>
        </a>
        <!-- Only a public link is worth sharing: the signed-in route opens for nobody else. -->
        <button v-if="publicLink" type="button" @click="copyLink"
                :title="copied === attachment.id ? 'Copied' : 'Copy link'" :aria-label="copied === attachment.id ? 'Copied' : 'Copy link'"
                class="shrink-0 flex items-center justify-center gap-2 @min-[40rem]:min-w-[7.5rem] px-3 @min-[40rem]:px-4 py-2 rounded-sm border border-zinc-700 text-white text-[10px] font-semibold hover:bg-zinc-800 transition-all">
          <svg v-if="copied !== attachment.id" class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101m-.758-4.899a4 4 0 005.656 0l4-4a4 4 0 00-5.656-5.656l-1.1 1.1"/></svg>
          <svg v-else class="w-4 h-4 text-green-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path d="M5 13l4 4L19 7"/></svg>
          <span class="hidden @min-[40rem]:inline">{{ copied === attachment.id ? 'Copied' : 'Copy link' }}</span>
        </button>
      </div>
    </div>
  </div>
</template>
