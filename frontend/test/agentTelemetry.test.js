import { describe, it, expect } from 'vitest'

import {
  agentTelemetryKind,
  isAgentTelemetry,
  planContent,
  planIsWithdrawn,
  planProgress,
  telemetryText,
  thoughtPreview,
  usageDetail,
} from '../src/composables/useAgentTelemetry'

const message = (metadata, text = '') => ({ id: 'm1', sender: 'agent', text, metadata })

describe('agentTelemetryKind', () => {
  it('names the kind of each telemetry message', () => {
    expect(agentTelemetryKind(message({ type: 'agent_thought' }))).toBe('thought')
    expect(agentTelemetryKind(message({ type: 'agent_plan' }))).toBe('plan')
    expect(agentTelemetryKind(message({ type: 'agent_usage' }))).toBe('usage')
  })

  it('leaves every other message alone', () => {
    expect(agentTelemetryKind(message({ type: 'permission_request' }))).toBeNull()
    expect(agentTelemetryKind(message(undefined))).toBeNull()
    expect(agentTelemetryKind(undefined)).toBeNull()
  })

  it('answers the same question as isAgentTelemetry', () => {
    expect(isAgentTelemetry(message({ type: 'agent_plan' }))).toBe(true)
    expect(isAgentTelemetry(message({ type: 'permission_request' }))).toBe(false)
  })
})

describe('telemetryText', () => {
  // Plans and usage lines are revised in place and only their metadata is
  // rewritten, so the body goes stale the moment the agent revises them.
  it('prefers the metadata, which is the part that gets revised', () => {
    expect(telemetryText(message({ type: 'agent_plan', text: 'current' }, 'as first written'))).toBe(
      'current'
    )
  })

  it('falls back to the body when the metadata carries no text', () => {
    expect(telemetryText(message({ type: 'agent_plan' }, 'as first written'))).toBe('as first written')
    expect(telemetryText(message({ type: 'agent_plan', text: '' }, 'as first written'))).toBe(
      'as first written'
    )
  })

  it('has nothing to show for a message with neither', () => {
    expect(telemetryText(message({ type: 'agent_plan' }))).toBe('')
    expect(telemetryText(undefined)).toBe('')
  })
})

describe('planContent', () => {
  it('reads an entry-based plan, filling in what an entry leaves out', () => {
    const plan = planContent(
      message({
        type: 'agent_plan',
        planType: 'items',
        entries: [
          { content: 'Read the config', priority: 'high', status: 'completed' },
          { content: 'Wire it up', priority: 'medium', status: 'in_progress' },
          { content: 'Add tests' },
        ],
      })
    )

    expect(plan.type).toBe('items')
    expect(plan.entries).toEqual([
      { content: 'Read the config', priority: 'high', status: 'completed', done: true, active: false },
      { content: 'Wire it up', priority: 'medium', status: 'in_progress', done: false, active: true },
      { content: 'Add tests', priority: 'medium', status: 'pending', done: false, active: false },
    ])
  })

  it('renders an entry with nothing in it rather than dropping the row', () => {
    const plan = planContent(message({ type: 'agent_plan', entries: [null] }))

    expect(plan.entries).toEqual([
      { content: '', priority: 'medium', status: 'pending', done: false, active: false },
    ])
  })

  it('reads a markdown plan', () => {
    expect(
      planContent(message({ type: 'agent_plan', planType: 'markdown', content: '## Steps' }))
    ).toEqual({ type: 'markdown', content: '## Steps' })
  })

  it('reads a file-backed plan', () => {
    expect(
      planContent(message({ type: 'agent_plan', planType: 'file', uri: 'file:///tmp/plan.md' }))
    ).toEqual({ type: 'file', uri: 'file:///tmp/plan.md' })
  })

  it('shows an empty markdown or file plan as empty rather than undefined', () => {
    expect(planContent(message({ type: 'agent_plan', planType: 'markdown' })).content).toBe('')
    expect(planContent(message({ type: 'agent_plan', planType: 'file' })).uri).toBe('')
  })

  it('falls back to whatever text there is for a shape it does not know', () => {
    expect(planContent(message({ type: 'agent_plan', text: 'Plan withdrawn.' }))).toEqual({
      type: 'text',
      text: 'Plan withdrawn.',
    })
    expect(planContent(undefined)).toEqual({ type: 'text', text: '' })
  })
})

