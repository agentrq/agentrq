// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi } from 'vitest'

import { NOTES_KEY, answer, appendLine, apply, inject, name, noteLine } from '../../../examples/extensions/panel-notes/index.js'
import { parseManifest } from '../../src/main/extensions/manifest.js'
import { buildContext } from '../../src/main/extensions/host.js'
import { createRegistries } from '../../src/main/extensions/registry.js'
import manifest from '../../../examples/extensions/panel-notes/agentrq-extension.json'

/**
 * The extension with a page of its own. What matters here is the split it
 * teaches: the page draws and asks, the extension keeps the data and answers.
 */

/** `ctx.storage`, in memory. */
function memoryStorage(initial = {}) {
  const held = new Map(Object.entries(initial))
  return { get: async (key) => held.get(key), set: async (key, value) => { held.set(key, value); return { ok: true } }, held }
}

/** A real context from the host, with a panel capability that records what it is asked. */
function context(storage = memoryStorage()) {
  const panel = { onMessage: vi.fn(), post: vi.fn(() => ({ ok: true, reached: 0 })), open: vi.fn(async () => ({ ok: true })) }
  const { ctx } = buildContext({
    name,
    registries: createRegistries(),
    capabilities: { panel: () => panel },
    inject,
    config: {},
    logger: { info() {}, warn() {} },
    storage,
  })
  return { ctx, panel, storage }
}

describe('the manifest', () => {
  it('is one, and declares its page in a folder of its own', () => {
    const parsed = parseManifest(manifest)
    expect(parsed.ok).toBe(true)
    expect(parsed.manifest.provides.panels).toEqual([{ id: 'notes', label: 'Notes', entry: 'panel/index.html' }])
  })

  it('asks for no permissions', () => {
    const { manifest: parsed } = parseManifest(manifest)
    expect(parsed.mcp).toEqual({ workspace: [], supervisor: [] })
    expect(parsed.net).toEqual([])
  })
})

describe('noteLine and appendLine', () => {
  it('writes a task as a line', () => {
    expect(noteLine({ title: '  Ship it ' })).toBe('- Ship it')
    expect(noteLine({})).toBe('- Untitled task')
    expect(noteLine(undefined)).toBe('- Untitled task')
  })

  it('adds a line without leaving a blank one', () => {
    expect(appendLine('', '- a')).toBe('- a')
    expect(appendLine(undefined, '- a')).toBe('- a')
    expect(appendLine('- a', '- b')).toBe('- a\n- b')
    expect(appendLine('- a\n', '- b')).toBe('- a\n- b')
  })
})

describe('answering the page', () => {
  it('loads what was kept, and nothing the first time', async () => {
    expect(await answer(memoryStorage(), { type: 'load' })).toEqual({ text: '' })
    expect(await answer(memoryStorage({ [NOTES_KEY]: 'kept' }), { type: 'load' })).toEqual({ text: 'kept' })
  })

  it('saves what was typed', async () => {
    const storage = memoryStorage()
    expect(await answer(storage, { type: 'save', text: 'hello' })).toEqual({ saved: true })
    expect(storage.held.get(NOTES_KEY)).toBe('hello')
    await answer(storage, { type: 'save' })
    expect(storage.held.get(NOTES_KEY)).toBe('')
  })

  it('refuses anything else, by name', async () => {
    await expect(answer(memoryStorage(), { type: 'delete-everything' })).rejects.toThrow('Notes does not know "delete-everything".')
    await expect(answer(memoryStorage(), undefined)).rejects.toThrow('Notes does not know "nothing".')
  })
})

describe('apply', () => {
  it('answers its page through ctx.panel', async () => {
    const { ctx, panel } = context(memoryStorage({ [NOTES_KEY]: 'kept' }))
    apply(ctx)
    const handler = panel.onMessage.mock.calls[0][0]
    expect(await handler({ type: 'load' })).toEqual({ text: 'kept' })
  })

  it('adds a task to the notes from its menu, tells an open page, and opens the page', async () => {
    const storage = memoryStorage({ [NOTES_KEY]: '- earlier' })
    const { ctx, panel } = context(storage)
    const registries = createRegistries()
    const { ctx: withRegistries } = buildContext({
      name, registries, capabilities: { panel: () => panel }, inject, config: {}, logger: console, storage,
    })
    apply(withRegistries)
    const [item] = registries.ui.listFor(name)
    expect(item).toMatchObject({ id: 'add-to-notes', surface: 'task-menu', label: 'Add to notes' })

    expect(await item.run({ title: 'Ship it' })).toBeUndefined()
    expect(storage.held.get(NOTES_KEY)).toBe('- earlier\n- Ship it')
    expect(panel.post).toHaveBeenCalledWith({ type: 'changed', text: '- earlier\n- Ship it' })
    expect(panel.open).toHaveBeenCalledWith({ page: 'notes' })
    expect(ctx.panel).toBe(panel)
  })
})
