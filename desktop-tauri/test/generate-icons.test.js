// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const SCRIPT = fileURLToPath(new URL('../scripts/generate-icons.mjs', import.meta.url))
const ICONS = path.join(path.dirname(SCRIPT), '..', 'src-tauri', 'icons')

const spawnSync = vi.fn()
const rm = vi.fn(async () => {})
const glob = vi.fn()

vi.mock('node:child_process', () => ({ spawnSync: (...a) => spawnSync(...a) }))
vi.mock('node:fs/promises', () => ({ rm: (...a) => rm(...a), glob: (...a) => glob(...a) }))

describe('generateIcons', () => {
  const argv = process.argv

  beforeEach(() => {
    vi.resetModules()
    spawnSync.mockReset()
    rm.mockClear()
    glob.mockReset()
  })

  afterEach(() => {
    process.argv = argv
  })

  it('runs tauri icon, then removes the mobile and store icons', async () => {
    spawnSync.mockReturnValue({ status: 0 })
    glob.mockImplementation(async function* () {
      yield path.join(ICONS, 'Square30x30Logo.png')
      yield path.join(ICONS, 'StoreLogo.png')
    })
    const { generateIcons } = await import('../scripts/generate-icons.mjs')
    await generateIcons()

    const [cmd, args, opts] = spawnSync.mock.calls[0]
    expect([cmd, args.slice(0, 2), args.slice(3)]).toEqual(['npx', ['tauri', 'icon'], ['-o', ICONS]])
    expect(args[2]).toMatch(/frontend[\\/]public[\\/]large-icon\.png$/)
    expect(opts.stdio).toBe('inherit')
    expect(glob).toHaveBeenCalledWith(path.join(ICONS, '{Square*,Store*}'))
    expect(rm.mock.calls).toEqual([
      [path.join(ICONS, 'android'), { recursive: true, force: true }],
      [path.join(ICONS, 'ios'), { recursive: true, force: true }],
      [path.join(ICONS, 'Square30x30Logo.png'), { force: true }],
      [path.join(ICONS, 'StoreLogo.png'), { force: true }],
    ])
  })

  it('throws, removing nothing, when tauri icon fails', async () => {
    spawnSync.mockReturnValue({ status: 2 })
    const { generateIcons } = await import('../scripts/generate-icons.mjs')
    await expect(generateIcons()).rejects.toThrow('tauri icon failed with exit code 2')
    expect(rm).not.toHaveBeenCalled()
  })

  it('generates the icons when run as a script', async () => {
    spawnSync.mockReturnValue({ status: 0 })
    glob.mockImplementation(async function* () {})
    process.argv = [argv[0], SCRIPT]
    await import('../scripts/generate-icons.mjs')
    expect(spawnSync).toHaveBeenCalledTimes(1)
  })
})

