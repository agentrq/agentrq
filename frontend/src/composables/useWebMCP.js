// Copyright 2026 Contextual, Inc. https://agentrq.com

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
 * Register the catalogue, and return the means to withdraw it.
 *
 * @param {object} deps
 * @param {object} deps.api the API client
 * @param {import('vue-router').Router} deps.router
 * @returns {Promise<{ status: string, registered: string[], refused: Array<object>, unregister: () => void }>}
 */
export async function connectWebMCP({ api, router, context }) {
  const controller = new AbortController()

  const tools = createToolCatalogue({
    api,
    navigate: (path) => router.push(path),
    // Read at call time, not at registration: the person moves around the app
    // while the tools stay registered, and a snapshot would answer for the page
    // they happened to be on when they signed in.
    currentPage: () => describePage(router.currentRoute.value),
  })

  const result = await registerTools(tools, { context, signal: controller.signal })
  return { ...result, unregister: () => controller.abort() }
}
