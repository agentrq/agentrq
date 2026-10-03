// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ref } from 'vue'

const updateTaskTitle = vi.fn()
vi.mock('../src/api', () => ({ updateTaskTitle: (...a) => updateTaskTitle(...a) }))

const { TASK_RENAME_WINDOW_MS, canRenameTask, useTaskRename } = await import('../src/composables/useTaskRename')

const NOW = Date.parse('2026-10-03T06:00:00Z')
const DAY = 24 * 60 * 60 * 1000
const createdAgo = (ms) => new Date(NOW - ms).toISOString()

function setup(createdAt = createdAgo(DAY)) {
  const task = ref({ id: 't1', title: 'Untitled', createdAt })
  const onError = vi.fn()
  const rename = useTaskRename(task, ref('ws1'), { onError, now: () => NOW })
  return { task, onError, rename }
}

beforeEach(() => {
  updateTaskTitle.mockReset()
})

describe('canRenameTask', () => {
  it('allows the first 7 days and not a moment after', () => {
    expect(canRenameTask({ createdAt: createdAgo(0) }, NOW)).toBe(true)
    expect(canRenameTask({ createdAt: createdAgo(TASK_RENAME_WINDOW_MS) }, NOW)).toBe(true)
    expect(canRenameTask({ createdAt: createdAgo(TASK_RENAME_WINDOW_MS + 1000) }, NOW)).toBe(false)
  })

  it('refuses a task with no readable creation time', () => {
    expect(canRenameTask({}, NOW)).toBe(false)
    expect(canRenameTask(null, NOW)).toBe(false)
    expect(canRenameTask({ createdAt: 'yesterday' }, NOW)).toBe(false)
  })

  it('measures against the current time by default', () => {
    expect(canRenameTask({ createdAt: new Date().toISOString() })).toBe(true)
  })
})

describe('useTaskRename', () => {
  it('saves the trimmed title and shows the server copy', async () => {
    updateTaskTitle.mockResolvedValue({ task: { id: 't1', title: 'Fix login', createdAt: createdAgo(DAY) } })
    const { task, rename } = setup()

    rename.start()
    expect(rename.editing.value).toBe(true)
    expect(rename.draft.value).toBe('Untitled')

    rename.draft.value = '  Fix login  '
    const saving = rename.save()
    // The blur that follows Enter must not send it twice.
    rename.save()
    await saving

    expect(updateTaskTitle).toHaveBeenCalledTimes(1)
    expect(updateTaskTitle).toHaveBeenCalledWith('ws1', 't1', 'Fix login')
    expect(task.value.title).toBe('Fix login')
    expect(rename.editing.value).toBe(false)
    expect(rename.saving.value).toBe(false)
  })

  it('just closes for an unchanged or empty title', async () => {
    const { rename } = setup()
    rename.start()
    await rename.save()
    expect(rename.editing.value).toBe(false)

    rename.start()
    rename.draft.value = '   '
    await rename.save()
    expect(rename.editing.value).toBe(false)
    expect(updateTaskTitle).not.toHaveBeenCalled()
  })

  it('does nothing when not editing, and cancel discards the draft', async () => {
    const { task, rename } = setup()
    await rename.save()
    rename.start()
    rename.draft.value = 'Something else'
    rename.cancel()
    await rename.save()
    expect(rename.editing.value).toBe(false)
    expect(task.value.title).toBe('Untitled')
    expect(updateTaskTitle).not.toHaveBeenCalled()
  })

  it('stays open and says why when the server refuses', async () => {
    updateTaskTitle.mockRejectedValue(new Error('a task can only be renamed in the first 7 days after it was created'))
    const { task, onError, rename } = setup()

    rename.start()
    rename.draft.value = 'Fix login'
    await rename.save()

    expect(onError).toHaveBeenCalledWith('Could not rename the task: a task can only be renamed in the first 7 days after it was created')
    expect(rename.editing.value).toBe(true)
    expect(task.value.title).toBe('Untitled')
  })

  it('is not offered past 7 days, or before the task has loaded', () => {
    const { rename } = setup(createdAgo(8 * DAY))
    expect(rename.renamable.value).toBe(false)
    rename.start()
    expect(rename.editing.value).toBe(false)

    const empty = useTaskRename(ref(null), ref('ws1'), { onError: vi.fn() })
    expect(empty.renamable.value).toBe(false)
  })
})
