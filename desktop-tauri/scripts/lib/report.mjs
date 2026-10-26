// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { pending } from './webdriver.mjs'

/**
 * What every verify script prints and exits with, as the Electron ones do:
 * ✓ / ✗ per check, a summary line, exit 0 only when nothing failed.
 */

/** A check that cannot run yet. Printed, never counted as a pass or a failure. */
export const skip = (name, reason) => ({ name, skipped: true, detail: reason })

/** A page's text can be a whole screen: keep a line to a line. */
const shorten = (text) => (String(text).length > 160 ? `${String(text).slice(0, 157)}...` : text)

/** Print one titled block of results. */
export function printResults(title, results) {
  console.log(`\n─── ${title} ───`)
  for (const r of results) {
    const mark = r.skipped ? '○' : r.pass ? '✓' : '✗'
    console.log(`${mark} ${r.name} — ${shorten(r.detail)}${r.skipped ? ' (skipped)' : ''}`)
  }
}

/** The summary line; returns the exit code. */
export function summarize(...blocks) {
  const all = blocks.flat()
  const failed = all.filter((r) => !r.skipped && !r.pass)
  const skipped = all.filter((r) => r.skipped)
  const note = skipped.length ? ` (${skipped.length} skipped)` : ''
  console.log(failed.length === 0 ? `\n✓ all checks passed${note}` : `\n✗ ${failed.length} check(s) failed${note}`)
  return failed.length === 0 ? 0 : 1
}

/**
 * Run `main` under a deadline. A script that hangs is worse than one that
 * fails: it blocks a run with no signal at all. `cleanup` always runs.
 */
export async function runVerify(main, cleanup, { timeout = 300000 } = {}) {
  const timer = setTimeout(async () => {
    console.error(`✗ verification timed out, waiting for: ${pending.what}`)
    await cleanup().catch(() => {})
    process.exit(3)
  }, timeout)
  let code = 1
  try {
    code = await main()
  } catch (err) {
    console.error('✗ verification threw:', (err && err.stack) || err)
  }
  clearTimeout(timer)
  await cleanup().catch(() => {})
  process.exit(code)
}
