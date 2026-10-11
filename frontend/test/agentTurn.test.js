// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect } from 'vitest'

import { agentIsWorking, agentIsBusy, turnStartedAt, formatElapsed, workingPlaceholder } from '../src/composables/useAgentTurn'

const gateway = { agentSupportsStop: true, agentConnected: true }
const ongoing = { status: 'ongoing', assignee: 'agent' }

const fromHuman = (id = 'h1') => ({ id, sender: 'human', text: 'do the thing' })
const fromAgent = (id = 'a1') => ({ id, sender: 'agent', text: 'done' })
const usageFooter = (id = 'u1') => ({ id, sender: 'agent', metadata: { type: 'agent_usage' } })

describe('agentIsWorking', () => {
  // The whole point: the ACP gateway does not interrupt, so a message sent
  // after somebody has spoken and before the agent has answered is queued
  // behind the turn rather than read.
  it('is working from the moment somebody sends until the agent answers', () => {
    expect(
      agentIsWorking({ task: ongoing, workspace: gateway, messages: [fromAgent(), fromHuman()] })
    ).toBe(true)
  })

  // The gateway flushes a usage footer when the prompt resolves — "the last
  // usage snapshot of the turn" — so that, and not a reply, is the end of one.
  it('is waiting once the turn has been footed', () => {
    expect(
      agentIsWorking({ task: ongoing, workspace: gateway, messages: [fromHuman(), usageFooter()] })
    ).toBe(false)
  })

  // The bug in the obvious version. The gateway's own instructions tell agents
  // to report progress with `reply` every few steps, so a reply arriving is
  // usually the agent talking *while* it works. Unlocking on one hands the
  // composer back mid-turn and queues whatever is typed next.
  it('is still working when the agent replies in the middle of a turn', () => {
    const messages = [fromHuman('h1'), usageFooter('u1'), fromHuman('h2'), fromAgent('progress')]

    expect(agentIsWorking({ task: ongoing, workspace: gateway, messages })).toBe(true)
  })

  // An agent that publishes no usage at all would otherwise lock the composer
  // for good. Worse than the footer, but the old behaviour rather than a dead
  // end.
  it('falls back to a reply when the agent has never sent a footer', () => {
    expect(
      agentIsWorking({ task: ongoing, workspace: gateway, messages: [fromHuman(), fromAgent()] })
    ).toBe(false)
  })

  // Pressing stop makes the prompt resolve, which flushes the footer — so the
  // composer comes back without anything else having to happen.
  it('is waiting again after a stopped turn is footed', () => {
    const stopNote = { id: 'n', sender: 'agent', text: 'The agent was stopped.' }
    const messages = [fromHuman(), stopNote, usageFooter()]

    expect(agentIsWorking({ task: ongoing, workspace: gateway, messages })).toBe(false)
  })

  // A task that has just been started has been handed to the agent and has
  // heard nothing back — which is working, not waiting.
  it('is working when the agent has not said anything yet', () => {
    expect(agentIsWorking({ task: ongoing, workspace: gateway, messages: [] })).toBe(true)
    expect(agentIsWorking({ task: ongoing, workspace: gateway })).toBe(true)
  })

  // Slack is a person sending a message into the task, and it queues behind a
  // turn exactly as the composer does.
  it('counts a message from Slack as somebody speaking', () => {
    const slack = { id: 's1', sender: 'slack', text: 'any update?' }

    expect(agentIsWorking({ task: ongoing, workspace: gateway, messages: [fromAgent(), slack] })).toBe(true)
  })

  // The countdown bubble is a message that has not gone anywhere yet, so it
  // cannot be what makes the agent busy — and if it were, sending with a delay
  // configured would lock the composer against its own pending message.
  it('ignores a message that has not been sent yet', () => {
    const pending = { ...fromHuman('p1'), _pending: true }

    expect(agentIsWorking({ task: ongoing, workspace: gateway, messages: [fromAgent(), pending] })).toBe(false)
  })

  // These arrive *during* a turn. Reading one as the end of it would hand the
  // composer back at the exact moment the agent is standing still waiting for
  // an answer, and the message would queue behind the rest of the turn.
  it('is still working while the agent is asking for something', () => {
    const permission = {
      id: 'p',
      sender: 'agent',
      metadata: { type: 'permission_request', status: 'pending' },
    }
    const elicitation = { id: 'e', sender: 'agent', metadata: { type: 'elicitation_request' } }

    expect(
      agentIsWorking({ task: ongoing, workspace: gateway, messages: [fromHuman(), permission] })
    ).toBe(true)
    expect(
      agentIsWorking({ task: ongoing, workspace: gateway, messages: [fromHuman(), elicitation] })
    ).toBe(true)
  })

  // A plan or a thought is the trace of a turn in progress, not the end of one.
  it('is still working while telemetry other than the footer arrives', () => {
    const plan = { id: 't', sender: 'agent', metadata: { type: 'agent_plan' } }
    const thought = { id: 'th', sender: 'agent', metadata: { type: 'agent_thought' } }

    expect(agentIsWorking({ task: ongoing, workspace: gateway, messages: [fromHuman(), plan] })).toBe(true)
    expect(agentIsWorking({ task: ongoing, workspace: gateway, messages: [fromHuman(), thought] })).toBe(true)
  })

  // Claude Code speaking MCP directly cannot be stopped and does not queue like
  // this. Taking its Send away would leave no way to say anything at all.
  it('is never working for an agent that cannot be stopped', () => {
    const plain = { agentSupportsStop: false, agentConnected: true }

    expect(agentIsWorking({ task: ongoing, workspace: plain, messages: [fromHuman()] })).toBe(false)
    expect(agentIsWorking({ task: ongoing, workspace: null, messages: [fromHuman()] })).toBe(false)
    expect(agentIsWorking({ task: ongoing, messages: [fromHuman()] })).toBe(false)
  })

  // `ongoing` is the only status in which an agent has the task. Treating the
  // others as working would lock the composer on a task nobody is acting on.
  it('is not working on a task no agent is holding', () => {
    for (const status of ['notstarted', 'pending', 'completed', 'blocked', 'rejected', 'cron', undefined]) {
      expect(
        agentIsWorking({ task: { status, assignee: 'agent' }, workspace: gateway, messages: [fromHuman()] })
      ).toBe(false)
    }
    expect(agentIsWorking({ workspace: gateway, messages: [fromHuman()] })).toBe(false)
  })

  // A task assigned to a person has no agent turn to be in the middle of.
  it('is not working on a task assigned to a human', () => {
    expect(
      agentIsWorking({
        task: { status: 'ongoing', assignee: 'human' },
        workspace: gateway,
        messages: [fromHuman()],
      })
    ).toBe(false)
  })

  it('reads the newest exchange, not the first', () => {
    const messages = [fromHuman('h1'), usageFooter('u1'), fromHuman('h2'), usageFooter('u2'), fromHuman('h3')]

    expect(agentIsWorking({ task: ongoing, workspace: gateway, messages })).toBe(true)
    expect(agentIsWorking({ task: ongoing, workspace: gateway, messages: messages.slice(0, 4) })).toBe(false)
  })

  // The bug: a stop pressed before the agent reported any usage has no footer
  // to flush, only the gateway's note on why the turn ended — and an agent
  // that has footed earlier turns no longer counts a plain reply as the end.
  // The composer stayed locked and the queue was never sent.
  it('is waiting once the gateway says the turn ended early, footer or not', () => {
    const notes = [
      '⚠️ The turn was cancelled before the agent finished.',
      '⚠️ The agent refused to continue this turn.',
      '⚠️ The agent stopped mid-turn: it ran out of output tokens.',
      '⚠️ The agent stopped mid-turn: it reached its limit on model requests for a single turn.',
      '⚠️ The agent stopped for a reason this gateway does not recognise: other.',
    ]
    for (const text of notes) {
      const messages = [fromHuman('h1'), usageFooter('u1'), fromHuman('h2'), { id: 'n', sender: 'agent', text }]

      expect(agentIsWorking({ task: ongoing, workspace: gateway, messages })).toBe(false)
    }
  })

  // Only the gateway's own note, word for word. An agent saying it was
  // cancelled in a progress reply, or a telemetry message carrying the text,
  // is still mid-turn.
  it('is still working when something only looks like a stop note', () => {
    const lookalikes = [
      { id: 'q', sender: 'agent', text: 'The turn was cancelled before the agent finished.' },
      { id: 'r', sender: 'agent', text: 'Note: ⚠️ The turn was cancelled before the agent finished.' },
      { id: 's', sender: 'human', text: '⚠️ The turn was cancelled before the agent finished.' },
      { id: 't', sender: 'agent', text: '⚠️ The turn was cancelled before the agent finished.', metadata: { type: 'agent_thought' } },
      { id: 'u', sender: 'agent' },
    ]
    for (const note of lookalikes) {
      const messages = [fromHuman('h1'), usageFooter('u1'), fromHuman('h2'), note]

      expect(agentIsWorking({ task: ongoing, workspace: gateway, messages })).toBe(true)
    }
  })

  it('survives a messages list that is not one', () => {
    expect(agentIsWorking({ task: ongoing, workspace: gateway, messages: null })).toBe(true)
    expect(agentIsWorking()).toBe(false)
  })
})

