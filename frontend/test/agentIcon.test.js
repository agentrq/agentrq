// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect } from 'vitest'
import { readdirSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { agentIconFor, agentIconFiles } from '../src/composables/useAgentIcon'

describe('agentIconFor', () => {
  it('finds an agent by a word of the name it gave itself', () => {
    expect(agentIconFor('claude-code').src).toContain('claude-agent')
    expect(agentIconFor('codex-mcp-client').src).toContain('codex-cli')
    // Cut to the 16 characters the history keeps.
    expect(agentIconFor('gemini-cli-mcp-c').src).toContain('gemini-cli')
    expect(agentIconFor('GitHub Copilot').svg).toContain('<svg')
  })

  it('matches a name of several words only as those words, in order', () => {
    expect(agentIconFor('fast-agent-acp').src).toContain('fast-agent')
    expect(agentIconFor('agent-fast')).toBeNull()
    expect(agentIconFor('vtcode').svg).toContain('<svg')
  })

  it('matches a short name only as a whole word', () => {
    expect(agentIconFor('pi').svg).toContain('<svg')
    expect(agentIconFor('pilot')).toBeNull()
    expect(agentIconFor('example-amp-ish').svg).toContain('<svg')
    expect(agentIconFor('sample')).toBeNull()
  })

  it('has no icon for an agent it does not know, or no name', () => {
    expect(agentIconFor('my-own-agent')).toBeNull()
    expect(agentIconFor('')).toBeNull()
    expect(agentIconFor(null)).toBeNull()
    expect(agentIconFor(undefined)).toBeNull()
  })

  it('points at every icon in the folder, and only at icons that are there', () => {
    const folder = readdirSync(resolve(process.cwd(), 'src/assets/agent-icons')).sort()
    expect([...agentIconFiles].sort()).toEqual(folder)
    // Each file's own name finds that file, with its content or its URL.
    for (const file of agentIconFiles) {
      const icon = agentIconFor(file.replace(/\.(svg|png)$/, ''))
      if (file.endsWith('.svg')) {
        expect(icon?.svg, file).toBe(readFileSync(resolve(process.cwd(), 'src/assets/agent-icons', file), 'utf8'))
      } else {
        expect(icon?.src, file).toContain(`/agent-icons/${file}`)
      }
    }
  })
})
