// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { EXT_SCHEME } from './guest.js'

/**
 * An Extension and its own pages in the side panel, talking to each other.
 *
 * The page side is `window.agentrq.panel` (the panel preload); the Extension
 * side is `ctx.panel`. Who a message is for is decided by **the address of
 * the page that sent it** — `agentrq-ext://<extension>/…` reaches that
 * Extension and no other — and never by anything in the message. A website in
 * the panel has no bridge to begin with, and if it had one its address would
 * name no Extension, so it would reach nothing.
 */

/** A message is a request between two pieces of this app, not a file transfer. */
export const MAX_MESSAGE_BYTES = 1024 * 1024

const fail = (reason) => ({ ok: false, reason })

/** The Extension an address belongs to, or '' for anything that is not one of its pages. */
export function extensionOf(rawUrl) {
  try {
    const url = new URL(String(rawUrl ?? ''))
    return url.protocol === `${EXT_SCHEME}:` ? url.host : ''
  } catch {
    return ''
  }
}

/** A message as it will cross, or why it cannot. */
function encode(message, owner) {
  let text
  try {
    text = JSON.stringify(message)
  } catch {
    return fail(`${owner}: that message is not plain data.`)
  }
  if (text === undefined) return fail(`${owner}: that message is not plain data.`)
  if (Buffer.byteLength(text) > MAX_MESSAGE_BYTES) return fail(`${owner}: that message is larger than 1 MB.`)
  return { ok: true, value: JSON.parse(text) }
}

/**
 * Hand `message` to every open page of `owner`. Pages are guests whose
 * address names it; anything else in the panel is skipped.
 *
 * @returns {number} how many pages it reached
 */
export function deliverTo(guests, owner, channel, message) {
  let reached = 0
  for (const guest of guests) {
    if (guest.isDestroyed?.() || extensionOf(guest.getURL()) !== owner) continue
    guest.send(channel, message)
    reached += 1
  }
  return reached
}

/**
 * @param {object} options
 * @param {(url: string) => void} options.show  opens the panel at an address
 * @param {(owner: string, message: unknown) => number} options.deliver
 * @param {(owner: string) => Promise<Array<{ id: string, entry: string }>>} options.pagesFor
 *   the panel pages an Extension declared
 */
export function createPanelHub({ show, deliver, pagesFor }) {
  /** owner → the one handler its pages talk to */
  const handlers = new Map()

  return {
    /** `ctx.panel`, closed over one Extension's name. */
    capabilityFor(owner) {
      return {
        /**
         * Open the panel at one of this Extension's pages, or at a web page.
         *
         * @param {{ page?: string, url?: string }} target
         */
        async open(target = {}) {
          if (target.page !== undefined) {
            const page = (await pagesFor(owner)).find((candidate) => candidate.id === target.page)
            if (!page) return fail(`${owner} has no panel page "${target.page}".`)
            show(`${EXT_SCHEME}://${owner}/${page.entry}`)
            return { ok: true }
          }
          let url
          try {
            url = new URL(String(target.url ?? ''))
          } catch {
            return fail(`${owner}: the side panel opens a page or an http(s) address.`)
          }
          if (url.protocol !== 'http:' && url.protocol !== 'https:') {
            return fail(`${owner}: the side panel opens a page or an http(s) address.`)
          }
          show(url.href)
          return { ok: true }
        },

        /** The one function this Extension's pages are answered by. Replaces any earlier one. */
        onMessage(handler) {
          if (typeof handler !== 'function') throw new Error(`${owner}: onMessage needs a function.`)
          handlers.set(owner, handler)
        },

        /**
         * Send to this Extension's open pages. Dropped when none is open.
         *
         * @returns {{ ok: true, reached: number } | { ok: false, reason: string }}
         */
        post(message) {
          const encoded = encode(message, owner)
          if (!encoded.ok) return encoded
          return { ok: true, reached: deliver(owner, encoded.value) }
        },
      }
    },

    /**
     * A page's `window.agentrq.panel.send`, answered by its own Extension.
     *
     * @param {string} senderUrl  `event.senderFrame.url` — never the message
     */
    async handleSend(senderUrl, message) {
      const owner = extensionOf(senderUrl)
      if (!owner) return fail('Only an extension’s own pages can send to it.')
      const handler = handlers.get(owner)
      if (!handler) return fail(`${owner} does not listen to its page.`)

      const encoded = encode(message, owner)
      if (!encoded.ok) return encoded

      let reply
      try {
        reply = await handler(encoded.value)
      } catch (error) {
        return fail(`${owner} failed: ${error?.message || String(error)}`)
      }
      const answer = encode(reply ?? null, owner)
      return answer.ok ? { ok: true, reply: answer.value } : answer
    },

    /** The Extension stopped: its pages are no longer answered. */
    retract(owner) {
      handlers.delete(owner)
    },
  }
}
