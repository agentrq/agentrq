import { describe, it, expect, vi } from 'vitest'

import {
  looksLikeTaskId,
  matchTasks,
  resolveTaskById,
  taskRoute,
} from '../src/composables/useTaskFinder'

const task = (id, title, extra = {}) => ({ id, title, ...extra })

describe('looksLikeTaskId', () => {
  it('accepts a base62 monoflake', () => {
    expect(looksLikeTaskId('0iCWbYTwIiH')).toBe(true)
  })

  it('rejects anything that cannot be one', () => {
    expect(looksLikeTaskId('short')).toBe(false)
    expect(looksLikeTaskId('a task about billing')).toBe(false)
    expect(looksLikeTaskId('0iCWbYTwIiH-extra-long-string')).toBe(false)
    expect(looksLikeTaskId('')).toBe(false)
    expect(looksLikeTaskId(null)).toBe(false)
    expect(looksLikeTaskId(undefined)).toBe(false)
  })

  it('ignores the whitespace around a pasted ID', () => {
    expect(looksLikeTaskId('  0iCWbYTwIiH  ')).toBe(true)
  })
})

describe('matchTasks', () => {
  const tasks = [
    task('0aaaaaaaaaa', 'Ship the billing fix'),
    task('0bbbbbbbbbb', 'Billing dashboard copy'),
    task('0bbbbbbbbbc', 'Unrelated'),
  ]

  it('shows the most recent tasks before anything is typed', () => {
    // The resting state of the box. A blank panel would be a worse answer.
    expect(matchTasks(tasks, '')).toEqual(tasks)
    expect(matchTasks(tasks, '   ')).toEqual(tasks)
    expect(matchTasks(tasks, null)).toEqual(tasks)
  })

  it('puts an exact ID first, then ID prefixes, then titles', () => {
    const ranked = matchTasks(tasks, '0bbbbbbbbbc')

    expect(ranked.map((t) => t.id)).toEqual(['0bbbbbbbbbc'])
  })

  it('matches an ID the user has only partly typed', () => {
    expect(matchTasks(tasks, '0bbbb').map((t) => t.id)).toEqual(['0bbbbbbbbbb', '0bbbbbbbbbc'])
  })

  it('matches titles, case-insensitively, so the box works without an ID', () => {
    expect(matchTasks(tasks, 'BILLING').map((t) => t.id)).toEqual(['0aaaaaaaaaa', '0bbbbbbbbbb'])
  })

  it('ranks an exact ID above a title that also matches', () => {
    const both = [task('0zzzzzzzzzz', 'mentions 0aaaaaaaaaa'), task('0aaaaaaaaaa', 'The real one')]

    expect(matchTasks(both, '0aaaaaaaaaa').map((t) => t.id)).toEqual([
      '0aaaaaaaaaa',
      '0zzzzzzzzzz',
    ])
  })

  it('returns nothing when nothing matches', () => {
    expect(matchTasks(tasks, 'nothing here')).toEqual([])
  })

  it('caps the list so the panel stays a panel', () => {
    const many = Array.from({ length: 20 }, (_, i) => task(`id${i}`, 'billing'))

    expect(matchTasks(many, 'billing')).toHaveLength(8)
    expect(matchTasks(many, '')).toHaveLength(8)
    expect(matchTasks(many, 'billing', 3)).toHaveLength(3)
  })

  it('copes with a list that never arrived', () => {
    expect(matchTasks(undefined, 'billing')).toEqual([])
    expect(matchTasks(null, '')).toEqual([])
  })

  it('copes with rows missing an id or a title', () => {
    expect(matchTasks([{}], 'billing')).toEqual([])
  })
})

describe('resolveTaskById', () => {
  it('finds the workspace that holds the task', async () => {
    const getTask = vi.fn(async (workspaceId) => {
      if (workspaceId !== 'ws2') throw new Error('404')
      return { task: task('0iCWbYTwIiH', 'Found') }
    })

    const found = await resolveTaskById('0iCWbYTwIiH', ['ws1', 'ws2', 'ws3'], getTask)

    expect(found).toEqual({ workspaceId: 'ws2', task: task('0iCWbYTwIiH', 'Found') })
  })

  it('asks every workspace at once rather than one after another', async () => {
    // A user with a dozen workspaces should not wait a dozen round trips for
    // eleven expected 404s.
    const getTask = vi.fn(async () => {
      throw new Error('404')
    })

    await resolveTaskById('0iCWbYTwIiH', ['a', 'b', 'c'], getTask)

    expect(getTask).toHaveBeenCalledTimes(3)
  })

  it('accepts a bare task as well as a wrapped one', async () => {
    const getTask = async () => task('0iCWbYTwIiH', 'Bare')

    const found = await resolveTaskById('0iCWbYTwIiH', ['ws1'], getTask)

    expect(found.task.title).toBe('Bare')
  })

  it('treats an empty answer as a miss and keeps looking', async () => {
    const getTask = vi.fn(async (workspaceId) => (workspaceId === 'ws1' ? null : task('t', 'Hit')))

    const found = await resolveTaskById('t', ['ws1', 'ws2'], getTask)

    expect(found.workspaceId).toBe('ws2')
  })

  it('reports a miss when no workspace has it', async () => {
    const getTask = async () => {
      throw new Error('404')
    }

    expect(await resolveTaskById('0iCWbYTwIiH', ['ws1'], getTask)).toBeNull()
  })

  it('does not go looking with nothing to look for', async () => {
    const getTask = vi.fn()

    expect(await resolveTaskById('', ['ws1'], getTask)).toBeNull()
    expect(await resolveTaskById('0iCWbYTwIiH', [], getTask)).toBeNull()
    expect(await resolveTaskById('0iCWbYTwIiH', undefined, getTask)).toBeNull()
    expect(getTask).not.toHaveBeenCalled()
  })
})

describe('taskRoute', () => {
  it('uses the workspace the task carries', () => {
    expect(taskRoute(task('t1', 'x', { workspaceId: 'ws9' }))).toBe('/workspaces/ws9/tasks/t1')
  })

  it('falls back to the workspace the caller resolved', () => {
    // The per-workspace lookup knows where it found the task even when the
    // payload does not repeat it.
    expect(taskRoute(task('t1', 'x'), 'ws9')).toBe('/workspaces/ws9/tasks/t1')
  })
})
