// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The two things the markdown-links check must not do for real, and the one it
 * must.
 *
 * Opening a link hands a path to the system's opener: a passing run would
 * launch an editor and a file manager on whoever ran it, and a CI runner has
 * none. So the run puts a stand-in `xdg-open` first on PATH, which records its
 * argument and does nothing. Everything on either side of it is the real shell.
 * (Windows and macOS have no such seam, so their shell calls are not checked.)
 *
 * The clipboard is the real one, read back with the OS's own tool, and put
 * back as it was.
 */
import { spawn, spawnSync } from 'node:child_process'
import { chmod, mkdtemp, readFile, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { delimiter, join } from 'node:path'

/**
 * Linux only. @returns {Promise<{env: object, calls: () => Promise<string[]>}>}
 */
export async function stubOpener() {
  const dir = await mkdtemp(join(tmpdir(), 'agentrq-verify-opener-'))
  const log = join(dir, 'calls.log')
  await writeFile(log, '')
  const script = join(dir, 'xdg-open')
  await writeFile(script, `#!/bin/sh\nprintf '%s\\n' "$1" >> '${log}'\n`)
  await chmod(script, 0o755)
  return {
    env: { PATH: `${dir}${delimiter}${process.env.PATH}` },
    calls: async () => (await readFile(log, 'utf-8')).split('\n').filter(Boolean),
  }
}

const READ = {
  linux: ['xclip', ['-selection', 'clipboard', '-o']],
  darwin: ['pbpaste', []],
  win32: ['powershell', ['-NoProfile', '-Command', 'Get-Clipboard -Raw']],
}

const WRITE = {
  linux: ['xclip', ['-selection', 'clipboard', '-i']],
  darwin: ['pbcopy', []],
  win32: ['clip', []],
}

/**
 * The clipboard's text, or null when this machine has no tool to read it. An
 * empty clipboard is not that: xclip exits non-zero for one, and it reads as "".
 */
export function readClipboard() {
  const [cmd, args] = READ[process.platform] ?? []
  if (!cmd) return null
  const out = spawnSync(cmd, args, { encoding: 'utf-8' })
  return out.error ? null : out.stdout.replace(/\r?\n$/, '')
}

/** Put text on the clipboard. xclip stays up to serve it, so do not wait for it. */
export function writeClipboard(text) {
  const [cmd, args] = WRITE[process.platform] ?? []
  if (!cmd) return
  const child = spawn(cmd, args, { stdio: ['pipe', 'ignore', 'ignore'], detached: true })
  child.stdin.end(text)
  child.unref()
}
