// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { mkdtemp, mkdir, rm, symlink, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  MAX_PANEL_FILE_BYTES,
  createExtSchemeHandler,
  panelFolderFor,
  panelPathOf,
  readPanelFile,
} from '../../src/main/side-panel/ext-scheme.js'

let dir
let outside

/** An installed extension with a panel folder, its Node code beside it. */
async function install() {
  dir = await mkdtemp(join(tmpdir(), 'agentrq-ext-'))
  outside = await mkdtemp(join(tmpdir(), 'agentrq-outside-'))
  await mkdir(join(dir, 'panel', 'assets'), { recursive: true })
  await mkdir(join(dir, 'panel-evil'), { recursive: true })
  await writeFile(join(dir, 'index.js'), 'export function apply() {} // the Node side')
  await writeFile(join(dir, 'agentrq-extension.json'), '{}')
  await writeFile(join(dir, 'panel', 'index.html'), '<!doctype html><h1>Notes</h1>')
  await writeFile(join(dir, 'panel', 'assets', 'app.js'), 'console.log(1)')
  await writeFile(join(dir, 'panel-evil', 'x.html'), 'evil')
  await writeFile(join(outside, 'secret.txt'), 'secret')
  await symlink(join(outside, 'secret.txt'), join(dir, 'panel', 'link.txt'))
  return {
    name: 'notes',
    dir,
    enabled: true,
    manifest: { provides: { panels: [{ id: 'board', label: 'Board', entry: 'panel/index.html' }] } },
  }
}

let installation
beforeEach(async () => { installation = await install() })
afterEach(async () => {
  await rm(dir, { recursive: true, force: true })
  await rm(outside, { recursive: true, force: true })
})

const handler = (list = () => [installation]) => createExtSchemeHandler({ installations: async () => list() })
const get = (url, h = handler()) => h({ url })

describe('panelPathOf', () => {
  it('reads the file a URL names', () => {
    expect(panelPathOf(new URL('agentrq-ext://notes/panel/index.html'))).toBe('panel/index.html')
    expect(panelPathOf(new URL('agentrq-ext://notes/panel/my%20page.html'))).toBe('')
    expect(panelPathOf(new URL('agentrq-ext://notes/panel/a%2Db.html'))).toBe('panel/a-b.html')
  })

  it('names nothing for a path that could leave the folder, or is hidden', () => {
    for (const path of ['/', '/panel/.secret', '/panel/a%5Cb.html', '/panel/%E0%A4%A', '/panel/%2e%2e%2findex.js']) {
      expect(panelPathOf(new URL(`agentrq-ext://notes${path}`)), path).toBe('')
    }
  })

  it('sees an encoded `..` already resolved by the URL, where the folder check catches it', () => {
    expect(panelPathOf(new URL('agentrq-ext://notes/panel/%2e%2e/index.js'))).toBe('index.js')
  })
})

describe('panelFolderFor', () => {
  it('finds the declared folder a file is in', () => {
    expect(panelFolderFor(installation, 'panel/assets/app.js')).toBe('panel')
  })

  it('is no folder for the rest of the package, or a sibling that only starts the same', () => {
    expect(panelFolderFor(installation, 'index.js')).toBe('')
    expect(panelFolderFor(installation, 'panel-evil/x.html')).toBe('')
    expect(panelFolderFor({ manifest: {} }, 'panel/index.html')).toBe('')
    expect(panelFolderFor(undefined, 'panel/index.html')).toBe('')
  })
})

describe('the agentrq-ext handler', () => {
  it('serves a declared page and the files beside it', async () => {
    const page = await get('agentrq-ext://notes/panel/index.html')
    expect(page.status).toBe(200)
    expect(page.headers.get('content-type')).toBe('text/html; charset=utf-8')
    expect(page.headers.get('cache-control')).toBe('no-cache')
    expect(await page.text()).toContain('<h1>Notes</h1>')

    const script = await get('agentrq-ext://notes/panel/assets/app.js')
    expect(script.headers.get('content-type')).toBe('text/javascript; charset=utf-8')
  })

  it('never serves the extension’s own code or manifest', async () => {
    for (const path of ['index.js', 'agentrq-extension.json', 'panel-evil/x.html', 'panel/%2e%2e/index.js']) {
      const response = await get(`agentrq-ext://notes/${path}`)
      expect(response.status, path).toBe(404)
      expect(await response.text()).toBe('That file is not in a panel folder of this extension.')
    }
  })

  it('refuses a symlink that points out of the folder', async () => {
    const response = await get('agentrq-ext://notes/panel/link.txt')
    expect(response.status).toBe(404)
    expect(await response.text()).toBe('That file is not in a panel folder of this extension.')
  })

  it('says so for a file that is not there, or a folder', async () => {
    expect(await (await get('agentrq-ext://notes/panel/gone.html')).text()).toBe('No such file in this extension.')
    expect(await (await get('agentrq-ext://notes/panel/assets')).text()).toBe('That is not a file.')
  })

  it('serves nothing for an extension that is not installed, or is turned off', async () => {
    expect(await (await get('agentrq-ext://other/panel/index.html')).text()).toBe('That extension is not installed, or is turned off.')
    const off = handler(() => [{ ...installation, enabled: false }])
    expect((await get('agentrq-ext://notes/panel/index.html', off)).status).toBe(404)
  })

  it('answers only its own scheme', async () => {
    expect(await (await get('https://notes/panel/index.html')).text()).toBe('Not an extension page.')
  })

  it('refuses a file too large to serve', async () => {
    const read = (d, f, p) => readPanelFile(d, f, p, {
      realpath: async (p2) => p2,
      lstat: async () => ({ isFile: () => true, size: MAX_PANEL_FILE_BYTES + 1 }),
      readFile: vi.fn(),
    })
    const h = createExtSchemeHandler({ installations: async () => [installation], read })
    const response = await get('agentrq-ext://notes/panel/index.html', h)
    expect(response.status).toBe(413)
    expect(await response.text()).toBe('That file is too large to serve.')
  })
})

describe('readPanelFile', () => {
  it('says a file cannot be read when the disk refuses', async () => {
    const denied = Object.assign(new Error('EACCES: permission denied'), { code: 'EACCES' })
    expect(await readPanelFile(dir, 'panel', 'panel/index.html', { realpath: async () => { throw denied } }))
      .toEqual({ ok: false, status: 404, reason: 'That file cannot be read.' })
    expect(await readPanelFile(dir, 'panel', 'panel/index.html', {
      realpath: async (p) => p,
      lstat: async () => { throw denied },
    })).toEqual({ ok: false, status: 404, reason: 'That file cannot be read.' })
  })
})
