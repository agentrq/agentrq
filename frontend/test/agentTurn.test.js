// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, it, expect } from 'vitest'

import { agentIsWorking, composerAction, workingPlaceholder } from '../src/composables/useAgentTurn'

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

  it('survives a messages list that is not one', () => {
    expect(agentIsWorking({ task: ongoing, workspace: gateway, messages: null })).toBe(true)
    expect(agentIsWorking()).toBe(false)
  })
})

describe('what the composer offers', () => {
  it('offers the stop while the agent is working, and the send otherwise', () => {
    expect(composerAction(true)).toBe('stop')
    expect(composerAction(false)).toBe('send')
  })

  // An input that stops accepting text without saying why reads as a broken
  // page, and the explanation is also the instruction.
  it('says why it is not taking anything and what to do about it', () => {
    expect(workingPlaceholder()).toMatch(/working/i)
    expect(workingPlaceholder()).toMatch(/stop/i)
  })
})
