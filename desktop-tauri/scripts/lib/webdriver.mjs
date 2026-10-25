// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * A minimal W3C WebDriver client, and the launcher for `tauri-driver`.
 *
 * Dependency-free on purpose: the verify scripts need a dozen commands, and a
 * client library would be one more package that has to start at its latest
 * release and stay there.
 *
 * Every wait here polls a condition up to a deadline. Nothing sleeps for a
 * fixed time and hopes: a slow runner makes a wait longer, never a check wrong.
 */
import { spawn } from 'node:child_process'

/** No single WebDriver command may take longer: a hung driver is an error, not a wait. */
const COMMAND_TIMEOUT = 90000

export const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))

/** What the run is waiting for now, so a run that hangs can say where. */
export const pending = { what: 'nothing' }

/** Poll `probe` until it returns something truthy, or throw once `timeout` passes. */
export async function waitFor(what, probe, { timeout = 30000, interval = 150 } = {}) {
  const deadline = Date.now() + timeout
  let last
  pending.what = what
  for (;;) {
    try {
      const value = await probe()
      if (value) return value
      last = undefined
    } catch (err) {
      last = err
    }
    if (Date.now() > deadline) {
      throw new Error(`timed out after ${timeout}ms waiting for ${what}${last ? `: ${last.message}` : ''}`)
    }
    await sleep(interval)
  }
}

/**
 * Start `tauri-driver` and wait until it answers `/status`.
 *
 * It starts the native driver (WebKitWebDriver, msedgedriver) and the app, and
 * the app inherits this environment, which is how a run points the app at its
 * own data folder.
 */
export async function startDriver({ port, env, nativeDriver } = {}) {
  const args = ['--port', String(port)]
  if (nativeDriver) args.push('--native-driver', nativeDriver)
  const child = spawn('tauri-driver', args, { env: { ...process.env, ...env }, stdio: ['ignore', 'inherit', 'inherit'] })
  let exited = null
  child.once('error', (err) => {
    exited = err
  })
  child.once('exit', (code) => {
    exited = new Error(`tauri-driver exited with ${code}`)
  })
  const base = `http://127.0.0.1:${port}`
  await waitFor(
    'tauri-driver to answer /status',
    async () => {
      if (exited) throw exited
      return (await fetch(`${base}/status`)).ok
    },
    { timeout: 20000 }
  ).catch((err) => {
    child.kill()
    throw err
  })
  return { base, stop: () => child.kill() }
}

export class Session {
  constructor(base, id) {
    this.base = base
    this.id = id
  }

  /** Open a session that launches `application`. */
  static async create(base, application) {
    pending.what = 'the app to start (POST /session)'
    const res = await fetch(`${base}/session`, {
      signal: AbortSignal.timeout(COMMAND_TIMEOUT),
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        capabilities: { alwaysMatch: { browserName: 'wry', 'tauri:options': { application } } },
      }),
    })
    const body = await res.json()
    if (!res.ok) throw new Error(`cannot start a session: ${JSON.stringify(body.value)}`)
    return new Session(base, body.value.sessionId)
  }

  async command(method, path, payload) {
    pending.what = `${method} ${path}`
    const res = await fetch(`${this.base}/session/${this.id}${path}`, {
      method,
      headers: { 'Content-Type': 'application/json' },
      body: payload === undefined ? undefined : JSON.stringify(payload),
      signal: AbortSignal.timeout(COMMAND_TIMEOUT),
    })
    const body = await res.json()
    if (!res.ok) throw new Error(`${method} ${path}: ${body.value?.error}: ${body.value?.message}`)
    return body.value
  }

  windowHandles() {
    return this.command('GET', '/window/handles')
  }

  switchToWindow(handle) {
    return this.command('POST', '/window', { handle })
  }

  /**
   * Run `expression` in a page whose answer makes the app replace its main
   * window (a save, a profile switch), and wait for the window that replaces
   * it. WebKitWebDriver keeps the window handle across the swap, so the old
   * page is told apart from the new one by a marker only the old one has.
   *
   * `ready` is what must hold in the new page before the caller goes on: the
   * app has mounted, by default.
   *
   * The old page may be gone before it can answer: the result is then
   * `undefined`, and the caller decides what that means.
   */
  async replaceWindow(expression, ready = "!!document.querySelector('#app')?.children.length") {
    await this.evaluate('(window.__verifyReplaced = true)')
    let result
    try {
      result = await this.evaluate(expression)
    } catch {
      // The page went with its window.
    }
    await waitFor('the replacement window', async () => {
      const [handle] = await this.windowHandles()
      // Between the old window going and the new one coming there is none.
      if (!handle) return false
      await this.switchToWindow(handle)
      return this.evaluate(
        `!window.__verifyReplaced && document.readyState === 'complete' && !!window.agentrq && (${ready})`
      )
    })
    return result
  }

  url() {
    return this.command('GET', '/url')
  }

  /**
   * Run `expression` in the page and return what it resolves to. The
   * expression may be async: it is awaited inside the page, and a throw comes
   * back as an error here with the page's own message.
   */
  async evaluate(expression) {
    const outcome = await this.command('POST', '/execute/async', {
      script: `const done = arguments[arguments.length - 1];
        Promise.resolve().then(() => (${expression}))
          .then((value) => done({ value }), (err) => done({ error: String((err && err.stack) || err) }));`,
      args: [],
    })
    if (outcome.error) throw new Error(`in the page: ${outcome.error}`)
    return outcome.value
  }

  /** Wait until `expression` is truthy in the page. */
  waitForPage(what, expression, options) {
    return waitFor(what, () => this.evaluate(expression), options)
  }

  async quit() {
    await this.command('DELETE', '').catch(() => {})
  }
}
