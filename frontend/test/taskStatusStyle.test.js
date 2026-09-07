import { describe, it, expect } from 'vitest'

import {
  taskAccentClass,
  taskDotClass,
  taskStatusTone,
  useTaskStatusStyle,
} from '../src/composables/useTaskStatusStyle'

const task = (props) => ({ id: 'abc', messages: [], ...props })
const permissionRequest = (status) => ({ metadata: { type: 'permission_request', status } })

describe('taskStatusTone', () => {
  it('reads each status the task lists can show', () => {
    expect(taskStatusTone(task({ status: 'ongoing' }))).toBe('ongoing')
    expect(taskStatusTone(task({ status: 'notstarted', assignee: 'agent' }))).toBe('notstarted')
    expect(taskStatusTone(task({ status: 'completed' }))).toBe('completed')
    expect(taskStatusTone(task({ status: 'rejected' }))).toBe('rejected')
    expect(taskStatusTone(task({ status: 'blocked' }))).toBe('blocked')
    expect(taskStatusTone(task({ status: 'cron' }))).toBe('cron')
  })

  it('accepts a bare status string, as the column headers pass one', () => {
    expect(taskStatusTone('blocked')).toBe('blocked')
  })

  it('falls back for a status it has never heard of', () => {
    expect(taskStatusTone(task({ status: 'archived' }))).toBe('unknown')
    expect(taskStatusTone(task({}))).toBe('unknown')
    expect(taskStatusTone(undefined)).toBe('unknown')
    expect(taskStatusTone(null)).toBe('unknown')
  })

  describe('waiting on the person', () => {
    it('outranks the underlying status', () => {
      // A task assigned to the human before it starts is not just "not started"
      // — it is the one thing on the board that will not move without them.
      expect(taskStatusTone(task({ status: 'notstarted', assignee: 'human' }))).toBe('pending')
    })

    it('covers a task held up on a permission request', () => {
      const held = task({ status: 'ongoing', messages: [permissionRequest('pending')] })

      expect(taskStatusTone(held)).toBe('pending')
    })

    it('ignores a permission request already answered', () => {
      const answered = task({ status: 'ongoing', messages: [permissionRequest('allow')] })

      expect(taskStatusTone(answered)).toBe('ongoing')
    })

    it('never applies to a finished task', () => {
      // Completing or rejecting a task settles any request it was holding; a
      // finished card that still reads "waiting on you" is asking for an answer
      // nothing will consume.
      const done = task({ status: 'completed', messages: [permissionRequest('pending')] })
      const dropped = task({ status: 'rejected', messages: [permissionRequest('pending')] })

      expect(taskStatusTone(done)).toBe('completed')
      expect(taskStatusTone(dropped)).toBe('rejected')
    })

    it('does not trip over a task carrying no messages', () => {
      expect(taskStatusTone({ id: 'x', status: 'ongoing' })).toBe('ongoing')
    })
  })
})

describe('taskDotClass', () => {
  it('gives every tone a class', () => {
    const tones = [
      'pending',
      'ongoing',
      'notstarted',
      'completed',
      'rejected',
      'blocked',
      'cron',
      'unknown',
    ]

    for (const tone of tones) {
      expect(taskDotClass(tone === 'pending' ? task({ status: 'notstarted', assignee: 'human' }) : tone))
        .toBeTruthy()
    }
  })

  it('separates a running task from a finished one, both being green', () => {
    const running = taskDotClass('ongoing')
    const done = taskDotClass('completed')

    expect(running).not.toBe(done)
    expect(running).toContain('animate-pulse')
    expect(done).not.toContain('animate-pulse')
  })

  it('marks a dark-mode variant on the greys, which have no contrast otherwise', () => {
    expect(taskDotClass('notstarted')).toContain('dark:')
    expect(taskDotClass('unknown')).toContain('dark:')
  })
})

describe('taskAccentClass', () => {
  it('gives every tone a class', () => {
    for (const status of ['ongoing', 'notstarted', 'completed', 'rejected', 'blocked', 'cron']) {
      expect(taskAccentClass(status)).toBeTruthy()
    }
    expect(taskAccentClass('something-else')).toBeTruthy()
  })

  it('agrees with the dot on the hue, so the two read as one signal', () => {
    // The bar and the dot sit on the same card. Comparing the colour family
    // rather than the exact class lets the bar drop the glow and soften the
    // finished statuses without the two being allowed to disagree.
    const family = (cls) => cls.match(/bg-([a-z]+)-/)?.[1]

    for (const status of ['ongoing', 'notstarted', 'completed', 'rejected', 'blocked']) {
      expect(family(taskAccentClass(status))).toBe(family(taskDotClass(status)))
    }
  })

  it('carries no glow or animation, being a full-height bar rather than a dot', () => {
    for (const status of ['ongoing', 'blocked', 'cron']) {
      expect(taskAccentClass(status)).not.toContain('shadow-[')
      expect(taskAccentClass(status)).not.toContain('animate-')
    }
  })
})

describe('useTaskStatusStyle', () => {
  it('hands back the same functions, for components that prefer the composable form', () => {
    const style = useTaskStatusStyle()

    expect(style.taskStatusTone).toBe(taskStatusTone)
    expect(style.taskDotClass).toBe(taskDotClass)
    expect(style.taskAccentClass).toBe(taskAccentClass)
  })
})
