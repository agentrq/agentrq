// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect } from 'vitest'
import { canEditTask, canDeleteTask, taskEditPath, withoutTask } from '../src/composables/useTaskRowActions'

describe('task row actions', () => {
  it('edits only tasks that have not run yet, and schedules', () => {
    expect(canEditTask({ status: 'notstarted' }, false)).toBe(true)
    expect(canEditTask({ status: 'cron' }, false)).toBe(true)
    expect(canEditTask({ status: 'ongoing' }, false)).toBe(false)
    expect(canEditTask(null, false)).toBe(false)
  })

  it('offers nothing on an archived workspace', () => {
    expect(canEditTask({ status: 'notstarted' }, true)).toBe(false)
    expect(canDeleteTask({ status: 'completed' }, true)).toBe(false)
    expect(canDeleteTask({ status: 'completed' }, false)).toBe(true)
    expect(canDeleteTask(null, false)).toBe(false)
  })

  it('edits under the task’s own workspace', () => {
    expect(taskEditPath({ id: 't1', workspaceId: 'w1' })).toBe('/workspaces/w1/tasks/t1/edit')
  })

  it('drops a task whichever type its id arrived as', () => {
    expect(withoutTask([{ id: 1 }, { id: '2' }], '1')).toEqual([{ id: '2' }])
  })
})
