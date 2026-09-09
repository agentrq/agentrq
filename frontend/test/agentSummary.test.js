import { describe, it, expect } from 'vitest'

import {
  agentClientName,
  agentDetails,
  agentSummary,
  currentModelName,
} from '../src/composables/useAgentSummary'

describe('currentModelName', () => {
  it('prefers the name the agent gave the model', () => {
    expect(
      currentModelName({
        currentModel: 'claude-sonnet-4-5',
        models: [{ id: 'claude-sonnet-4-5', name: 'Claude Sonnet 4.5' }],
      })
    ).toBe('Claude Sonnet 4.5')
  })

  it('falls back to the id, which still tells a reader something', () => {
    expect(currentModelName({ currentModel: 'gpt-5-codex', models: [] })).toBe('gpt-5-codex')
    expect(currentModelName({ currentModel: 'gpt-5-codex' })).toBe('gpt-5-codex')
    expect(
      currentModelName({ currentModel: 'x', models: [{ id: 'x', name: '   ' }] })
    ).toBe('x')
    expect(currentModelName({ currentModel: 'x', models: [{ id: 'x', name: 7 }] })).toBe('x')
  })

  it('ignores entries that are not the current one', () => {
    expect(
      currentModelName({
        currentModel: 'b',
        models: [null, { id: 'a', name: 'A' }, { id: 'b', name: 'B' }],
      })
    ).toBe('B')
  })

  it('has nothing to say when no model is selected', () => {
    // Normal: an agent that offers no choice never reports one.
    expect(currentModelName({ models: [{ id: 'a', name: 'A' }] })).toBeUndefined()
    expect(currentModelName({ currentModel: '' })).toBeUndefined()
    expect(currentModelName(undefined)).toBeUndefined()
  })
})

describe('agentSummary', () => {
  it('names the client and the model it is running', () => {
    expect(
      agentSummary({
        agentClient: { name: 'acp-gateway', version: '0.2.13' },
        agentModels: { currentModel: 'gpt-5-codex', models: [{ id: 'gpt-5-codex', name: 'GPT-5 Codex' }] },
      })
    ).toBe('acp-gateway · GPT-5 Codex')
  })

  it('says just the client when no model has been reported', () => {
    expect(agentSummary({ agentClient: { name: 'claude-code' } })).toBe('claude-code')
  })

  it('says just the model when the client did not name itself', () => {
    expect(
      agentSummary({ agentModels: { currentModel: 'x', models: [{ id: 'x', name: 'X' }] } })
    ).toBe('X')
  })

  it('says nothing at all when nothing is known', () => {
    // A connected agent that has said nothing about itself is ordinary, and
    // "Unknown agent" would read as a fault rather than as silence.
    expect(agentSummary({})).toBe('')
    expect(agentSummary(undefined)).toBe('')
    expect(agentSummary({ agentClient: { name: '   ' } })).toBe('')
    expect(agentSummary({ agentClient: { name: 42 } })).toBe('')
  })
})

describe('agentClientName', () => {
  it('gives the client its own name, trimmed', () => {
    expect(agentClientName({ agentClient: { name: '  acp-gateway  ' } })).toBe('acp-gateway')
  })

  it('has nothing to give when the client did not name itself', () => {
    expect(agentClientName({ agentClient: { name: '   ' } })).toBeUndefined()
    expect(agentClientName({ agentClient: { name: 42 } })).toBeUndefined()
    expect(agentClientName({ agentClient: {} })).toBeUndefined()
    expect(agentClientName({})).toBeUndefined()
    expect(agentClientName(undefined)).toBeUndefined()
  })
})

describe('agentDetails', () => {
  it('hands the card each half separately, so each can truncate on its own', () => {
    expect(
      agentDetails({
        agentClient: { name: 'acp-gateway' },
        agentModels: { currentModel: 'x', models: [{ id: 'x', name: 'GPT-5 Codex' }] },
      })
    ).toEqual({ client: 'acp-gateway', model: 'GPT-5 Codex' })
  })

  it('gives just the half it knows', () => {
    expect(agentDetails({ agentClient: { name: 'claude-code' } })).toEqual({
      client: 'claude-code',
      model: undefined,
    })
    expect(agentDetails({ agentModels: { currentModel: 'x' } })).toEqual({
      client: undefined,
      model: 'x',
    })
  })

  it('is null when there is nothing to lay out, so the card drops the row', () => {
    expect(agentDetails({})).toBeNull()
    expect(agentDetails(undefined)).toBeNull()
    expect(agentDetails({ agentClient: { name: '  ' }, agentModels: {} })).toBeNull()
  })
})
