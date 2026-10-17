// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { agentIsWorking } from '../src/composables/useAgentTurn'

// The native ACP engine posts these notes itself, and the composer knows a
// turn has ended by their exact words. Read from the Go source, so the two
// cannot drift.
const source = readFileSync(resolve(__dirname, '../../backend/internal/controller/acpv1/notes.go'), 'utf8')
const notes = [...source.matchAll(/note[A-Za-z]+\s*=\s*"(⚠️[^"]*)"/gu)].map((m) => m[1])

// A prefix gets the rest a real note would carry.
const completed = (note) => (note.endsWith(': ') ? `${note}other.` : note.endsWith('(') ? `${note}x).` : note)

describe('the notes ACP v1 ends a turn with', () => {
  it('reads every note from the Go source', () => {
    expect(notes.length).toBe(9)
  })

  for (const note of notes) {
    const text = completed(note)
    // A footer from an earlier turn: a plain reply no longer ends one, so
    // only the note can.
    it(`ends the turn: ${text}`, () => {
      const messages = [
        { id: 'h1', sender: 'human', text: 'go', createdAt: '2026-10-12T10:00:00Z' },
        { id: 'u1', sender: 'agent', metadata: { type: 'agent_usage' } },
        { id: 'h2', sender: 'human', text: 'again', createdAt: '2026-10-12T10:01:00Z' },
        { id: 'n', sender: 'agent', text, createdAt: '2026-10-12T10:01:05Z' },
      ]
      expect(agentIsWorking({ task: { status: 'ongoing', assignee: 'agent' }, workspace: { agentSupportsStop: true }, messages })).toBe(false)
    })
  }
})
