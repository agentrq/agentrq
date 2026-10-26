// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The extension host's end of its wire to the shell: JSON-RPC 2.0, one
 * message per line. The shell's end, and why, is `core/src/ext_rpc.rs`.
 *
 * Every request this side makes is raced against a deadline: a shell that
 * never answers is an extension call that hangs, and an extension waiting on
 * it is a screen that never settles.
 */

export const METHOD_NOT_FOUND = -32601
export const FAILED = -32000

/** As `ext_rpc.rs`'s `MAX_LINE_BYTES`, counted in characters here. */
export const MAX_LINE = 40 * 1024 * 1024

/** How long this side waits for the shell, by default. */
export const DEADLINE_MS = 120_000

/**
 * @param {object} deps
 * @param {(line: string) => void} deps.write  Sends one line, newline included.
 * @param {Record<string, (params: any) => any>} [deps.handlers]  What the shell may ask.
 * @param {Record<string, (params: any) => void>} [deps.notifications]  What the shell may tell.
 * @param {(line: string) => void} [deps.onStray]  A line that is not a message.
 */
export function createPeer({
  write,
  handlers = {},
  notifications = {},
  onStray = () => {},
  deadlineMs = DEADLINE_MS,
  setTimer = setTimeout,
  clearTimer = clearTimeout,
}) {
  let nextId = 0
  const pending = new Map()
  let held = ''
  let skipping = false
  let closed = null

  const send = (message) => write(`${JSON.stringify({ jsonrpc: '2.0', ...message })}\n`)

  async function answer(id, method, params) {
    const handler = Object.hasOwn(handlers, method) ? handlers[method] : null
    if (!handler) {
      send({ id, error: { code: METHOD_NOT_FOUND, message: `No such method: ${method}` } })
      return
    }
    try {
      const result = await handler(params)
      send({ id, result: result ?? null })
    } catch (error) {
      send({ id, error: { code: FAILED, message: error?.message || String(error) } })
    }
  }

  function dispatch(line) {
    let message
    try {
      message = JSON.parse(line)
    } catch {
      onStray(line)
      return
    }
    if (message?.jsonrpc !== '2.0') {
      onStray(line)
      return
    }
    const { id, method, params } = message
    if (typeof method === 'string' && id !== undefined) {
      answer(id, method, params)
    } else if (typeof method === 'string') {
      if (Object.hasOwn(notifications, method)) notifications[method](params)
    } else if (pending.has(id)) {
      const { resolve, reject, timer } = pending.get(id)
      pending.delete(id)
      clearTimer(timer)
      if (message.error) reject(new Error(message.error.message || 'The shell failed without saying why.'))
      else resolve(message.result ?? null)
    } else {
      onStray(line)
    }
  }

  return {
    /** Takes text read off the wire, in chunks of any size. */
    receive(chunk) {
      held += chunk
      let end
      while ((end = held.indexOf('\n')) !== -1) {
        const line = held.slice(0, end)
        held = held.slice(end + 1)
        if (skipping) {
          skipping = false
          continue
        }
        if (Buffer.byteLength(line, 'utf8') > MAX_LINE) {
          onStray('(a line longer than the limit was dropped)')
          continue
        }
        if (line.trim()) dispatch(line)
      }
      if (skipping) {
        held = ''
        return
      }
      if (Buffer.byteLength(held, 'utf8') > MAX_LINE) {
        held = ''
        skipping = true
        onStray('(a line longer than the limit was dropped)')
      }
    },

    /** Ask the shell, racing its answer against a deadline. */
    request(method, params = {}, { timeoutMs = deadlineMs } = {}) {
      if (closed) return Promise.reject(new Error(closed))
      const id = ++nextId
      return new Promise((resolve, reject) => {
        const timer = setTimer(() => {
          pending.delete(id)
          reject(new Error(`The app did not answer "${method}" in time.`))
        }, timeoutMs)
        pending.set(id, { resolve, reject, timer })
        send({ id, method, params })
      })
    },

    /** Tell the shell something; nothing is answered. */
    notify(method, params = {}) {
      if (!closed) send({ method, params })
    },

    /** The wire is gone: everything still waiting fails, and so does anything asked later. */
    close(reason = 'The app closed the extension host.') {
      closed = reason
      for (const [id, { reject, timer }] of pending) {
        pending.delete(id)
        clearTimer(timer)
        reject(new Error(reason))
      }
    },
  }
}
