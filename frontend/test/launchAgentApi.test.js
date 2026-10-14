// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { launchAgent } from '../src/api.js'

describe('launchAgent', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 202, json: async () => ({ session: { id: 's1' } }) }))
  })
  afterEach(() => vi.unstubAllGlobals())

  const sent = () => {
    const [url, init] = fetch.mock.calls[0]
    return { url, init, body: JSON.parse(init.body) }
  }

  it('sends the defaults for everything left out', async () => {
    expect(await launchAgent('ws1', { machineId: 'm1', kind: 'claude-code' })).toEqual({ session: { id: 's1' } })
    const { url, init, body } = sent()
    expect(url).toBe('/api/v1/workspaces/ws1/agent')
    expect(init.method).toBe('POST')
    expect(body).toEqual({
      machineId: 'm1', kind: 'claude-code', model: '', effort: '', autocompact: '', agent: '', allowUnverified: false, acpVersion: '', cols: 0, rows: 0,
    })
  })

  it('sends the consent to an unverified install', async () => {
    await launchAgent('ws1', { machineId: 'm1', kind: 'acp-gateway', agent: 'crow-cli', allowUnverified: true })
    expect(sent().body).toEqual(expect.objectContaining({ agent: 'crow-cli', allowUnverified: true }))
  })

  it('sends the native ACP version', async () => {
    await launchAgent('ws1', { machineId: 'm1', kind: 'acp-gateway', agent: 'codex-acp', acpVersion: '1' })
    expect(sent().body).toEqual(expect.objectContaining({ agent: 'codex-acp', acpVersion: '1' }))
  })
})
