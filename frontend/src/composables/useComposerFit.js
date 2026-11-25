// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The New Task form's description: it grows with its text, from `min` up to
 * the room left in the scrolling `container`, so the whole form stays on
 * screen; past that it scrolls inside. A height dragged by hand is kept until
 * the size is toggled, and never exceeds the room either.
 *
 * The form's width changes only when asked. `expanded` gives it the whole page
 * and the description all the room; dragging an edge sets `width` instead
 * (null is the page's own), keeping the form centred, so it grows on both
 * sides by what the edge moved. Either one clears the other; `reset` gives the
 * page's width back.
 *
 * `content` is the column holding the form inside `container`: the room is
 * the container's inner size less what the column takes besides the text.
 */
import { nextTick, onMounted, onUnmounted, ref, watch } from 'vue'

export function useComposerFit({ el, container, content, text, min = 160, minWidth = 384 }) {
  const expanded = ref(false)
  const width = ref(null)
  /** Whether an edge is being dragged; set by the edge's handle. */
  const dragging = ref(false)
  // The height last written, to tell a drag of the grip from our own fit.
  let written = null
  let startWidth = 0

  const padding = (c, a, b) => {
    const style = getComputedStyle(c)
    return parseFloat(style[a]) + parseFloat(style[b])
  }

  function room(t) {
    const c = container.value
    const besides = content.value.offsetHeight - t.offsetHeight
    return Math.max(min, c.clientHeight - padding(c, 'paddingTop', 'paddingBottom') - besides)
  }

  function fit() {
    const t = el.value
    if (!t || !container.value || !content.value) return
    const dragged = written !== null && t.style.height !== `${written}px`
    if (!dragged) t.style.height = `${min}px`
    const limit = room(t)
    t.style.maxHeight = `${limit}px`
    if (dragged) return
    written = expanded.value ? limit : Math.min(Math.max(t.scrollHeight, min), limit)
    t.style.height = `${written}px`
  }

  /** An edge moved `moved` px outwards since the drag began. */
  function resize(moved) {
    const c = container.value
    const most = Math.max(minWidth, c.clientWidth - padding(c, 'paddingLeft', 'paddingRight'))
    width.value = Math.round(Math.min(Math.max(startWidth + 2 * moved, minWidth), most))
  }

  function reset() {
    width.value = null
  }

  function toggleExpanded() {
    expanded.value = !expanded.value
    width.value = null
    // A toggle is a fresh start: whatever height was dragged gives way to it.
    written = null
  }

  watch(dragging, (on) => {
    if (!on) return
    startWidth = content.value.offsetWidth
    expanded.value = false
  })
  watch(text, () => nextTick(fit))
  // A new width wraps the text anew before it is measured.
  watch([width, expanded], () => nextTick(fit))

  onMounted(() => {
    fit()
    globalThis.addEventListener('resize', fit)
  })
  onUnmounted(() => globalThis.removeEventListener('resize', fit))

  return { expanded, toggleExpanded, width, dragging, resize, reset, fit }
}
