// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// Draws a Pano doc's a2ui blocks in its rendered body. The body stays the
// markdown's HTML, which the page's checklist, assignees and file links work
// on; each valid block's <pre> is hidden behind its card after it renders,
// and an invalid one stays as code. Hidden, not replaced: a body drawn again
// with the same HTML is not rewritten, and must still find its blocks.

import { getCurrentInstance, h, nextTick, onBeforeUnmount, render, watch } from 'vue'

import PanoA2ui from '../components/a2ui/PanoA2ui.vue'
import { panoSurface } from '../a2ui/pano.js'

/**
 * Mounts the cards of container's a2ui blocks, and returns what unmounts
 * them. appContext gives the cards the app's stores.
 */
export function mountPanoA2ui(container, { files, color, appContext = null }) {
  const mounted = []
  for (const code of container?.querySelectorAll('pre > code.language-a2ui') ?? []) {
    const surface = panoSurface(code.textContent, files)
    if (!surface) continue
    const pre = code.parentElement
    const host = document.createElement('div')
    host.dataset.panoA2uiHost = ''
    pre.hidden = true
    pre.after(host)
    const vnode = h(PanoA2ui, { surface, files, color })
    vnode.appContext = appContext
    render(vnode, host)
    mounted.push([pre, host])
  }
  return () => {
    for (const [pre, host] of mounted) {
      render(null, host)
      host.remove()
      pre.hidden = false
    }
  }
}

/**
 * Keeps the cards of the element el() returns drawn: again whenever source()
 * changes, after the page renders it.
 */
export function usePanoA2ui(el, source) {
  const appContext = getCurrentInstance()?.appContext ?? null
  let unmount = null
  watch(source, async () => {
    unmount?.()
    unmount = null
    await nextTick()
    const { files, color } = source()
    unmount = mountPanoA2ui(el(), { files, color, appContext })
  }, { immediate: true, flush: 'post' })
  onBeforeUnmount(() => unmount?.())
}
