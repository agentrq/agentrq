// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The three things the markdown-links check must not do for real, and the one
 * it must.
 *
 * Opening a link hands a path to the system's opener, and revealing one asks
 * the file manager: a passing run would launch an editor and a file manager on
 * whoever ran it, and a CI runner has none. So the run stands in for both
 * (Linux only; Windows and macOS have no such seam, so their shell calls are
 * not checked):
 *
 * - a stand-in `xdg-open` first on PATH, which records its argument;
 * - a private session bus, with a stand-in `org.freedesktop.FileManager1` on it
 *   that records what `ShowItems` is asked to show. The app is pointed at that
 *   bus alone, so the person's own file manager and bus are never reached, and
 *   neither is their running AgentRQ (single-instance is keyed on the bus).
 *
 * Everything on either side of the stand-ins is the real shell.
 *
 * The clipboard is the real one, read back with the OS's own tool, and put
 * back as it was.
 */
import { spawn, spawnSync } from 'node:child_process'
import { chmod, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { delimiter, join } from 'node:path'

// The file manager's side of ShowItems, on GLib's own D-Bus, which any desktop
// Linux (and ubuntu-latest) has through python3-gi.
const FILE_MANAGER = `
import sys
import gi
gi.require_version('Gio', '2.0')
from gi.repository import Gio, GLib

XML = """<node><interface name="org.freedesktop.FileManager1">
  <method name="ShowItems"><arg type="as" name="uris" direction="in"/><arg type="s" name="startup_id" direction="in"/></method>
</interface></node>"""

def on_call(conn, sender, path, iface, method, params, invocation):
    with open(sys.argv[1], 'a') as log:
        log.write('\\n'.join(params.unpack()[0]) + '\\n')
    invocation.return_value(None)

def on_bus(conn, name):
    conn.register_object('/org/freedesktop/FileManager1', Gio.DBusNodeInfo.new_for_xml(XML).interfaces[0], on_call, None, None)

def on_name(conn, name):
    print('ready', flush=True)

Gio.bus_own_name(Gio.BusType.SESSION, 'org.freedesktop.FileManager1', Gio.BusNameOwnerFlags.NONE, on_bus, on_name, None)
GLib.MainLoop().run()
`

/** The first line a child prints, or null if it exits (or the wait runs out) first. */
function firstLine(child) {
  return new Promise((resolve) => {
    let seen = ''
    const timer = setTimeout(() => resolve(null), 10000)
    const done = (line) => {
      clearTimeout(timer)
      resolve(line)
    }
    child.stdout.on('data', (chunk) => {
      seen += chunk
      if (seen.includes('\n')) done(seen.split('\n')[0])
    })
    child.once('exit', () => done(null))
  })
}

/**
 * Linux only.
 * @returns {Promise<{
 *   env: object,
 *   calls: () => Promise<string[]>,
 *   reveals: (() => Promise<string[]>) | null,
 *   whyNoReveals: string | null,
 *   stop: () => Promise<void>,
 * }>}
 */
export async function stubShell() {
  const dir = await mkdtemp(join(tmpdir(), 'agentrq-verify-opener-'))
  const log = join(dir, 'calls.log')
  const revealLog = join(dir, 'reveals.log')
  await writeFile(log, '')
  await writeFile(revealLog, '')
  const script = join(dir, 'xdg-open')
  await writeFile(script, `#!/bin/sh\nprintf '%s\\n' "$1" >> '${log}'\n`)
  await chmod(script, 0o755)

  const children = []
  const read = async (path) => (await readFile(path, 'utf-8')).split('\n').filter(Boolean)
  const env = { PATH: `${dir}${delimiter}${process.env.PATH}` }
  let reveals = null
  let whyNoReveals = null

  const bus = spawn('dbus-daemon', ['--session', '--nofork', '--print-address=1'], { stdio: ['ignore', 'pipe', 'ignore'] })
  bus.once('error', () => {})
  children.push(bus)
  const address = await firstLine(bus)
  if (!address) whyNoReveals = 'dbus-daemon would not start a private session bus'
  else {
    const manager = spawn('python3', ['-c', FILE_MANAGER, revealLog], {
      env: { ...process.env, DBUS_SESSION_BUS_ADDRESS: address },
      stdio: ['ignore', 'pipe', 'ignore'],
    })
    manager.once('error', () => {})
    children.push(manager)
    if ((await firstLine(manager)) === 'ready') {
      env.DBUS_SESSION_BUS_ADDRESS = address
      reveals = () => read(revealLog)
    } else whyNoReveals = 'no python3 with GLib bindings (python3-gi) for the stand-in file manager'
  }

  return {
    env,
    calls: () => read(log),
    reveals,
    whyNoReveals,
    stop: async () => {
      for (const child of children) child.kill()
      await rm(dir, { recursive: true, force: true }).catch(() => {})
    },
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
