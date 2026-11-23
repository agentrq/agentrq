// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { onUnmounted, ref } from 'vue'

/** What every page appends to its title; the title bar already says whose app this is. */
const SUFFIX = ' | AgentRQ'

/**
 * The title bar's text: the page's title without the app name after it.
 *
 * A title that is only the app name is left alone, so the bar is never empty.
 *
 * @param {string} title
 * @returns {string}
 */
export function windowTitleText(title) {
  const text = typeof title === 'string' ? title.trim() : ''
  if (text.endsWith(SUFFIX) && text.length > SUFFIX.length) return text.slice(0, -SUFFIX.length).trim()
  return text
}

/**
 * The page's title, kept current, for the desktop title bar on macOS.
 *
 * Pages set `document.title` themselves — App.vue for the fixed pages, the task
 * and workspace views once their names load — so the title is watched where it
 * lands rather than by a second copy of every one of those rules. `<head>` is
 * observed, not the `<title>` element, because assigning `document.title` on a
 * page with no `<title>` creates one.
 *
 * @param {{ doc?: Document }} [options]
 * @returns {{ title: import('vue').Ref<string>, stop: () => void }}
 */
export function useWindowTitle({ doc = document } = {}) {
  const title = ref(windowTitleText(doc.title))
  const observer = new MutationObserver(() => {
    title.value = windowTitleText(doc.title)
  })
  observer.observe(doc.head, { childList: true, subtree: true, characterData: true })

  const stop = () => observer.disconnect()
  onUnmounted(stop)
  return { title, stop }
}
