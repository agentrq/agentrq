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
import { createServer } from 'node:net'

/** No single WebDriver command may take longer: a hung driver is an error, not a wait. */
const COMMAND_TIMEOUT = 90000

export const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))

/** A port the OS says is free right now. */
export function freePort() {
  return new Promise((resolvePort, reject) => {
    const probe = createServer()
    probe.once('error', reject)
    probe.listen(0, '127.0.0.1', () => {
      const { port } = probe.address()
      probe.close(() => resolvePort(port))
    })
  })
}

/** What the run is waiting for now, so a run that hangs can say where. */
export const pending = { what: 'nothing' }

/** Poll `probe` until it returns something truthy, or throw once `timeout` passes. */
export async function waitFor(what, probe, { timeout = 30000, interval = 150 } = {}) {
  const started = Date.now()
  const deadline = started + timeout
  const callError = new Error()
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
      const elapsed = Date.now() - started
      const err = new Error(
        `timed out after ${elapsed}ms (timeout: ${timeout}ms) waiting for ${what}${last ? `: ${last.message}` : ''}`
      )
      if (callError.stack) {
        err.stack = `${err.name}: ${err.message}\n` + callError.stack.split('\n').slice(2).join('\n')
      }
      throw err
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
  // Both ports are picked, not fixed: a driver still exiting from the last
  // script, or anything else on 4444/4445, would otherwise answer in its place.
  port ??= await freePort()
  const args = ['--port', String(port), '--native-port', String(await freePort())]
  if (nativeDriver) args.push('--native-driver', nativeDriver)
  const child = spawn('tauri-driver', args, {
    env: { ...process.env, ...env },
    stdio: ['ignore', 'inherit', 'inherit'],
    detached: process.platform !== 'win32',
  })
  let exited = null
  const gone = new Promise((resolveGone) => {
    child.once('exit', resolveGone)
    child.once('error', resolveGone)
  })
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
      return (await fetch(`${base}/status`, { signal: AbortSignal.timeout(5000) })).ok
    },
    { timeout: 30000 }
  ).catch((err) => {
    if (process.platform !== 'win32' && child.pid) {
      try { process.kill(-child.pid, 'SIGCONT') } catch {}
      try { process.kill(-child.pid, 'SIGKILL') } catch {}
    } else {
      child.kill()
    }
    throw err
  })
  return {
    base,
    // Resolves once the driver, and so the port, is really gone.
    stop: async () => {
      if (process.platform !== 'win32' && child.pid) {
        try { process.kill(-child.pid, 'SIGCONT') } catch {}
        try { process.kill(-child.pid, 'SIGTERM') } catch {}
        setTimeout(() => {
          try { process.kill(-child.pid, 'SIGKILL') } catch {}
        }, 2000).unref()
      } else {
        try { child.kill() } catch {}
      }
      return gone
    },
  }
}

export class Session {
  constructor(base, id) {
    this.base = base
    this.id = id
  }

  static async request(method, url, payload, timeout = COMMAND_TIMEOUT) {
    const started = Date.now()
    const callError = new Error()
    let body
    let ok = false
    try {
      const res = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: payload === undefined ? undefined : JSON.stringify(payload),
        signal: AbortSignal.timeout(timeout),
      })
      ok = res.ok
      body = await res.json().catch(() => null)
    } catch (err) {
      const elapsed = Date.now() - started
      if (err.name === 'TimeoutError' || err.name === 'AbortError') {
        const target = `${method} ${url}`
        const timeoutErr = new Error(
          `WebDriver command timed out after ${elapsed}ms (timeout: ${timeout}ms) for ${target}`,
          { cause: err }
        )
        if (callError.stack) {
          timeoutErr.stack = `${timeoutErr.name}: ${timeoutErr.message}\n` +
            callError.stack.split('\n').slice(2).join('\n')
        }
        throw timeoutErr
      }
      throw err
    }
    if (!ok) {
      const detail = body?.value?.message || body?.value?.error || JSON.stringify(body) || 'unknown error'
      const err = new Error(`${method} ${url}: ${detail}`)
      if (callError.stack) {
        err.stack = `${err.name}: ${err.message}\n` + callError.stack.split('\n').slice(2).join('\n')
      }
      throw err
    }
    return body?.value
  }

  /** Open a session that launches `application`. */
  static async create(base, application) {
    pending.what = 'the app to start (POST /session)'
    // App startup outer bound (120s) accounts for cold cache and stalls.
    const value = await Session.request('POST', `${base}/session`, {
      capabilities: { alwaysMatch: { browserName: 'wry', 'tauri:options': { application } } },
    }, 120000)
    const session = new Session(base, value.sessionId)
    // Align WebDriver's own timeouts: 60s for script evaluation, 300s for page load.
    await session.command('POST', '/timeouts', { script: 60000, pageLoad: 300000, implicit: 0 })
    return session
  }

  async command(method, path, payload, timeout) {
    pending.what = `${method} ${path}`
    if (timeout === undefined) {
      if (path === '/execute/async') timeout = 90000
      else if (path === '/url' && method === 'POST') timeout = 330000
      else timeout = 60000
    }
    return Session.request(method, `${this.base}/session/${this.id}${path}`, payload, timeout)
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
          .then((value) => done({ value }), (err) => {
            const msg = err && (err.message && !String(err.stack || '').includes(err.message) ? String(err) + '\\n' + err.stack : (err.stack || String(err)));
            done({ error: String(msg || err) });
          });`,
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
    await this.command('DELETE', '', undefined, 10000).catch(() => {})
  }
}
