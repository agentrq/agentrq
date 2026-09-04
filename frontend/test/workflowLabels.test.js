import { describe, it, expect } from 'vitest'

import {
  emittedEventTooltip,
  eventLabel,
  nodeTooltip,
  paletteTooltip,
  triggerWorkspaceTooltip,
  workspaceLabel,
} from '../src/composables/useWorkflowLabels'

const workspaces = { w1: { id: 'w1', name: 'agentrq-release-engineering' } }
const events = { e1: { id: 'e1', name: 'deploy_done' } }

describe('workspaceLabel', () => {
  it('names the workspace', () => {
    expect(workspaceLabel(workspaces, 'w1')).toBe('agentrq-release-engineering')
  })

  // A raw base62 ID says nothing about which workspace runs, which is the
  // question the label exists to answer.
  it('says so when the workspace is gone, rather than showing an ID', () => {
    expect(workspaceLabel(workspaces, 'w404')).toBe('(deleted workspace)')
    expect(workspaceLabel(undefined, 'w1')).toBe('(deleted workspace)')
  })
})

describe('eventLabel', () => {
  it('names the event', () => {
    expect(eventLabel(events, 'e1')).toBe('deploy_done')
  })

  it('says so when the event is gone', () => {
    expect(eventLabel(events, 'e404')).toBe('(deleted event)')
    expect(eventLabel(undefined, 'e1')).toBe('(deleted event)')
  })
})

describe('nodeTooltip', () => {
  // The name leads every tooltip: the node truncates it, and reading the rest
  // is why anyone hovers.
  it('leads with the full name of an event', () => {
    const tooltip = nodeTooltip({ kind: 'event', label: 'deploy_done' })

    expect(tooltip.split('\n')[0]).toBe('deploy_done')
    expect(tooltip).toBe('deploy_done\nEvent')
  })

  it('marks the event a run begins at', () => {
    expect(nodeTooltip({ kind: 'event', label: 'deploy_done', isStart: true }))
      .toBe('deploy_done\nEvent · starts this workflow')
  })

  it('marks an event that is published outside the workflow', () => {
    expect(nodeTooltip({ kind: 'global-event', label: 'docs_updated' }))
      .toBe('docs_updated\nEvent · published outside this workflow')
  })

  // The node has room for the workspace and nothing else, so the work it will
  // create is only ever visible here.
  it('says which workspace runs and what it is asked to do', () => {
    expect(nodeTooltip({
      kind: 'step',
      label: 'agentrq-release-engineering',
      step: { title: 'Cut the release notes' },
    })).toBe('agentrq-release-engineering\nWorkspace · creates "Cut the release notes"')
  })

  it('adds the schedule when the task is a scheduled one', () => {
    expect(nodeTooltip({
      kind: 'step',
      label: 'agentrq-code',
      step: { title: 'Sweep the queue', cronSchedule: '30 * * * *' },
    })).toBe('agentrq-code\nWorkspace · creates "Sweep the queue"\nRuns on schedule 30 * * * *')
  })

  it('still names the workspace when the step carries no title', () => {
    expect(nodeTooltip({ kind: 'step', label: 'agentrq-code', step: { title: '   ' } }))
      .toBe('agentrq-code\nWorkspace')
    expect(nodeTooltip({ kind: 'step', label: 'agentrq-code' }))
      .toBe('agentrq-code\nWorkspace')
  })

  // A global subscriber is not part of this workflow but does run on its
  // events, so the tooltip has to say both.
  it('marks a global subscriber as one', () => {
    expect(nodeTooltip({
      kind: 'global',
      label: 'agentrq-docs',
      trigger: { title: 'Update the changelog', cronSchedule: '0 9 * * *' },
    })).toBe([
      'agentrq-docs',
      'Workspace · creates "Update the changelog"',
      'Runs on schedule 0 9 * * *',
      'Global subscriber · always runs on this event',
    ].join('\n'))
  })

  it('has nothing to say about a node that is not there', () => {
    expect(nodeTooltip(null)).toBe('')
    expect(nodeTooltip({ kind: 'something-else', label: 'mystery' })).toBe('mystery')
    // No blank first line when there is no name to lead with.
    expect(nodeTooltip({ kind: 'event' })).toBe('Event')
  })
})

describe('paletteTooltip', () => {
  it('names the chip and what dragging it does', () => {
    expect(paletteTooltip('workspace', 'agentrq-release-engineering'))
      .toBe('agentrq-release-engineering\nWorkspace · drag onto an event to create tasks here')
    expect(paletteTooltip('event', 'deploy_done'))
      .toBe('deploy_done\nEvent · drag onto a workspace to emit it on completion')
  })

  it('survives a chip with no name yet', () => {
    expect(paletteTooltip('workspace', undefined))
      .toBe('Workspace · drag onto an event to create tasks here')
  })
})

describe('emittedEventTooltip', () => {
  it('names the event and when it fires', () => {
    expect(emittedEventTooltip('docs_updated'))
      .toBe('docs_updated\nEvent · published when this task completes')
  })

  it('survives a missing name', () => {
    expect(emittedEventTooltip(null)).toBe('Event · published when this task completes')
  })
})

describe('triggerWorkspaceTooltip', () => {
  it('spells out where the work lands', () => {
    expect(triggerWorkspaceTooltip('agentrq-code'))
      .toBe('agentrq-code\nWorkspace · a task is created here when this event fires')
  })

  it('survives a missing name', () => {
    expect(triggerWorkspaceTooltip(undefined))
      .toBe('Workspace · a task is created here when this event fires')
  })
})
