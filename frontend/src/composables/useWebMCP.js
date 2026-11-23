// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * Offering the AgentRQ interface to an agent running in the browser.
 *
 * This is the wiring: it builds the catalogue in `src/webmcp/tools.js` with the
 * live API client and router, hands it to the browser, and withdraws it again
 * when the session ends. The two halves it joins are deliberately framework-free
 * — this file is the only part that knows about Vue.
 *
 * Registration is tied to being signed in, and that is a security property
 * rather than tidiness. Every tool acts as the signed-in user, with the same
 * cookie and therefore exactly the same permissions the interface itself has:
 * no more, and none at all once they log out. Withdrawing the tools on sign-out
 * is what keeps that true, because the page is not reloaded in between.
 */
import { createToolCatalogue } from '../webmcp/tools'
import { registerTools } from '../webmcp/modelContext'
import { notifyWebMCPChange } from './useWebMCPChanges'
import { withScreen, hasUnsavedInput } from './useWebMCPScreen'
import { useToasts } from './useToasts'

/**
 * A description of where the person is, in the terms the tools speak.
 *
 * Route params are what an agent needs to turn "this task" into IDs, so they
 * are handed over as they are rather than being summarised into prose.
 *
 * @param {import('vue-router').RouteLocationNormalized} route
 */
export function describePage(route) {
  return {
    path: route?.path ?? '/',
    params: { ...(route?.params ?? {}) },
    query: { ...(route?.query ?? {}) },
  }
}

/**
 * Make a tool tell the open page when it has changed something, so the page
 * shows it without being left and reopened.
 *
 * Only after success, and never for a read. `navigate` is not annotated as a
 * read, since it changes what the person sees, but it changes no data, and the
 * page it lands on loads fresh anyway.
 *
 * @param {object} tool a descriptor from `createToolCatalogue`
 */
export function announceChanges(tool) {
  if (tool.annotations.readOnlyHint || tool.name === 'navigate') return tool
  return {
    ...tool,
    execute: async (...args) => {
      const result = await tool.execute(...args)
      notifyWebMCPChange()
      return result
    },
  }
}

/**
 * Register the catalogue, and return the means to withdraw it.
 *
 * @param {object} deps
 * @param {object} deps.api the API client
 * @param {import('vue-router').Router} deps.router
 * @returns {Promise<{ status: string, registered: string[], refused: Array<object>, unregister: () => void }>}
 */
export async function connectWebMCP({ api, router, context }) {
  const controller = new AbortController()

  const catalogue = createToolCatalogue({
    api,
    // The route table has no catch-all, so an unknown path would push the
    // person onto a blank page; refusing it tells the agent its guess was wrong.
    navigate: (path) => {
      if (!router.resolve(path).matched.length) {
        return Promise.reject(new Error(`No page at ${path}; see the navigate tool's description for the routes`))
      }
      return router.push(path)
    },
    // Read at call time, not at registration: the person moves around the app
    // while the tools stay registered, and a snapshot would answer for the page
    // they happened to be on when they signed in.
    currentPage: () => describePage(router.currentRoute.value),
  })
  const { addToast } = useToasts()
  const screen = {
    // A guard or a missing route must not fail a tool that has already done its
    // work; the toast still says what happened.
    go: (path) => router.push(path),
    isCurrent: (path) => router.resolve(path).fullPath === router.currentRoute.value.fullPath,
    hasUnsavedInput,
    // A toast offering to show the screen waits to be dismissed: it is the only
    // way to it, and four seconds is not long enough to finish typing.
    notify: (message, link) => addToast(message, 'info', null, link ? 0 : null, link),
  }
  const tools = catalogue.map(announceChanges).map((t) => withScreen(t, screen))

  const result = await registerTools(tools, { context, signal: controller.signal })
  return { ...result, unregister: () => controller.abort() }
}