describe('planProgress', () => {
  it('counts what is done against what there is', () => {
    expect(
      planProgress(
        message({
          type: 'agent_plan',
          entries: [
            { content: 'a', status: 'completed' },
            { content: 'b', status: 'completed' },
            { content: 'c', status: 'in_progress' },
          ],
        })
      )
    ).toEqual({ done: 2, total: 3 })
  })

  it('has nothing to count for an empty or non-entry plan', () => {
    expect(planProgress(message({ type: 'agent_plan', entries: [] }))).toBeNull()
    expect(planProgress(message({ type: 'agent_plan', planType: 'markdown', content: '#' }))).toBeNull()
  })
})

describe('planIsWithdrawn', () => {
  it('knows a plan the agent dropped', () => {
    expect(planIsWithdrawn(message({ type: 'agent_plan', removed: true }))).toBe(true)
    expect(planIsWithdrawn(message({ type: 'agent_plan' }))).toBe(false)
    expect(planIsWithdrawn(undefined)).toBe(false)
  })
})

describe('usageDetail', () => {
  it('carries the counters through as the agent reported them', () => {
    expect(
      usageDetail(
        message({
          type: 'agent_usage',
          used: 50000,
          size: 200000,
          percent: 25,
          text: 'Context 50,000 / 200,000 tokens (25%)',
        })
      )
    ).toEqual({
      used: 50000,
      size: 200000,
      percent: 25,
      text: 'Context 50,000 / 200,000 tokens (25%)',
    })
  })

  it('works out the percentage when the agent did not send one', () => {
    expect(usageDetail(message({ type: 'agent_usage', used: 25000, size: 200000 })).percent).toBe(13)
  })

  // A bar cannot be drawn outside its track.
  it('keeps the percentage inside the range a bar can draw', () => {
    expect(usageDetail(message({ type: 'agent_usage', percent: 140 })).percent).toBe(100)
    expect(usageDetail(message({ type: 'agent_usage', percent: -5 })).percent).toBe(0)
  })

  it('has no percentage to show without a window size to divide by', () => {
    expect(usageDetail(message({ type: 'agent_usage', used: 10, size: 0 })).percent).toBeNull()
    expect(usageDetail(message({ type: 'agent_usage', used: 10 })).percent).toBeNull()
  })

  it('treats counters that are not real numbers as missing', () => {
    const detail = usageDetail(message({ type: 'agent_usage', used: '50000', size: NaN }))

    expect(detail).toEqual({ used: null, size: null, percent: null, text: '' })
  })

  it('has nothing to report for a message that is not there', () => {
    expect(usageDetail(undefined)).toEqual({ used: null, size: null, percent: null, text: '' })
  })
})

describe('thoughtPreview', () => {
  it('summarises a block by its first line', () => {
    expect(
      thoughtPreview(message({ type: 'agent_thought', text: '\n  Checking the config.\nThen tests.' }))
    ).toBe('Checking the config.')
  })

  it('collapses the whitespace inside that line', () => {
    expect(thoughtPreview(message({ type: 'agent_thought', text: 'Checking\tthe   config.' }))).toBe(
      'Checking the config.'
    )
  })

  it('truncates a long line rather than letting it wrap the header', () => {
    const preview = thoughtPreview(message({ type: 'agent_thought', text: 'x'.repeat(200) }), 10)

    expect(preview).toBe(`${'x'.repeat(9)}…`)
  })

  it('does not truncate a line that already fits', () => {
    expect(thoughtPreview(message({ type: 'agent_thought', text: 'Short.' }), 10)).toBe('Short.')
  })

  it('trims the trailing space a cut can leave behind', () => {
    expect(thoughtPreview(message({ type: 'agent_thought', text: 'abcdefghi jklmn' }), 11)).toBe(
      'abcdefghi…'
    )
  })

  it('has nothing to preview for an empty block', () => {
    expect(thoughtPreview(message({ type: 'agent_thought', text: '   \n  ' }))).toBe('')
  })
})