describe('what the composer says', () => {
  // A Send that does not send has to say where the message went, or the box
  // reads as broken: it takes the text and nothing visibly happens.
  it('says the agent is working and that the message will be queued', () => {
    expect(workingPlaceholder()).toMatch(/working/i)
    expect(workingPlaceholder()).toMatch(/queued/i)
  })
})

describe('agentIsBusy', () => {
  const direct = { agentSupportsStop: false, agentConnected: true }

  it('follows the turn rules where the agent can be stopped', () => {
    expect(agentIsBusy({ task: ongoing, workspace: gateway, messages: [fromHuman()] })).toBe(true)
    expect(agentIsBusy({ task: ongoing, workspace: gateway, messages: [fromHuman(), usageFooter()] })).toBe(false)
  })

  it('is false with nobody connected', () => {
    const away = { ...direct, agentConnected: false }
    expect(agentIsBusy({ task: ongoing, workspace: away, messages: [fromHuman()] })).toBe(false)
    expect(agentIsBusy({ task: ongoing, workspace: { ...gateway, agentConnected: false }, messages: [fromHuman()] })).toBe(false)
    expect(agentIsBusy()).toBe(false)
  })

  it('is true for a connected agent that has not answered yet', () => {
    expect(agentIsBusy({ task: ongoing, workspace: direct, messages: [fromHuman()] })).toBe(true)
    expect(agentIsBusy({ task: ongoing, workspace: direct, messages: [] })).toBe(true)
    expect(agentIsBusy({ task: ongoing, workspace: direct })).toBe(true)
  })

  it('is false once the agent has answered, and for telemetry it is still busy', () => {
    expect(agentIsBusy({ task: ongoing, workspace: direct, messages: [fromHuman(), fromAgent()] })).toBe(false)
    expect(agentIsBusy({ task: ongoing, workspace: direct, messages: [fromHuman(), usageFooter()] })).toBe(true)
  })

  it('is false for a task that is not the agent\'s to work on', () => {
    expect(agentIsBusy({ task: { status: 'ongoing', assignee: 'human' }, workspace: direct, messages: [fromHuman()] })).toBe(false)
    expect(agentIsBusy({ task: { status: 'completed', assignee: 'agent' }, workspace: direct, messages: [fromHuman()] })).toBe(false)
  })
})

describe('turnStartedAt', () => {
  it('is when the last person spoke', () => {
    const messages = [
      { sender: 'human', createdAt: '2026-10-11T10:00:00Z' },
      { sender: 'agent', createdAt: '2026-10-11T10:01:00Z' },
      { sender: 'slack', createdAt: '2026-10-11T10:02:00Z' },
    ]
    expect(turnStartedAt(messages)).toBe(Date.parse('2026-10-11T10:02:00Z'))
  })

  it('is null when nobody has spoken', () => {
    expect(turnStartedAt([{ sender: 'agent', createdAt: '2026-10-11T10:00:00Z' }])).toBeNull()
    expect(turnStartedAt([{ sender: 'human' }])).toBeNull()
    expect(turnStartedAt()).toBeNull()
  })
})

describe('formatElapsed', () => {
  it('reads seconds, minutes and hours', () => {
    expect(formatElapsed(7400)).toBe('7s')
    expect(formatElapsed(65000)).toBe('1m 05s')
    expect(formatElapsed(3720000)).toBe('1h 02m')
    expect(formatElapsed(-5)).toBe('0s')
  })
})
