import { describe, it, expect } from 'vitest'

import { apply, describeAge, describeStatus, statsFor, wordCount } from '../../../examples/extensions/task-stats/index.js'
import { parseManifest } from '../../src/main/extensions/manifest.js'
import manifest from '../../../examples/extensions/task-stats/agentrq-extension.json'

import { describeAsk } from '../../../frontend/src/composables/useExtensionGrant.js'

/**
 * The smallest extension there is, and the one that tests the hardest screen.
 *
 * It asks for nothing, so its install screen shows no permission list at all —
 * and an absent list reads as safety. The sentence about full machine access is
 * the whole of what is said there, which is why the last test in this file is
 * about the grant screen rather than about this extension.
 */

const NOW = Date.parse('2026-03-10T12:00:00Z')
const ago = (ms) => new Date(NOW - ms).toISOString()

describe('the manifest', () => {
  it('is one', () => {
    expect(parseManifest(manifest).ok).toBe(true)
  })

  it('asks for nothing at all', () => {
    const { manifest: parsed } = parseManifest(manifest)
    expect(parsed.mcp).toEqual({ workspace: [], supervisor: [] })
    expect(parsed.net).toEqual([])
    expect(parsed.config).toEqual([])
  })
})

describe('describeAge', () => {
  it('counts in the largest unit that still says something', () => {
    expect(describeAge(ago(90_000), NOW)).toBe('1 minute')
    expect(describeAge(ago(3 * 3600_000), NOW)).toBe('3 hours')
    expect(describeAge(ago(5 * 86_400_000), NOW)).toBe('5 days')
  })

  // "1 days ago" is the kind of thing people notice and nobody reports.
  it('gets the singular right', () => {
    expect(describeAge(ago(3600_000), NOW)).toBe('1 hour')
    expect(describeAge(ago(48 * 3600_000), NOW)).toBe('2 days')
  })

  it('says so rather than guessing when there is no date', () => {
    expect(describeAge(undefined, NOW)).toBe('unknown')
    expect(describeAge('not a date', NOW)).toBe('unknown')
  })

  // Clocks disagree; a task created "in the future" is not worth a negative.
  it('never counts backwards', () => {
    expect(describeAge(new Date(NOW + 60_000).toISOString(), NOW)).toBe('0 minutes')
  })
})

describe('wordCount', () => {
  it('counts words, not characters', () => {
    expect(wordCount('two words')).toBe(2)
    expect(wordCount('  spaced   out \n across lines ')).toBe(4)
  })

  it('is zero for nothing', () => {
    expect(wordCount('')).toBe(0)
    expect(wordCount(undefined)).toBe(0)
  })
})

describe('describeStatus', () => {
  it('says who has it as well as what state it is in', () => {
    // On a board where both agents and people work, "ongoing" means something
    // different for each.
    expect(describeStatus({ status: 'ongoing', assignee: 'agent' })).toBe('ongoing, with an agent')
    expect(describeStatus({ status: 'blocked', assignee: 'human' })).toBe('blocked, with a person')
  })

  it('leaves out what it was not told', () => {
    expect(describeStatus({ status: 'ongoing' })).toBe('ongoing')
    expect(describeStatus({})).toBe('unknown')
    expect(describeStatus(undefined)).toBe('unknown')
  })
})

describe('statsFor', () => {
  const task = {
    title: 'Ship it',
    body: 'three words here',
    status: 'ongoing',
    assignee: 'agent',
    createdAt: ago(2 * 3600_000),
  }

  it('describes the task without asking anything of the server', () => {
    const view = statsFor(task, NOW)

    expect(view.title).toContain('Ship it')
    const rows = view.nodes[0].items
    expect(rows.find((row) => row.label === 'Age').value).toBe('2 hours')
    expect(rows.find((row) => row.label === 'Words in the description').value).toBe('3')
    expect(rows.find((row) => row.label === 'Status').value).toBe('ongoing, with an agent')
  })

  /**
   * The bug this example shipped with, and why the count is gone rather than
   * fixed.
   *
   * It reported a message count, and always said one. The board's task list is
   * a summary — the server sends the last message per task and nothing else —
   * so `task.messages` has one element however long the thread is. The real
   * number needs `getTask`, which needs a permission this example deliberately
   * does not ask for. `standup` asks, and counts.
   */
  it('does not report a message count it has no way to know', () => {
    const withSummary = statsFor({ ...task, messages: [{ text: 'the last one' }] }, NOW)
    const withNone = statsFor(task, NOW)

    for (const view of [withSummary, withNone]) {
      expect(view.nodes[0].items.map((row) => row.label)).not.toContain('Messages')
    }
    // The two are identical: what the list happens to carry changes nothing.
    expect(withSummary).toEqual(withNone)
  })

  it('says where its numbers came from, and what it cannot see', () => {
    const view = statsFor(task, NOW)

    expect(view.nodes.at(-1).value).toContain('asks for no permissions')
    expect(view.nodes.at(-1).value).toContain('cannot read the conversation')
  })

  it('counts only the description, not anything attached to it', () => {
    expect(statsFor({ ...task, body: '' }, NOW).nodes[0].items[1].value).toBe('0')
  })

  it('holds up when handed almost nothing', () => {
    const view = statsFor(undefined, NOW)
    expect(view.title).toContain('this task')
    expect(view.nodes[0].items.find((row) => row.label === 'Status').value).toBe('unknown')
  })
})

describe('apply', () => {
  it('registers one task menu item and nothing else', () => {
    const added = []
    apply({ ui: { add: (entry) => added.push(entry) } })

    expect(added).toHaveLength(1)
    expect(added[0]).toMatchObject({ id: 'stats', surface: 'task-menu', label: 'Task Stats' })
    // No `when`, because every task has the fields it reads.
    expect(added[0].when).toBeUndefined()
    expect(added[0].run({ title: 'x' }).title).toContain('x')
  })
})

describe('the install screen for an extension that asks for nothing', () => {
  // The path that is easy to get wrong: no permission list appears, and absence
  // reads as safety, so the one constant sentence carries the whole weight.
  it('shows no permissions and still says what an extension can do', () => {
    const ask = describeAsk(parseManifest(manifest).manifest)

    expect(ask.level).toBe('none')
    expect(ask.workspace).toEqual([])
    expect(ask.supervisor).toEqual([])
    expect(ask.networkClaim).toBe('')
    expect(ask.machineAccess).toContain('full access to your computer')
  })
})
